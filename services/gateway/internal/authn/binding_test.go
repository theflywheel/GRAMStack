package authn

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type bindingResume struct{ RequestID, CSRF, ContextID, CookieName, CookieValue string }

func bindingBrowser(t *testing.T, base, id, person string) (*http.Client, string) {
	t.Helper()
	browser := browserClient(t)
	if os.Getenv("GRAMSTACK_TEST_ESIGNET") == "1" {
		start := beginESignet(t, browser, base, id)
		individual := "9000000001"
		if person == "rani" {
			individual = "9000000002"
		}
		target := esignetCallback(t, browser, base, start, individual)
		request, _ := http.NewRequest("GET", target, nil)
		response, body := liveResponse(t, browser, request)
		found := csrfPattern.FindStringSubmatch(body)
		if response.StatusCode != 200 || response.Request.URL.Path != "/interactions/"+id || len(found) != 2 {
			t.Fatal("registry verification interaction unavailable after real eSignet login")
		}
		return browser, found[1]
	}
	action, values := beginBrowser(t, browser, base, id)
	values.Set("identity", person)
	request, _ := http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, body := liveResponse(t, browser, request)
	found := csrfPattern.FindStringSubmatch(body)
	if response.StatusCode != 200 || response.Request.URL.Path != "/interactions/"+id || len(found) != 2 {
		t.Fatal("registry verification interaction unavailable after login")
	}
	return browser, found[1]
}
func bindingRequest(base, id, csrf string) *http.Request {
	values := url.Values{"csrf": {csrf}}
	request, _ := http.NewRequest("POST", base+"/interactions/"+id+"/verify", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", base)
	return request
}
func serviceClient(t *testing.T, identity, peer string) *http.Client {
	t.Helper()
	dir := os.Getenv("GRAMSTACK_TEST_CERTS")
	ca, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid isolated CA")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: peer}
	if identity != "" {
		pair, err := tls.LoadX509KeyPair(filepath.Join(dir, identity+".pem"), filepath.Join(dir, identity+".key"))
		if err != nil {
			t.Fatal(err)
		}
		config.Certificates = []tls.Certificate{pair}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: config}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func TestActualSubjectBinding(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_BINDING") != "1" || os.Getenv("GRAMSTACK_TEST_GATEWAY_URL") == "" || os.Getenv("GRAMSTACK_TEST_DATABASE_URL") == "" {
		t.Skip("requires isolated gateway, SDK adapter and native mock processes")
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	pool, _ := database(t)
	ctx := context.Background()
	token := liveToken(t)
	id := liveRequestMember(t, base, token, "member-12345")
	browser, csrf := bindingBrowser(t, base, id, "sunita")
	for _, tc := range []struct {
		name  string
		alter func(*http.Request)
	}{
		{"cross origin", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") }},
		{"wrong csrf", func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader("csrf=wrong"))
			r.ContentLength = int64(len("csrf=wrong"))
		}},
		{"identity injection", func(r *http.Request) {
			raw := url.Values{"csrf": {csrf}, "principal": {"another-person"}}.Encode()
			r.Body = io.NopCloser(strings.NewReader(raw))
			r.ContentLength = int64(len(raw))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := bindingRequest(base, id, csrf)
			tc.alter(request)
			if response, _ := liveResponse(t, browser, request); response.StatusCode != 403 {
				t.Fatal("unsafe mutation accepted")
			}
		})
	}
	if response, _ := liveResponse(t, browserClient(t), bindingRequest(base, id, csrf)); response.StatusCode != 403 {
		t.Fatal("missing session accepted")
	}
	// Valid concurrent retries must converge on one persisted context and one FSM transition.
	type outcome struct {
		status int
		body   string
	}
	results := make(chan outcome, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := browser.Do(bindingRequest(base, id, csrf))
			if err != nil {
				results <- outcome{status: 0}
				return
			}
			defer response.Body.Close()
			raw, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
			results <- outcome{response.StatusCode, string(raw)}
		}()
	}
	wg.Wait()
	close(results)
	reference := ""
	for result := range results {
		var value struct {
			Status    string `json:"status"`
			Reference string `json:"subject_context_ref"`
		}
		if result.status != 200 || json.Unmarshal([]byte(result.body), &value) != nil || value.Status != "CONSENT_PENDING" || value.Reference == "" {
			t.Fatalf("concurrent verification failed HTTP %d", result.status)
		}
		if reference == "" {
			reference = value.Reference
		} else if reference != value.Reference {
			t.Fatal("retry changed authoritative context")
		}
		for _, private := range []string{"member-12345", "demo-sunita", "source_mapping_ref", "savings_summary"} {
			if strings.Contains(result.body, private) {
				t.Fatal("verification exposed native or business data")
			}
		}
	}
	var state string
	var count int
	if err := pool.QueryRow(ctx, `SELECT status FROM access_requests WHERE request_id=$1`, id).Scan(&state); err != nil || state != "CONSENT_PENDING" {
		t.Fatal("verification did not advance FSM", state, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id=$1 AND payload->>'status'='CONSENT_PENDING'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("verification transition was duplicated", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consent_state WHERE request_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("subject verification implicitly approved consent")
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM browser_state WHERE kind='subject.context' AND key_digest=$1`, cm.Digest([]byte(reference))).Scan(&payload); err != nil || bytes.Contains(payload, []byte(`"source_mapping_ref"`)) || json.Valid(payload) {
		t.Fatal("gateway context is not encrypted", err)
	}
	t.Run("wrong member", func(t *testing.T) {
		wrongID := liveRequestMember(t, base, token, "member-67890")
		wrongBrowser, wrongCSRF := bindingBrowser(t, base, wrongID, "sunita")
		if response, _ := liveResponse(t, wrongBrowser, bindingRequest(base, wrongID, wrongCSRF)); response.StatusCode != 403 {
			t.Fatal("login plus locator established wrong registry ownership")
		}
	})
	t.Run("service authentication", func(t *testing.T) {
		endpoint := os.Getenv("GRAMSTACK_TEST_ADAPTER_URL") + "/internal/v1/subject-verifications"
		for _, role := range []string{"", "unauthorised"} {
			client := serviceClient(t, role, "lokos-adapter.gramstack.internal")
			request, _ := http.NewRequest("POST", endpoint, strings.NewReader("{}"))
			response, err := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if role == "" && err == nil {
				t.Fatal("adapter accepted missing client certificate")
			}
			if role != "" && (err != nil || response.StatusCode != 403) {
				t.Fatal("adapter did not reject wrong service identity")
			}
		}
		trusted := serviceClient(t, "gateway", "lokos-adapter.gramstack.internal")
		for _, tc := range []struct{ version, contentType, body string }{
			{"", "application/json", "{}"},
			{"9.0.0", "application/json", "{}"},
			{"0.1.0", "text/plain", "{}"},
			{"0.1.0", "application/json", `{"snapshot":{},"snapshot":{}}`},
			{"0.1.0", "application/json", `{"unexpected":true}`},
		} {
			request, _ := http.NewRequest("POST", endpoint, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			request.Header.Set("Gramstack-Subject-Verification-Version", tc.version)
			if response, _ := liveResponse(t, trusted, request); response.StatusCode != 400 {
				t.Fatal("unsupported or ambiguous verifier contract accepted", response.StatusCode)
			}
		}
		gateway := serviceClient(t, "gateway", "mock-lokos.gramstack.internal")
		request, _ := http.NewRequest("GET", os.Getenv("GRAMSTACK_TEST_SOURCE_URL")+"/native/v1/members/member-12345", nil)
		response, _ := liveResponse(t, gateway, request)
		if response.StatusCode != 403 {
			t.Fatal("gateway bypassed adapter and read native data")
		}
	})
	t.Run("source outage", func(t *testing.T) {
		failure := filepath.Join(os.Getenv("GRAMSTACK_TEST_SOURCE_GATE"), "unavailable")
		if err := os.WriteFile(failure, []byte("enabled"), 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(failure)
		outageID := liveRequestMember(t, base, token, "member-12345")
		owner, form := bindingBrowser(t, base, outageID, "sunita")
		if response, _ := liveResponse(t, owner, bindingRequest(base, outageID, form)); response.StatusCode != 503 {
			t.Fatal("source outage was accepted or reported as a denial")
		}
		if err := pool.QueryRow(ctx, `SELECT status FROM access_requests WHERE request_id=$1`, outageID).Scan(&state); err != nil || state != "BINDING_PENDING" {
			t.Fatal("outage advanced authority")
		}
	})
	t.Run("authoritative rollback", func(t *testing.T) {
		rollbackID := liveRequestMember(t, base, token, "member-12345")
		owner, form := bindingBrowser(t, base, rollbackID, "sunita")
		var before int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM browser_state WHERE kind LIKE 'subject.%'`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		_, err := pool.Exec(ctx, `CREATE FUNCTION binding_write_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='subject.request' THEN RAISE EXCEPTION 'injected binding write outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER binding_failure BEFORE INSERT ON browser_state FOR EACH ROW EXECUTE FUNCTION binding_write_failure()`)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DROP TRIGGER IF EXISTS binding_failure ON browser_state; DROP FUNCTION IF EXISTS binding_write_failure()`)
		if response, _ := liveResponse(t, owner, bindingRequest(base, rollbackID, form)); response.StatusCode != 503 {
			t.Fatal("failed binding write did not fail closed as unavailable")
		}
		if err = pool.QueryRow(ctx, `SELECT status FROM access_requests WHERE request_id=$1`, rollbackID).Scan(&state); err != nil || state != "BINDING_PENDING" {
			t.Fatal("binding FSM did not roll back")
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM browser_state WHERE kind LIKE 'subject.%'`).Scan(&count); err != nil || count != before {
			t.Fatal("partial binding context persisted")
		}
		if _, err = pool.Exec(ctx, `DROP TRIGGER binding_failure ON browser_state; DROP FUNCTION binding_write_failure()`); err != nil {
			t.Fatal(err)
		}
		if response, _ := liveResponse(t, owner, bindingRequest(base, rollbackID, form)); response.StatusCode != 200 {
			t.Fatal("retry could not recover after rolled-back binding")
		}
	})
	t.Run("cancellation during native verification", func(t *testing.T) {
		raceID := liveRequestMember(t, base, token, "member-12345")
		owner, form := bindingBrowser(t, base, raceID, "sunita")
		dir := os.Getenv("GRAMSTACK_TEST_SOURCE_GATE")
		for _, name := range []string{"enabled", "entered", "release"} {
			os.Remove(filepath.Join(dir, name))
			defer os.Remove(filepath.Join(dir, name))
		}
		if err := os.WriteFile(filepath.Join(dir, "enabled"), []byte("enabled"), 0600); err != nil {
			t.Fatal(err)
		}
		result := make(chan int, 1)
		go func() {
			response, err := owner.Do(bindingRequest(base, raceID, form))
			if err != nil {
				result <- 0
				return
			}
			response.Body.Close()
			result <- response.StatusCode
		}()
		entered := false
		for start := time.Now(); time.Since(start) < 3*time.Second; {
			if _, err := os.Stat(filepath.Join(dir, "entered")); err == nil {
				entered = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !entered {
			t.Fatal("native verifier gate was not reached")
		}
		cancel, _ := http.NewRequest("POST", base+"/v1/access-requests/"+raceID+"/cancel", nil)
		cancel.Header.Set("Authorization", "Bearer "+token)
		if response, _ := liveResponse(t, &http.Client{Timeout: 10 * time.Second}, cancel); response.StatusCode != 200 {
			t.Fatal("concurrent cancellation failed")
		}
		if err := os.WriteFile(filepath.Join(dir, "release"), []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case status := <-result:
			if status != 403 {
				t.Fatal("late verification revived cancelled authority", status)
			}
		case <-time.After(7 * time.Second):
			t.Fatal("native verification did not finish")
		}
		if err := pool.QueryRow(ctx, `SELECT status FROM access_requests WHERE request_id=$1`, raceID).Scan(&state); err != nil || state != "CANCELLED" {
			t.Fatal("late verification changed cancelled state")
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM browser_state WHERE kind='subject.request' AND key_digest=$1`, cm.Digest([]byte(raceID))).Scan(&count); err != nil || count != 0 {
			t.Fatal("late verification installed an authoritative binding")
		}
	})
	// Save only ephemeral private test state for the runner's process restart check.
	resume := bindingResume{RequestID: id, CSRF: csrf, ContextID: reference}
	origin, _ := url.Parse(base)
	for _, cookie := range browser.Jar.Cookies(origin) {
		if strings.Contains(cookie.Name, "session") {
			resume.CookieName = cookie.Name
			resume.CookieValue = cookie.Value
		}
	}
	if resume.CookieValue == "" {
		t.Fatal("session cookie unavailable for restart test")
	}
	raw, err := json.Marshal(resume)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("GRAMSTACK_TEST_BINDING_RESUME"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: actual SDK/native processes, wrong member/service denial, CSRF/session ownership, concurrent idempotency, encrypted custody, outage/rollback and cancellation race; no business data or consent")
}
func TestActualSubjectBindingRestart(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_BINDING") != "1" || os.Getenv("GRAMSTACK_TEST_GATEWAY_URL") == "" || os.Getenv("GRAMSTACK_TEST_DATABASE_URL") == "" {
		t.Skip("requires restarted gateway, SDK adapter and source")
	}
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_TEST_BINDING_RESUME"))
	if err != nil {
		t.Fatal(err)
	}
	var resume bindingResume
	if json.Unmarshal(raw, &resume) != nil {
		t.Fatal("invalid private restart state")
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	browser := browserClient(t)
	origin, _ := url.Parse(base)
	browser.Jar.SetCookies(origin, []*http.Cookie{{Name: resume.CookieName, Value: resume.CookieValue, Path: "/"}})
	response, body := liveResponse(t, browser, bindingRequest(base, resume.RequestID, resume.CSRF))
	var result struct {
		Reference string `json:"subject_context_ref"`
	}
	if response.StatusCode != 200 || json.Unmarshal([]byte(body), &result) != nil || result.Reference != resume.ContextID {
		t.Fatal("gateway restart lost persisted subject binding")
	}
	pool, _ := database(t)
	ctx := context.Background()
	var snapshot []byte
	var digest string
	if err = pool.QueryRow(ctx, `SELECT a.snapshot_bytes,r.request_digest FROM accepted_requests a JOIN access_requests r USING(request_id) WHERE r.request_id=$1`, resume.RequestID).Scan(&snapshot, &digest); err != nil {
		t.Fatal(err)
	}
	stateKey, err := base64.StdEncoding.DecodeString(os.Getenv("GRAMSTACK_AUTH_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(pool, stateKey)
	if err != nil {
		t.Fatal(err)
	}
	var principal Principal
	if err = store.Peek(ctx, "auth.principal", resume.RequestID, "server", &principal); err != nil {
		t.Fatal(err)
	}
	var accepted any
	if json.Unmarshal(snapshot, &accepted) != nil {
		t.Fatal("invalid accepted snapshot")
	}
	payload := map[string]any{"snapshot": accepted, "request_digest": digest, "principal": map[string]any{"context_id": principal.Reference, "issuer": principal.Issuer, "provider_subject": principal.Subject, "authentication_transaction_id": principal.AuthenticationTransactionID, "authenticated_at": principal.AuthenticatedAt, "expires_at": principal.ExpiresAt, "assurance": principal.Assurance, "methods": principal.Methods}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("POST", os.Getenv("GRAMSTACK_TEST_ADAPTER_URL")+"/internal/v1/subject-verifications", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Gramstack-Subject-Verification-Version", "0.1.0")
	response, body = liveResponse(t, serviceClient(t, "gateway", "lokos-adapter.gramstack.internal"), request)
	var adapterResult struct {
		ID string `json:"context_id"`
	}
	if response.StatusCode != 200 || json.Unmarshal([]byte(body), &adapterResult) != nil || adapterResult.ID != resume.ContextID {
		t.Fatal("adapter restart lost its independently persisted verified mapping")
	}
	t.Log("PASS: gateway, adapter and native source restarted; session/core context/adapter encrypted mapping survived without a new authority transition")
}
