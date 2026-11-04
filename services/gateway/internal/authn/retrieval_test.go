package authn

import (
	"bytes"
	"context"
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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/admission"
)

type retrievalCheckpoint struct {
	RequestID, Grant, ReceiptID, AttemptID string
	Reads                                  int
	Version                                int64
}

func dataRequest(origin, id, grant string) *http.Request {
	r, _ := http.NewRequest("GET", origin+"/v1/access-requests/"+id+"/data", nil)
	if grant != "" {
		r.Header.Set("Authorization", "Bearer "+grant)
	}
	return r
}
func sourceReads(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GRAMSTACK_TEST_SOURCE_READ_GATE"), "reads.log"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(raw, []byte("\n"))
}
func adapterDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), os.Getenv("GRAMSTACK_TEST_ADAPTER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func TestActualProtectedRetrieval(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_RETRIEVAL") != "1" {
		t.Skip("requires configured end-to-end protected data path")
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	private := os.Getenv("GRAMSTACK_TEST_AUTHZ_URL")
	token := liveToken(t)
	pool, _ := database(t)
	adapterPool := adapterDatabase(t)
	ctx := context.Background()
	type prepared struct {
		id      string
		browser *http.Client
		values  url.Values
	}
	cases := map[string]prepared{}
	for _, name := range []string{"happy", "both fields", "offline", "disconnect", "custody outage", "withdraw before admission", "withdraw after admission"} {
		fields := []string{"membership_status"}
		if name == "both fields" {
			fields = append(fields, "savings_summary")
		}
		next := liveRequestFields(t, base, token, "member-12345", fields)
		_, browser, values, _ := reviewExistingRequest(t, base, next)
		values.Set("decision", "approve")
		values.Set("confirmation", "yes")
		if response, _ := liveResponse(t, browser, decisionRequest(base, next, values)); response.StatusCode != 200 {
			t.Fatal("batch approval failed")
		}
		cases[name] = prepared{next, browser, values}
	}
	for _, p := range cases {
		awaitRequestState(t, base, token, p.id, "CONSENTED")
	}
	id := cases["happy"].id
	g := liveGrant(t, base, id, token, "retrieval-"+id)
	before := sourceReads(t)
	consumerClient := serviceClient(t, "consumer-one", "gateway.gramstack.internal")
	if response, _ := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, dataRequest(base, id, g.Grant)); response.StatusCode != 404 {
		t.Fatal("data route exposed without consumer TLS transport", response.StatusCode)
	}
	for _, identity := range []string{"gateway", "lokos-adapter", "unauthorised"} {
		r := dataRequest(private, id, g.Grant)
		r.Header.Set("X-Forwarded-Client-Cert", "consumer-one")
		response, body := liveResponse(t, serviceClient(t, identity, "gateway.gramstack.internal"), r)
		if response.StatusCode != 403 || strings.Contains(body, "membership_status") {
			t.Fatal("sender substitution reached data", identity, response.StatusCode)
		}
	}
	head := dataRequest(private, id, g.Grant)
	head.Method = "HEAD"
	if response, _ := liveResponse(t, consumerClient, head); response.StatusCode != 405 {
		t.Fatal("HEAD consumed a disclosure", response.StatusCode)
	}
	if response, _ := liveResponse(t, consumerClient, dataRequest(private, id, "")); response.StatusCode != 401 {
		t.Fatal("missing grant accepted", response.StatusCode)
	}
	if sourceReads(t) != before {
		t.Fatal("invalid presenter caused native access")
	}
	type outcome struct {
		status int
		body   []byte
	}
	results := make(chan outcome, 12)
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			r, err := consumerClient.Do(dataRequest(private, id, g.Grant))
			if err != nil {
				results <- outcome{}
				return
			}
			defer r.Body.Close()
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			results <- outcome{r.StatusCode, raw}
		}()
	}
	group.Wait()
	close(results)
	var approved struct {
		RequestID string                     `json:"request_id"`
		Data      map[string]json.RawMessage `json:"data"`
		ReceiptID string                     `json:"receipt_id"`
	}
	successes := 0
	for result := range results {
		switch result.status {
		case 200:
			successes++
			if json.Unmarshal(result.body, &approved) != nil || approved.RequestID != id || len(approved.Data) != 1 || string(approved.Data["membership_status"]) != `"active"` || approved.ReceiptID == "" || bytes.Contains(result.body, []byte("12500")) {
				t.Fatal("projection or receipt was wrong")
			}
		case 409, 429:
			if bytes.Contains(result.body, []byte("membership_status")) {
				t.Fatal("denied replay leaked data")
			}
		default:
			t.Fatal("unexpected concurrent retrieval result", result.status)
		}
	}
	if successes != 1 || sourceReads(t) != before+1 {
		t.Fatal("concurrent requests did not perform exactly one native call", successes, sourceReads(t)-before)
	}
	version := awaitRequestState(t, base, token, id, "COMPLETED")
	var attempt, state string
	var count int
	if err := pool.QueryRow(ctx, `SELECT attempt_id,state FROM gateway_retrieval_attempts WHERE request_id=$1`, id).Scan(&attempt, &state); err != nil || state != "COMPLETED" {
		t.Fatal("gateway completion was not durable", err, state)
	}
	if err := adapterPool.QueryRow(ctx, `SELECT state FROM adapter_attempts WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, attempt).Scan(&state); err != nil || state != "COMPLETED" {
		t.Fatal("SDK completion was not durable", err, state)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM evidence_receipts e JOIN outbox o ON o.aggregate_type='evidence' AND o.aggregate_id=e.receipt_id WHERE e.receipt_id=$1 AND e.request_id=$2 AND e.attempt_id=$3 AND e.outcome='release_ready'`, approved.ReceiptID, id, attempt).Scan(&count); err != nil || count != 1 {
		t.Fatal("data was released without durable evidence and work", err)
	}
	t.Run("recipient handling report", func(t *testing.T) {
		path := "/v1/disclosures/" + approved.ReceiptID + "/handling-reports/report-1"
		occurred := time.Now().UTC().Format(time.RFC3339Nano)
		payload := `{"action":"retention_exception","exception_code":"legal_hold","occurred_at":"` + occurred + `"}`
		send := func(origin string, client *http.Client, body string) (int, string) {
			t.Helper()
			request, err := http.NewRequest("PUT", origin+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Forwarded-Client-Cert", "consumer-one")
			response, content := liveResponse(t, client, request)
			return response.StatusCode, content
		}
		if status, _ := send(base, &http.Client{Timeout: 5 * time.Second}, payload); status != 404 {
			t.Fatal("handling report route exposed on public listener", status)
		}
		if status, _ := send(private, serviceClient(t, "lokos-adapter", "gateway.gramstack.internal"), payload); status != 404 {
			t.Fatal("adapter could submit a recipient report", status)
		}
		if status, _ := send(private, consumerClient, strings.Replace(payload, "legal_hold", "unapproved", 1)); status != 400 {
			t.Fatal("unsupported retention exception admitted", status)
		}
		status, recorded := send(private, consumerClient, payload)
		if status != 200 || !strings.Contains(recorded, `"verification":"reported"`) ||
			!strings.Contains(recorded, `"terms_version":"terms-v1"`) ||
			!strings.Contains(recorded, `"producer_id":"consumer-one"`) {
			t.Fatal("valid recipient assertion was not bound to the disclosure and terms", status)
		}
		if status, repeated := send(private, consumerClient, payload); status != 200 || repeated != recorded {
			t.Fatal("identical retry changed handling report", status)
		}
		if status, _ := send(private, consumerClient, strings.Replace(payload, "legal_hold", "statutory_retention", 1)); status != 409 {
			t.Fatal("report ID reuse changed recorded assertion", status)
		}
		receiptURL := base + "/interactions/" + id + "/receipt"
		view, _ := http.NewRequest("GET", receiptURL, nil)
		response, page := liveResponse(t, cases["happy"].browser, view)
		if response.StatusCode != 200 || !strings.Contains(page, "Recipient handling reports") ||
			!strings.Contains(page, "Reported by consumer-one") || !strings.Contains(page, "Legal hold") ||
			!strings.Contains(page, "has not independently verified") || strings.Contains(page, approved.ReceiptID) {
			t.Fatal("protected receipt did not render the limited handling assertion", response.StatusCode)
		}
		anonymous := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		unauthenticatedView, _ := http.NewRequest("GET", receiptURL, nil)
		response, page = liveResponse(t, anonymous, unauthenticatedView)
		if response.StatusCode != 303 || strings.Contains(page, "Legal hold") {
			t.Fatal("handling assertion escaped the authenticated receipt", response.StatusCode)
		}
		export, _ := http.NewRequest("GET", receiptURL+"?format=json", nil)
		response, raw := liveResponse(t, cases["happy"].browser, export)
		if response.StatusCode != 200 || strings.Contains(raw, "legal_hold") || strings.Contains(raw, "HandlingReports") {
			t.Fatal("the v1 consent export changed", response.StatusCode)
		}
		if sourceReads(t) != before+1 {
			t.Fatal("handling report triggered another native read")
		}
	})
	if os.Getenv("GRAMSTACK_TEST_ESIGNET") != "1" {
		t.Run("owner access audit", func(t *testing.T) {
			browser := historyBrowser(t, base, "sunita")
			history, _ := http.NewRequest("GET", base+"/consent-history/lokos", nil)
			response, body := liveResponse(t, browser, history)
			for _, card := range strings.Split(body, "</section>") {
				if strings.Contains(card, "/"+id+"/receipt") {
					if response.StatusCode != 200 || !strings.Contains(card, "Single use spent · Request COMPLETED") {
						t.Fatal("history presented completed single-use consent as reusable")
					}
					var deliveredAt time.Time
					if err := pool.QueryRow(ctx, `SELECT updated_at FROM gateway_retrieval_attempts WHERE request_id=$1`, id).Scan(&deliveredAt); err != nil {
						t.Fatal(err)
					}
					verified, err := admission.Verify(ctx, pool, id)
					if err != nil || verified.ReceiptID != approved.ReceiptID || verified.Used != 1 {
						t.Fatal("platform admission verification failed", err)
					}
					for _, expected := range []string{"Approval used to authorize retrieval", "Authorization event reference", verified.EventID,
						verified.UsedAt.Format("2 Jan 2006, 15:04:05 UTC"), "Access audit", "consumer-one", "<code>membership_status</code>", "Response written by GRAMStack",
						deliveredAt.UTC().Format("2 Jan 2006, 15:04:05 UTC"), "Signed access receipt", "1 of 1 authorized retrievals used"} {
						if !strings.Contains(card, expected) {
							t.Errorf("history audit is missing %q", expected)
						}
					}
					return
				}
			}
			t.Fatal("completed request missing from owner history")
		})
	}
	// Direct SDK retries remain denied, even with the trusted gateway certificate.
	var snapshot []byte
	var digest string
	if err := pool.QueryRow(ctx, `SELECT a.snapshot_bytes,r.request_digest FROM accepted_requests a JOIN access_requests r USING(request_id) WHERE request_id=$1`, id).Scan(&snapshot, &digest); err != nil {
		t.Fatal(err)
	}
	var thumb string
	if err := pool.QueryRow(ctx, `SELECT presenter_thumbprint FROM gateway_retrieval_attempts WHERE request_id=$1`, id).Scan(&thumb); err != nil {
		t.Fatal(err)
	}
	for _, attemptID := range []string{attempt, "new-attempt"} {
		raw, _ := json.Marshal(map[string]any{"attempt_id": attemptID, "snapshot": json.RawMessage(snapshot), "request_digest": digest, "grant": g.Grant, "presenter": map[string]string{"authenticated_client_id": "consumer-one", "certificate_thumbprint": thumb}})
		r, _ := http.NewRequest("POST", os.Getenv("GRAMSTACK_TEST_ADAPTER_URL")+"/internal/v1/retrievals", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Gramstack-Contract-Version", "0.3.0")
		if response, _ := liveResponse(t, serviceClient(t, "gateway", "lokos-adapter.gramstack.internal"), r); response.StatusCode != 409 {
			t.Fatal("direct SDK retry admitted source execution", response.StatusCode)
		}
	}
	if sourceReads(t) != before+1 {
		t.Fatal("SDK replay performed another native read")
	}
	// The mock's control files are operator-only, scoped to this temporary runtime.
	for _, mode := range []string{"offline", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			failed := cases[mode].id
			grant := liveGrant(t, base, failed, token, "fault-"+failed)
			path := filepath.Join(os.Getenv("GRAMSTACK_TEST_SOURCE_READ_GATE"), mode)
			if err := os.WriteFile(path, []byte(mode), 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			start := sourceReads(t)
			response, body := liveResponse(t, consumerClient, dataRequest(private, failed, grant.Grant))
			if response.StatusCode != 503 || strings.Contains(body, "membership_status") || strings.Contains(body, "12500") {
				t.Fatal("native failure released data", response.StatusCode)
			}
			awaitRequestState(t, base, token, failed, "DELIVERY_UNKNOWN")
			os.Remove(path)
			if response, _ = liveResponse(t, consumerClient, dataRequest(private, failed, grant.Grant)); response.StatusCode != 409 {
				t.Fatal("failure restored a spent disclosure", response.StatusCode)
			}
			if sourceReads(t) != start+1 {
				t.Fatal("native transport retried an ambiguous or failed read", mode, sourceReads(t)-start)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM evidence_receipts WHERE request_id=$1`, failed).Scan(&count); err != nil || count != 0 {
				t.Fatal("source failure invented release-ready evidence", err)
			}
		})
	}
	t.Run("custody outage", func(t *testing.T) {
		failed := cases["custody outage"].id
		grant := liveGrant(t, base, failed, token, "evidence-fault-"+failed)
		if _, err := pool.Exec(ctx, `CREATE TABLE test_evidence_faults(request_id text PRIMARY KEY); CREATE FUNCTION injected_evidence_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF EXISTS(SELECT 1 FROM test_evidence_faults WHERE request_id=NEW.request_id) THEN RAISE EXCEPTION 'test custody outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER injected_evidence_fault BEFORE INSERT ON evidence_receipts FOR EACH ROW EXECUTE FUNCTION injected_evidence_fault()`); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DROP TRIGGER injected_evidence_fault ON evidence_receipts; DROP FUNCTION injected_evidence_fault(); DROP TABLE test_evidence_faults`)
		if _, err := pool.Exec(ctx, `INSERT INTO test_evidence_faults VALUES($1)`, failed); err != nil {
			t.Fatal(err)
		}
		start := sourceReads(t)
		response, body := liveResponse(t, consumerClient, dataRequest(private, failed, grant.Grant))
		if response.StatusCode != 503 || strings.Contains(body, "membership_status") {
			t.Fatal("custody outage released source data", response.StatusCode)
		}
		awaitRequestState(t, base, token, failed, "DELIVERY_UNKNOWN")
		if response, _ = liveResponse(t, consumerClient, dataRequest(private, failed, grant.Grant)); response.StatusCode != 409 {
			t.Fatal("custody outage permitted replay", response.StatusCode)
		}
		if sourceReads(t) != start+1 {
			t.Fatal("custody outage repeated native retrieval")
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM evidence_receipts WHERE request_id=$1`, failed).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed custody partially committed", err)
		}
		var raw []byte
		if err := adapterPool.QueryRow(ctx, `SELECT protected_evidence FROM adapter_attempts WHERE request_id=$1`, failed).Scan(&raw); err != nil || len(raw) == 0 {
			t.Fatal("signed recovery evidence was lost", err)
		}
	})
	t.Run("both fields", func(t *testing.T) {
		next := cases["both fields"].id
		grant := liveGrant(t, base, next, token, "two-fields-"+next)
		start := sourceReads(t)
		response, body := liveResponse(t, consumerClient, dataRequest(private, next, grant.Grant))
		var result struct {
			RequestID string                     `json:"request_id"`
			Data      map[string]json.RawMessage `json:"data"`
			ReceiptID string                     `json:"receipt_id"`
		}
		if response.StatusCode != 200 || json.Unmarshal([]byte(body), &result) != nil || len(result.Data) != 2 || result.ReceiptID == "" {
			t.Fatal("full approved projection failed", response.StatusCode)
		}
		var savings struct {
			Amount   json.Number `json:"amount"`
			Currency string      `json:"currency"`
		}
		if json.Unmarshal(result.Data["savings_summary"], &savings) != nil || savings.Amount.String() != "12500.75" || savings.Currency != "INR" {
			t.Fatal("source amount or currency mapping changed")
		}
		if sourceReads(t) != start+1 {
			t.Fatal("two-field projection repeated a native read")
		}
	})
	withdraw := func(t *testing.T, p prepared) {
		t.Helper()
		record := readRecord(t, p.browser, base, p.id)
		form := url.Values{"csrf": {p.values.Get("csrf")}}
		r, _ := http.NewRequest("POST", base+"/consents/"+record.Consent.ConsentID+"/withdrawal", strings.NewReader(form.Encode()))
		r.Header.Set("Origin", base)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if response, _ := liveResponse(t, p.browser, r); response.StatusCode != 200 {
			t.Fatal("withdrawal failed", response.StatusCode)
		}
	}
	t.Run("withdraw before admission", func(t *testing.T) {
		p := cases["withdraw before admission"]
		grant := liveGrant(t, base, p.id, token, "withdraw-before-"+p.id)
		start := sourceReads(t)
		withdraw(t, p)
		awaitRequestState(t, base, token, p.id, "WITHDRAWN")
		if response, _ := liveResponse(t, consumerClient, dataRequest(private, p.id, grant.Grant)); response.StatusCode != 409 {
			t.Fatal("stopped request admitted data", response.StatusCode)
		}
		if sourceReads(t) != start {
			t.Fatal("pre-admission withdrawal reached native data")
		}
	})
	t.Run("withdraw after admission", func(t *testing.T) {
		p := cases["withdraw after admission"]
		grant := liveGrant(t, base, p.id, token, "withdraw-after-"+p.id)
		dir := os.Getenv("GRAMSTACK_TEST_SOURCE_READ_GATE")
		for _, name := range []string{"block", "entered", "release"} {
			os.Remove(filepath.Join(dir, name))
			defer os.Remove(filepath.Join(dir, name))
		}
		if err := os.WriteFile(filepath.Join(dir, "block"), []byte("block"), 0600); err != nil {
			t.Fatal(err)
		}
		start := sourceReads(t)
		done := make(chan int, 1)
		go func() {
			response, err := consumerClient.Do(dataRequest(private, p.id, grant.Grant))
			if err != nil {
				done <- 0
				return
			}
			defer response.Body.Close()
			io.Copy(io.Discard, response.Body)
			done <- response.StatusCode
		}()
		entered := false
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if _, err := os.Stat(filepath.Join(dir, "entered")); err == nil {
				entered = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !entered {
			os.WriteFile(filepath.Join(dir, "release"), []byte("release"), 0600)
			t.Fatal("native admission cutoff not reached")
		}
		cancel, _ := http.NewRequest("POST", base+"/v1/access-requests/"+p.id+"/cancel", nil)
		cancel.Header.Set("Authorization", "Bearer "+token)
		if response, _ := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, cancel); response.StatusCode != 409 {
			t.Error("post-admission cancellation hid the cutoff", response.StatusCode)
		}
		withdraw(t, p)
		if err := os.WriteFile(filepath.Join(dir, "release"), []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case code := <-done:
			if code != 200 {
				t.Fatal("admitted delivery was incorrectly reopened or denied", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("admitted delivery did not finish")
		}
		awaitRequestState(t, base, token, p.id, "COMPLETED")
		if response, _ := liveResponse(t, consumerClient, dataRequest(private, p.id, grant.Grant)); response.StatusCode != 409 {
			t.Fatal("withdrawn consumed slot was replayed", response.StatusCode)
		}
		if sourceReads(t) != start+1 {
			t.Fatal("post-admission stop caused another native read")
		}
	})
	t.Run("distinct request capacity", func(t *testing.T) {
		checkDistinctRetrievalCapacity(t, pool, base, private, token, consumerClient)
	})
	// Persisted operational rows contain metadata, never native resource plaintext.
	for _, query := range []string{`SELECT to_jsonb(a)::text FROM gateway_retrieval_attempts a WHERE request_id=$1`, `SELECT to_jsonb(a)::text FROM evidence_receipts a WHERE request_id=$1`} {
		var raw string
		if err := pool.QueryRow(ctx, query, id).Scan(&raw); err != nil || strings.Contains(raw, `"data"`) || strings.Contains(raw, "12500.75") {
			t.Fatal("gateway/evidence persisted resource plaintext", err)
		}
	}
	raw, err := json.Marshal(retrievalCheckpoint{id, g.Grant, approved.ReceiptID, attempt, sourceReads(t), version})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("GRAMSTACK_TEST_RETRIEVAL_RESUME"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: consumer certificate -> durable gateway dispatch -> SDK checks/reservation -> exactly one native read -> projected resource -> real custody -> response; concurrent/replayed/native/custody failures do not re-fetch or release")
}
func TestActualProtectedRetrievalRestart(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_RETRIEVAL") != "1" {
		t.Skip("requires restarted protected data stack")
	}
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_TEST_RETRIEVAL_RESUME"))
	var saved retrievalCheckpoint
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.RequestID == "" {
		t.Fatal("missing retrieval checkpoint", err)
	}
	response, body := liveResponse(t, serviceClient(t, "consumer-one", "gateway.gramstack.internal"), dataRequest(os.Getenv("GRAMSTACK_TEST_AUTHZ_URL"), saved.RequestID, saved.Grant))
	if response.StatusCode != 409 || strings.Contains(body, "membership_status") {
		t.Fatal("restart replay released data", response.StatusCode)
	}
	if sourceReads(t) != saved.Reads {
		t.Fatal("gateway/SDK/native restart repeated a native call")
	}
	if got := awaitRequestState(t, os.Getenv("GRAMSTACK_TEST_GATEWAY_URL"), liveToken(t), saved.RequestID, "COMPLETED"); got != saved.Version {
		t.Fatal("restart changed completed request")
	}
	pool, _ := database(t)
	var receipt string
	if err = pool.QueryRow(context.Background(), `SELECT receipt_id FROM gateway_retrieval_attempts WHERE request_id=$1`, saved.RequestID).Scan(&receipt); err != nil || receipt != saved.ReceiptID {
		t.Fatal("restart lost receipt binding", err)
	}
	t.Log("PASS: gateway, SDK adapter and native source restart preserved the spent dispatch, signed receipt and request version; no cached response or repeated fetch")
}
