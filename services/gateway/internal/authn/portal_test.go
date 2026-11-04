package authn

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

var hiddenInput = regexp.MustCompile(`<input type="hidden" name="([a-z_]+)" value="([^"]*)">`)

func reviewRequest(t *testing.T, base, token string) (string, *http.Client, url.Values, string) {
	t.Helper()
	id := liveRequestMember(t, base, token, "member-12345")
	return reviewExistingRequest(t, base, id)
}
func reviewExistingRequest(t *testing.T, base, id string) (string, *http.Client, url.Values, string) {
	t.Helper()
	browser, csrf := bindingBrowser(t, base, id, "sunita")
	request := bindingRequest(base, id, csrf)
	request.Header.Set("Accept", "text/html")
	response, body := liveResponse(t, browser, request)
	if response.StatusCode != 200 || response.Request.URL.Path != "/interactions/"+id+"/consent" {
		t.Fatalf("consent notice did not render: HTTP %d path %s", response.StatusCode, response.Request.URL.Path)
	}
	values := url.Values{}
	for _, match := range hiddenInput.FindAllStringSubmatch(body, -1) {
		values.Set(match[1], html.UnescapeString(match[2]))
	}
	if values.Get("presentation_id") == "" || values.Get("notice_digest") == "" || values.Get("csrf") == "" {
		t.Fatal("rendered notice lacks bound presentation")
	}
	if strings.Contains(body, `name="confirmation" value="yes" checked`) || !strings.Contains(body, `name="confirmation" value="yes" required`) {
		t.Fatal("approval confirmation is missing or preselected")
	}
	if !strings.Contains(body, "English is the supported notice language") || strings.Contains(body, "source_mapping_ref") || strings.Contains(body, "demo-sunita") {
		t.Fatal("notice hid language limits or exposed private verification data")
	}
	return id, browser, values, body
}
func decisionRequest(base, id string, values url.Values) *http.Request {
	request, _ := http.NewRequest("POST", base+"/interactions/"+id+"/decision", strings.NewReader(values.Encode()))
	request.Header.Set("Origin", base)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}
func copyValues(v url.Values) url.Values {
	out := url.Values{}
	for key, values := range v {
		out[key] = append([]string(nil), values...)
	}
	return out
}
func readRecord(t *testing.T, browser *http.Client, base, id string) cm.Record {
	t.Helper()
	request, _ := http.NewRequest("GET", base+"/interactions/"+id+"/receipt?format=json", nil)
	response, body := liveResponse(t, browser, request)
	var record cm.Record
	if response.StatusCode != 200 || json.Unmarshal([]byte(body), &record) != nil || record.RequestID != id || record.Notice.NoticeDigest == "" {
		t.Fatal("protected receipt unavailable", response.StatusCode)
	}
	if response.Header.Get("Gramstack-Consent-Portal-Version") != "0.1.0" || response.Header.Get("Referrer-Policy") != "same-origin" || response.Header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatal("receipt cache/download controls missing")
	}
	return record
}
func TestActualConsentPortal(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_PORTAL") != "1" || os.Getenv("GRAMSTACK_TEST_GATEWAY_URL") == "" {
		t.Skip("requires configured consent portal and isolated auth/adapter stack")
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	pool, _ := database(t)
	ctx := context.Background()
	token := liveToken(t)
	id, browser, values, body := reviewRequest(t, base, token)
	var text string
	if err := pool.QueryRow(ctx, `SELECT notice_text FROM consent_notices JOIN consent_state USING(consent_id) WHERE request_id=$1`, id).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.UnescapeString(body), text) {
		t.Fatal("browser did not receive the exact stored notice")
	}
	pending := readRecord(t, browser, base, id)
	if pending.Consent.Status != "PENDING" || pending.Decision != nil || pending.SignedArtefact != "" {
		t.Fatal("notice/receipt read activated consent")
	}
	t.Run("unsupported language", func(t *testing.T) {
		request, _ := http.NewRequest("GET", base+"/interactions/"+id+"/consent?lang=hi", nil)
		response, body := liveResponse(t, browser, request)
		if response.StatusCode != 400 || !strings.Contains(body, "Other notice languages are not supported") {
			t.Fatal("unsupported language silently accepted")
		}
	})
	for _, tc := range []struct {
		name   string
		edit   func(url.Values)
		origin string
		status int
	}{
		{"missing confirmation", func(v url.Values) { v.Del("confirmation") }, base, 400},
		{"notice substitution", func(v url.Values) { v.Set("notice_digest", cm.Digest([]byte("other notice"))) }, base, 403},
		{"presentation substitution", func(v url.Values) { v.Set("presentation_id", "forged-presentation") }, base, 403},
		{"principal injection", func(v url.Values) { v.Set("principal", "another-principal") }, base, 403},
		{"duplicate csrf", func(v url.Values) { v.Add("csrf", "second") }, base, 403},
		{"partial decision", func(v url.Values) { v.Set("decision", "partial_refusal") }, base, 403},
		{"cross origin", func(v url.Values) {}, "https://attacker.example", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := copyValues(values)
			v.Set("decision", "approve")
			v.Set("confirmation", "yes")
			tc.edit(v)
			request := decisionRequest(base, id, v)
			request.Header.Set("Origin", tc.origin)
			response, _ := liveResponse(t, browser, request)
			if response.StatusCode != tc.status {
				t.Fatalf("unsafe decision returned %d want %d", response.StatusCode, tc.status)
			}
		})
	}
	approve := copyValues(values)
	approve.Set("decision", "approve")
	approve.Set("confirmation", "yes")
	// Concurrent same-action retries produce one immutable decision and presentation link.
	statuses := make(chan int, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response, err := browser.Do(decisionRequest(base, id, approve))
			if err != nil {
				statuses <- 0
				return
			}
			response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	group.Wait()
	close(statuses)
	for status := range statuses {
		if status != 200 {
			t.Fatal("valid decision retry failed", status)
		}
	}
	recorded := readRecord(t, browser, base, id)
	if recorded.Consent.Status != "APPROVAL_RECORDED" || recorded.SignedArtefact != "" || recorded.Decision == nil || recorded.Decision.Presentation == nil || !recorded.Decision.ExplicitConfirmation || recorded.Decision.Presentation.ID != values.Get("presentation_id") {
		t.Fatal("approval provenance missing or approval incorrectly treated as active")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consent_decisions WHERE consent_id=$1`, recorded.Consent.ConsentID).Scan(&count); err != nil || count != 1 {
		t.Fatal("approval duplicated", err, count)
	}
	conflicting := copyValues(values)
	conflicting.Set("decision", "reject")
	if response, _ := liveResponse(t, browser, decisionRequest(base, id, conflicting)); response.StatusCode != 409 {
		t.Fatal("conflicting second decision accepted")
	}
	withdrawal := url.Values{"csrf": {values.Get("csrf")}}
	request, _ := http.NewRequest("POST", base+"/consents/"+recorded.Consent.ConsentID+"/withdrawal", strings.NewReader(withdrawal.Encode()))
	request.Header.Set("Origin", base)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if response, _ := liveResponse(t, browser, request); response.StatusCode != 200 {
		t.Fatal("authenticated pending-approval withdrawal failed")
	}
	stopped := readRecord(t, browser, base, id)
	if stopped.Consent.Status != "WITHDRAWN" || stopped.Decision.Action != "approve" || stopped.Notice.NoticeDigest != recorded.Notice.NoticeDigest {
		t.Fatal("withdrawal lost original decision or notice")
	}
	// Rejection needs no approval checkbox and never creates an artefact.
	rejectedID, rejectedBrowser, rejectedValues, _ := reviewRequest(t, base, token)
	rejectedValues.Set("decision", "reject")
	if response, _ := liveResponse(t, rejectedBrowser, decisionRequest(base, rejectedID, rejectedValues)); response.StatusCode != 200 {
		t.Fatal("explicit rejection failed")
	}
	rejected := readRecord(t, rejectedBrowser, base, rejectedID)
	if rejected.Consent.Status != "REJECTED" || rejected.Decision == nil || rejected.Decision.Action != "reject" || rejected.Decision.ExplicitConfirmation || rejected.SignedArtefact != "" {
		t.Fatal("rejection created usable permission")
	}
	// A valid session for another request still cannot inspect this request's record.
	otherID := liveRequestMember(t, base, token, "member-67890")
	other, _ := bindingBrowser(t, base, otherID, "rani")
	historyOwner := func(requestID string) (owner, principal string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT owner_digest,principal_ref FROM consent_history_owners WHERE request_id=$1`, requestID).Scan(&owner, &principal); err != nil {
			t.Fatal("history ownership was not persisted", err)
		}
		return
	}
	firstOwner, firstPrincipal := historyOwner(id)
	secondOwner, secondPrincipal := historyOwner(rejectedID)
	otherOwner, _ := historyOwner(otherID)
	if firstOwner != secondOwner || firstPrincipal == secondPrincipal || firstOwner == otherOwner {
		t.Fatal("history must join only the same person across distinct request principals")
	}
	other.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	stolen, _ := http.NewRequest("GET", base+"/interactions/"+rejectedID+"/receipt?format=json", nil)
	response, leak := liveResponse(t, other, stolen)
	if response.StatusCode != 303 || strings.Contains(leak, rejected.Notice.NoticeDigest) {
		t.Fatal("receipt crossed request/session boundary")
	}
	if os.Getenv("GRAMSTACK_TEST_ESIGNET") != "1" {
		t.Run("independent history authentication", func(t *testing.T) {
			listURL := base + "/consent-history/lokos"
			owner := historyBrowser(t, base, "sunita")
			get := func(browser *http.Client, path string) (int, string) {
				t.Helper()
				req, _ := http.NewRequest("GET", path, nil)
				res, body := liveResponse(t, browser, req)
				return res.StatusCode, body
			}
			if status, page := get(owner, listURL); status != 200 ||
				!strings.Contains(page, id+"/receipt") || !strings.Contains(page, rejectedID+"/receipt") ||
				strings.Contains(page, "member-12345") || strings.Contains(page, firstOwner) {
				t.Fatal("history did not list the same person's records with minimal metadata", status)
			}
			exportURL := listURL + "/" + rejectedID + "/receipt"
			status, raw := get(owner, exportURL)
			var record cm.Record
			if status != 200 || json.Unmarshal([]byte(raw), &record) != nil || record.RequestID != rejectedID ||
				record.Decision == nil || record.Decision.Action != "reject" {
				t.Fatal("authenticated history export failed", status)
			}
			if status, page := get(owner, base+"/consent-history/other"); status != 200 || !strings.Contains(page, "Sign in to view history") {
				t.Fatal("history session crossed its registry scope", status)
			}
			another := historyBrowser(t, base, "rani")
			if status, page := get(another, listURL); status != 200 || strings.Contains(page, rejectedID) {
				t.Fatal("history list crossed verified identity", status)
			}
			if status, _ := get(another, exportURL); status != 404 {
				t.Fatal("history export crossed verified identity", status)
			}
			if status, page := get(browserClient(t), exportURL); status != 200 || !strings.Contains(page, "Sign in to view history") {
				t.Fatal("unauthenticated history export did not require sign-in", status)
			}
			rightsURL := listURL + "/" + rejectedID + "/rights"
			if status, _ := get(another, rightsURL); status != 404 {
				t.Fatal("case status crossed owner scope", status)
			}
			status, casePage := get(owner, rightsURL)
			if status != 200 {
				t.Fatal("owner could not open rights case form", status)
			}
			form := url.Values{"case_type": {"correction"}}
			for _, match := range hiddenInput.FindAllStringSubmatch(casePage, -1) {
				form.Set(match[1], match[2])
			}
			post := func(browser *http.Client, origin string) int {
				t.Helper()
				req, _ := http.NewRequest("POST", rightsURL, strings.NewReader(form.Encode()))
				req.Header.Set("Origin", origin)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				res, _ := liveResponse(t, browser, req)
				return res.StatusCode
			}
			if form.Get("csrf") == "" || form.Get("case_id") == "" ||
				post(owner, "https://other.example") != 403 || post(another, base) != 403 {
				t.Fatal("rights form accepted a foreign origin or session")
			}
			for range 2 {
				if status := post(owner, base); status != 200 {
					t.Fatal("rights case submission/retry failed", status)
				}
			}
			status, casePage = get(owner, rightsURL)
			if status != 200 || !strings.Contains(casePage, "correction · queued") ||
				!strings.Contains(casePage, "Responsible registry") ||
				strings.Count(casePage, "Case "+form.Get("case_id")) != 1 {
				t.Fatal("rights case did not retain its pending registry route", status)
			}
			correctionID := form.Get("case_id")
			form.Set("case_type", "grievance")
			if status := post(owner, base); status != 409 {
				t.Fatal("changed retry did not conflict", status)
			}
			form.Set("case_id", rand.Text())
			if status := post(owner, base); status != 200 {
				t.Fatal("grievance submission failed", status)
			}
			var role, responsible, state string
			if err := pool.QueryRow(ctx, `SELECT responsible_role,responsible_id,status FROM rights_cases WHERE case_id=$1`, form.Get("case_id")).
				Scan(&role, &responsible, &state); err != nil || role != "recipient" || responsible != "consumer-one" || state != "queued" {
				t.Fatal("grievance did not retain its pending recipient route", err)
			}
			if os.Getenv("GRAMSTACK_TEST_AUTHZ_URL") != "" {
				checkRightsOperator(t, base, correctionID, form.Get("case_id"))
				status, casePage = get(owner, rightsURL)
				if status != 200 || !strings.Contains(casePage, "correction · resolved") ||
					!strings.Contains(casePage, "reported resolution") || !strings.Contains(casePage, "grievance · exception") ||
					!strings.Contains(casePage, "Legal hold") {
					t.Fatal("owner did not see the asynchronous reported outcomes", status)
				}
			}
		})
	}
	// Safe markup is rendered; private data is not cached or put in the URL.
	css, _ := http.NewRequest("GET", base+"/assets/consent.css", nil)
	response, styles := liveResponse(t, &http.Client{Timeout: 10 * time.Second}, css)
	if response.StatusCode != 200 || !strings.Contains(styles, "focus-visible") {
		t.Fatal("accessible portal styling unavailable")
	}
	if os.Getenv("GRAMSTACK_TEST_WORKERS") == "1" {
		pendingID, pendingBrowser, pendingValues, _ := reviewRequest(t, base, token)
		pendingValues.Set("decision", "approve")
		pendingValues.Set("confirmation", "yes")
		if response, _ := liveResponse(t, pendingBrowser, decisionRequest(base, pendingID, pendingValues)); response.StatusCode != 200 {
			t.Fatal("could not persist pre-restart approval")
		}
		pendingRecord := readRecord(t, pendingBrowser, base, pendingID)
		if pendingRecord.Consent.Status != "APPROVAL_RECORDED" || pendingRecord.SignedArtefact != "" {
			t.Fatal("disabled worker created authority")
		}
		checkpoint := workerCheckpoint{Pending: pendingID, Consent: pendingRecord.Consent.ConsentID, Digest: pendingRecord.Notice.RequestDigest, NoticeDigest: pendingRecord.Notice.NoticeDigest, Withdrawn: id, Rejected: rejectedID}
		raw, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(os.Getenv("GRAMSTACK_TEST_WORKER_RESUME"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("PASS: exact notice and session-bound presentation, explicit approve/reject, replay/substitution denial, pending-approval withdrawal and protected receipt export; no implicit activation")
}

func historyBrowser(t *testing.T, base, identity string) *http.Client {
	t.Helper()
	browser := browserClient(t)
	start, _ := http.NewRequest("GET", base+"/auth/history/lokos", nil)
	response, body := liveResponse(t, browser, start)
	ticket, csrf := ticketPattern.FindStringSubmatch(body), csrfPattern.FindStringSubmatch(body)
	if response.StatusCode != 200 || len(ticket) != 2 || len(csrf) != 2 {
		t.Fatal("independent history sign-in did not reach the identity provider")
	}
	values := url.Values{"ticket": {ticket[1]}, "csrf": {csrf[1]}, "identity": {identity}}
	origin := response.Request.URL.Scheme + "://" + response.Request.URL.Host
	choose, _ := http.NewRequest("POST", origin+"/authorize", strings.NewReader(values.Encode()))
	choose.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	browser.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if next.URL.Path == "/auth/provider/callback" {
			return http.ErrUseLastResponse
		}
		return nil
	}
	pending, _ := liveResponse(t, browser, choose)
	target := pending.Header.Get("Location")
	if !strings.Contains(target, "/auth/provider/callback") {
		t.Fatal("history sign-in did not reach provider callback")
	}
	foreign, _ := http.NewRequest("GET", target, nil)
	if res, _ := liveResponse(t, browserClient(t), foreign); res.StatusCode != 403 {
		t.Fatal("foreign browser consumed history sign-in")
	}
	browser.CheckRedirect = nil
	callback, _ := http.NewRequest("GET", target, nil)
	if res, _ := liveResponse(t, browser, callback); res.StatusCode != 200 || res.Request.URL.Path != "/consent-history/lokos" {
		t.Fatal("history sign-in did not establish an independent session", res.StatusCode)
	}
	replay, _ := http.NewRequest("GET", target, nil)
	if res, _ := liveResponse(t, browser, replay); res.StatusCode != 403 {
		t.Fatal("history callback was replayable", res.StatusCode)
	}
	return browser
}

func checkRightsOperator(t *testing.T, public, correction, grievance string) {
	t.Helper()
	private := os.Getenv("GRAMSTACK_TEST_AUTHZ_URL")
	registry := serviceClient(t, "lokos-rights", "gateway.gramstack.internal")
	recipient := serviceClient(t, "consumer-one", "gateway.gramstack.internal")
	adapter := serviceClient(t, "lokos-adapter", "gateway.gramstack.internal")
	call := func(client *http.Client, method, origin, path, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, origin+"/internal/v1/rights-cases/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response, content := liveResponse(t, client, req)
		return response.StatusCode, content
	}
	registryPath := "registry/lokos"
	recipientPath := "recipient/consumer-one"
	if status, _ := call(adapter, "GET", private, registryPath, ""); status != 404 {
		t.Fatal("adapter was treated as a registry rights operator", status)
	}
	if status, _ := call(browserClient(t), "GET", public, registryPath, ""); status != 404 {
		t.Fatal("rights operator queue exposed on the public listener", status)
	}
	if status, queue := call(registry, "GET", private, registryPath, ""); status != 200 ||
		!strings.Contains(queue, correction) || strings.Contains(queue, grievance) || strings.Contains(queue, "member-12345") {
		t.Fatal("registry queue leaked or omitted case metadata", status)
	}
	if status, queue := call(recipient, "GET", private, recipientPath, ""); status != 200 ||
		!strings.Contains(queue, grievance) || strings.Contains(queue, correction) {
		t.Fatal("recipient queue crossed case routes", status)
	}
	ack := `{"action":"acknowledge"}`
	resolve := `{"action":"resolve"}`
	exception := `{"action":"record_exception","exception_code":"legal_hold"}`
	if status, _ := call(registry, "PUT", private, registryPath+"/"+grievance+"/ack-1", ack); status != 404 {
		t.Fatal("registry could update the recipient's case", status)
	}
	if status, _ := call(registry, "PUT", private, registryPath+"/"+correction+"/done-1", resolve); status != 409 {
		t.Fatal("case skipped acknowledgement", status)
	}
	for _, fixture := range []struct {
		client              *http.Client
		path, outcome, want string
	}{
		{registry, registryPath + "/" + correction, resolve, "resolved"},
		{recipient, recipientPath + "/" + grievance, exception, "exception"},
	} {
		status, first := call(fixture.client, "PUT", private, fixture.path+"/ack-1", ack)
		if status != 200 || !strings.Contains(first, `"status":"acknowledged"`) {
			t.Fatal("case acknowledgement failed", status)
		}
		status, terminal := call(fixture.client, "PUT", private, fixture.path+"/done-1", fixture.outcome)
		if status != 200 || !strings.Contains(terminal, `"status":"`+fixture.want+`"`) {
			t.Fatal("case terminal assertion failed", status)
		}
		if status, repeated := call(fixture.client, "PUT", private, fixture.path+"/ack-1", ack); status != 200 || repeated != first {
			t.Fatal("acknowledgement retry changed after terminal outcome", status)
		}
		if status, _ := call(fixture.client, "PUT", private, fixture.path+"/ack-1", resolve); status != 409 {
			t.Fatal("changed event retry was accepted", status)
		}
	}
}
