package authn

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ticketPattern = regexp.MustCompile(`name="ticket" value="([A-Za-z0-9]+)"`)
var csrfPattern = regexp.MustCompile(`name="csrf" value="([A-Za-z0-9]+)"`)

func liveResponse(t *testing.T, client *http.Client, request *http.Request) (*http.Response, string) {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("browser request failed:", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65536))
	if err != nil {
		t.Fatal(err)
	}
	return response, string(raw)
}
func liveToken(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest("POST", os.Getenv("GRAMSTACK_TEST_HYDRA_PUBLIC")+"/oauth2/token", strings.NewReader("grant_type=client_credentials&scope=gramstack.access&audience=gramstack"))
	req.SetBasicAuth("consumer-one", "isolated-synthetic-secret")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, body := liveResponse(t, &http.Client{Timeout: 10 * time.Second}, req)
	var value struct {
		Token string `json:"access_token"`
	}
	if json.Unmarshal([]byte(body), &value) != nil || response.StatusCode != 200 || value.Token == "" {
		t.Fatal("synthetic consumer token unavailable")
	}
	return value.Token
}
func liveRequest(t *testing.T, base, token string) string {
	return liveRequestMember(t, base, token, "fictional-member")
}
func liveRequestMember(t *testing.T, base, token, member string) string {
	t.Helper()
	input := `{"registry":"lokos","resource":"shg.membership","subject_hint":{"type":"member_reference","value":"fictional-member"},"fields":["membership_status"],"purpose":"credit_eligibility","access":{"mode":"single_use","valid_for_seconds":300},"recipient_terms":{"retention_policy_id":"terms-v1"}}`
	encoded, _ := json.Marshal(member)
	input = strings.Replace(input, `"fictional-member"`, string(encoded), 1)
	req, _ := http.NewRequest("POST", base+"/v1/access-requests", strings.NewReader(input))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", time.Now().Format("150405.000000000"))
	response, body := liveResponse(t, &http.Client{Timeout: 10 * time.Second}, req)
	var value struct {
		ID string `json:"request_id"`
	}
	if response.StatusCode != 202 || json.Unmarshal([]byte(body), &value) != nil || value.ID == "" {
		t.Fatal("request acceptance failed:", response.StatusCode)
	}
	return value.ID
}
func browserClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}
func beginBrowser(t *testing.T, browser *http.Client, base, id string) (string, url.Values) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/interactions/"+id, nil)
	response, body := liveResponse(t, browser, req)
	if response.StatusCode != 200 || !strings.Contains(body, "Choose a demo identity") {
		t.Fatalf("federation did not reach fictional IdP: status=%d endpoint=%s body=%s", response.StatusCode, response.Request.URL.Path, body)
	}
	ticket, csrf := ticketPattern.FindStringSubmatch(body), csrfPattern.FindStringSubmatch(body)
	if len(ticket) != 2 || len(csrf) != 2 {
		t.Fatal("fictional IdP form unavailable")
	}
	return response.Request.URL.Scheme + "://" + response.Request.URL.Host + "/authorize", url.Values{"ticket": {ticket[1]}, "csrf": {csrf[1]}, "identity": {"sunita"}}
}
func TestActualFederation(t *testing.T) {
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	if base == "" || os.Getenv("GRAMSTACK_TEST_AUTHN") != "1" {
		t.Skip("isolated Hydra/IdP/gateway not configured")
	}
	pool, key := database(t)
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
	browser := browserClient(t)
	action, values := beginBrowser(t, browser, base, id)
	req, _ := http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var callbackURL string
	browser.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Path == "/auth/callback" {
			callbackURL = next.URL.String()
		}
		if len(via) > 15 {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, body := liveResponse(t, browser, req)
	if response.StatusCode != 200 || response.Request.URL.Path != "/interactions/"+id || !strings.Contains(body, "Authenticated interaction") {
		t.Fatalf("federation failed: status=%d endpoint=%s body=%s", response.StatusCode, response.Request.URL.Path, body)
	}
	var state string
	var consents int
	if err = pool.QueryRow(req.Context(), "SELECT status FROM access_requests WHERE request_id=$1", id).Scan(&state); err != nil || state != "BINDING_PENDING" {
		t.Fatalf("authentication did not stop before registry verification: %s %v", state, err)
	}
	if err = pool.QueryRow(req.Context(), "SELECT count(*) FROM consent_state WHERE request_id=$1", id).Scan(&consents); err != nil || consents != 0 {
		t.Fatal("OAuth login implicitly approved disclosure")
	}
	var principal Principal
	if err = store.Peek(req.Context(), "auth.principal", id, "server", &principal); err != nil || principal.Subject != "demo-sunita" || principal.Issuer != os.Getenv("GRAMSTACK_TEST_IDP_URL") {
		t.Fatalf("upstream assurance not retained: %v", err)
	}
	if callbackURL == "" {
		t.Fatal("Hydra callback not observed")
	}
	replay, _ := http.NewRequest("GET", callbackURL, nil)
	if res, _ := liveResponse(t, browser, replay); res.StatusCode != 403 {
		t.Fatal("outer callback replay accepted")
	}
	// Capture a still-unused upstream callback, reject it in a different browser,
	// then prove the correct browser can complete the very same transaction.
	id2 := liveRequest(t, base, token)
	owner := browserClient(t)
	action, values = beginBrowser(t, owner, base, id2)
	owner.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Path == "/auth/provider/callback" {
			return http.ErrUseLastResponse
		}
		return nil
	}
	choose, _ := http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	choose.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	pending, _ := liveResponse(t, owner, choose)
	target := pending.Header.Get("Location")
	if !strings.Contains(target, "/auth/provider/callback") {
		t.Fatal("upstream callback not reached")
	}
	stolen, _ := http.NewRequest("GET", target, nil)
	if result, _ := liveResponse(t, browserClient(t), stolen); result.StatusCode != 403 {
		t.Fatal("wrong browser completed authentication")
	}
	owner.CheckRedirect = nil
	complete, _ := http.NewRequest("GET", target, nil)
	if result, _ := liveResponse(t, owner, complete); result.StatusCode != 200 || result.Request.URL.Path != "/interactions/"+id2 {
		t.Fatal("correct browser could not complete after invalid attempt")
	}
	// Expiry and cancellation are checked again after authentication, not only at start.
	id3 := liveRequest(t, base, token)
	cancelBrowser := browserClient(t)
	action, values = beginBrowser(t, cancelBrowser, base, id3)
	cancel, _ := http.NewRequest("POST", base+"/v1/access-requests/"+id3+"/cancel", nil)
	cancel.Header.Set("Authorization", "Bearer "+token)
	if result, _ := liveResponse(t, &http.Client{Timeout: 10 * time.Second}, cancel); result.StatusCode != 200 {
		t.Fatal("cancellation failed")
	}
	choose, _ = http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	choose.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if result, _ := liveResponse(t, cancelBrowser, choose); result.StatusCode != 403 {
		t.Fatal("cancelled request completed login")
	}
	// A different freshly authenticated person cannot take over an existing request.
	another := browserClient(t)
	action, values = beginBrowser(t, another, base, id)
	values.Set("identity", "rani")
	choose, _ = http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	choose.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if result, _ := liveResponse(t, another, choose); result.StatusCode != 403 {
		t.Fatal("different person took over request-scoped authentication")
	}
	id4 := liveRequest(t, base, token)
	expiredBrowser := browserClient(t)
	action, values = beginBrowser(t, expiredBrowser, base, id4)
	if _, err = pool.Exec(req.Context(), "UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=$1", id4); err != nil {
		t.Fatal(err)
	}
	choose, _ = http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	choose.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if result, _ := liveResponse(t, expiredBrowser, choose); result.StatusCode != 403 {
		t.Fatal("expired request completed login")
	}

}
