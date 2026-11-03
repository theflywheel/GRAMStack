package authn

import (
	"context"
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
	other.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	stolen, _ := http.NewRequest("GET", base+"/interactions/"+rejectedID+"/receipt?format=json", nil)
	response, leak := liveResponse(t, other, stolen)
	if response.StatusCode != 303 || strings.Contains(leak, rejected.Notice.NoticeDigest) {
		t.Fatal("receipt crossed request/session boundary")
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
