package authn

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type esignetFlow struct {
	Challenge string `json:"challengeToken"`
	Status    string `json:"flowStatus"`
	Assertion string `json:"assertion"`
	Data      struct {
		Inputs []struct {
			ID string `json:"identifier"`
		} `json:"inputs"`
		Actions []struct {
			Ref string `json:"ref"`
		} `json:"actions"`
	} `json:"data"`
}

func beginESignet(t *testing.T, browser *http.Client, base, id string) *url.URL {
	t.Helper()
	request, _ := http.NewRequest("GET", base+"/interactions/"+id, nil)
	response, _ := liveResponse(t, browser, request)
	u := response.Request.URL
	if response.StatusCode != 200 || u.Scheme+"://"+u.Host != os.Getenv("GRAMSTACK_TEST_ESIGNET_UI") || u.Path != "/signin" || u.Query().Get("executionId") == "" || u.Query().Get("authId") == "" {
		t.Fatalf("federation did not reach official eSignet UI: status %d path %s", response.StatusCode, u.Path)
	}
	return u
}

// Complete the official mock OTP flow API behind the real eSignet UI. This is
// HTTP federation integration; rendering and interactive UI acceptance are D15.
func esignetCallback(t *testing.T, browser *http.Client, base string, start *url.URL, person string) string {
	t.Helper()
	post := func(path string, input, output any) {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest("POST", os.Getenv("GRAMSTACK_TEST_ESIGNET_API_URL")+path, bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		response, body := liveResponse(t, browser, request)
		if response.StatusCode != 200 || json.Unmarshal([]byte(body), output) != nil {
			t.Fatalf("official eSignet %s failed: HTTP %d", path, response.StatusCode)
		}
	}
	var flow esignetFlow
	post("/flow/execute", map[string]string{"executionId": start.Query().Get("executionId")}, &flow)
	for step := 0; step < 8 && flow.Status != "COMPLETE"; step++ {
		if flow.Status == "ERROR" || len(flow.Data.Actions) == 0 {
			t.Fatalf("official eSignet flow stopped at step %d status %s", step, flow.Status)
		}
		inputs := map[string]string{}
		for _, input := range flow.Data.Inputs {
			switch input.ID {
			case "username":
				inputs[input.ID] = person
			case "otp":
				inputs[input.ID] = "111111"
			case "captcha_token":
				inputs[input.ID] = "local-demo-captcha-token"
			default:
				t.Fatalf("unexpected official eSignet input %q", input.ID)
			}
		}
		payload := map[string]any{"executionId": start.Query().Get("executionId"), "challengeToken": flow.Challenge, "action": flow.Data.Actions[0].Ref, "inputs": inputs}
		flow = esignetFlow{}
		post("/flow/execute", payload, &flow)
	}
	if flow.Status != "COMPLETE" || flow.Assertion == "" {
		t.Fatal("official eSignet authentication did not complete")
	}
	var completed struct {
		Redirect string `json:"redirect_uri"`
		Camel    string `json:"redirectUri"`
	}
	post("/oauth2/auth/callback", map[string]string{"authId": start.Query().Get("authId"), "assertion": flow.Assertion}, &completed)
	target := completed.Redirect
	if target == "" {
		target = completed.Camel
	}
	u, err := url.Parse(target)
	if err != nil || !sameOrigin(target, base) || u.Path != "/auth/provider/callback" || u.Query().Get("state") == "" || u.Query().Get("code") == "" || u.Query().Get("error") != "" {
		t.Fatal("invalid official eSignet callback target")
	}
	return target
}
func TestActualESignetFederation(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_ESIGNET") != "1" || os.Getenv("GRAMSTACK_TEST_GATEWAY_URL") == "" {
		t.Skip("requires isolated Hydra, gateway and official local eSignet stack")
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	pool, _ := database(t)
	key, err := base64.StdEncoding.DecodeString(os.Getenv("GRAMSTACK_AUTH_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(pool, key)
	if err != nil {
		t.Fatal(err)
	}
	token := liveToken(t)
	id := liveRequest(t, base, token)
	owner := browserClient(t)
	start := beginESignet(t, owner, base, id)
	callback := esignetCallback(t, owner, base, start, "9000000001")
	tampered, err := url.Parse(callback)
	if err != nil {
		t.Fatal(err)
	}
	parameters := tampered.Query()
	parameters.Set("state", strings.Repeat("x", 26))
	tampered.RawQuery = parameters.Encode()
	altered, _ := http.NewRequest("GET", tampered.String(), nil)
	if denied, _ := liveResponse(t, owner, altered); denied.StatusCode != 403 {
		t.Fatal("incorrect upstream state accepted")
	}
	// Incorrect browser must not consume the owner's unused upstream transaction.
	request, _ := http.NewRequest("GET", callback, nil)
	if response, _ := liveResponse(t, browserClient(t), request); response.StatusCode != 403 {
		t.Fatal("wrong browser completed real eSignet callback")
	}
	var outerCallback string
	owner.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Path == "/auth/callback" {
			outerCallback = next.URL.String()
		}
		if len(via) > 15 {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, body := liveResponse(t, owner, request)
	if response.StatusCode != 200 || response.Request.URL.Path != "/interactions/"+id || !strings.Contains(body, "Authenticated interaction") {
		t.Fatalf("real federation failed: status %d path %s", response.StatusCode, response.Request.URL.Path)
	}
	var state string
	var consents int
	if err = pool.QueryRow(request.Context(), "SELECT status FROM access_requests WHERE request_id=$1", id).Scan(&state); err != nil || state != "BINDING_PENDING" {
		t.Fatal("real authentication did not stop before binding:", state, err)
	}
	if err = pool.QueryRow(request.Context(), "SELECT count(*) FROM consent_state WHERE request_id=$1", id).Scan(&consents); err != nil || consents != 0 {
		t.Fatal("real OAuth authentication implicitly granted disclosure consent")
	}
	var principal Principal
	if err = store.Peek(request.Context(), "auth.principal", id, "server", &principal); err != nil || principal.Issuer != os.Getenv("GRAMSTACK_TEST_IDP_URL") || principal.Subject == "" || principal.Assurance == "" || principal.Subject == "9000000001" || !principal.ExpiresAt.After(time.Now()) || principal.ExpiresAt.After(time.Now().Add(3*time.Minute)) {
		t.Fatal("verified eSignet principal/lifetime not retained", err)
	}
	service := &Service{store: store}
	origin, _ := url.Parse(base)
	var session Session
	for _, cookie := range owner.Jar.Cookies(origin) {
		if cookie.Name == service.cookieName("session") {
			if err = store.Peek(request.Context(), "auth.session", cookie.Value, cookie.Value, &session); err != nil {
				t.Fatal(err)
			}
		}
	}
	if session.RequestID != id || session.PrincipalRef != principal.Reference || session.ExpiresAt.After(principal.ExpiresAt) {
		t.Fatal("session exceeds verified identity lifetime")
	}
	if outerCallback == "" {
		t.Fatal("outer Hydra callback not observed")
	}
	for _, replay := range []string{callback, outerCallback} {
		request, _ = http.NewRequest("GET", replay, nil)
		if response, _ = liveResponse(t, owner, request); response.StatusCode != 403 {
			t.Fatal("federation callback replay accepted")
		}
	}
	// Reauthentication can renew only the same verified issuer/subject.
	fresh := browserClient(t)
	start = beginESignet(t, fresh, base, id)
	callback = esignetCallback(t, fresh, base, start, "9000000001")
	request, _ = http.NewRequest("GET", callback, nil)
	if response, _ = liveResponse(t, fresh, request); response.StatusCode != 200 || response.Request.URL.Path != "/interactions/"+id {
		t.Fatal("same-person real eSignet reauthentication failed")
	}
	var renewed Principal
	if store.Peek(request.Context(), "auth.principal", id, "server", &renewed) != nil || renewed.Reference != principal.Reference || renewed.Subject != principal.Subject || renewed.AuthenticationTransactionID == principal.AuthenticationTransactionID {
		t.Fatal("reauthentication did not preserve only the request-scoped identity binding")
	}
	another := browserClient(t)
	start = beginESignet(t, another, base, id)
	callback = esignetCallback(t, another, base, start, "9000000002")
	request, _ = http.NewRequest("GET", callback, nil)
	if response, _ = liveResponse(t, another, request); response.StatusCode != 403 {
		t.Fatal("different real eSignet principal took over a request")
	}
	for _, reason := range []string{"cancelled", "expired"} {
		t.Run(reason, func(t *testing.T) {
			id := liveRequest(t, base, token)
			browser := browserClient(t)
			start := beginESignet(t, browser, base, id)
			if reason == "cancelled" {
				cancel, _ := http.NewRequest("POST", base+"/v1/access-requests/"+id+"/cancel", nil)
				cancel.Header.Set("Authorization", "Bearer "+token)
				if response, _ := liveResponse(t, &http.Client{Timeout: 10 * time.Second}, cancel); response.StatusCode != 200 {
					t.Fatal("request cancellation failed")
				}
			} else {
				if _, err := pool.Exec(request.Context(), "UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=$1", id); err != nil {
					t.Fatal(err)
				}
			}
			target := esignetCallback(t, browser, base, start, "9000000001")
			request, _ := http.NewRequest("GET", target, nil)
			if response, _ := liveResponse(t, browser, request); response.StatusCode != 403 {
				t.Fatal("late callback revived a", reason, "request")
			}
		})
	}
	t.Log("PASS: real Hydra + official eSignet + running gateway, browser binding, callback replay denial, session lifetime, same/different-person reauthentication and cancelled/expired request denial; identity backend is mock")
}
