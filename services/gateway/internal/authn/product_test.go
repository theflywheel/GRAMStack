package authn

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type productResult struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	RequestID string `json:"request_id"`
	Result    *struct {
		ReceiptID string                     `json:"receipt_id"`
		Data      map[string]json.RawMessage `json:"data"`
	} `json:"result"`
}
type productCheckpoint struct {
	ID, Receipt, CSRF string
	Cookie            http.Cookie
}

func productCall(t *testing.T, browser *http.Client, method, path, csrf string) (int, productResult) {
	t.Helper()
	origin := os.Getenv("GRAMSTACK_TEST_PRODUCT_URL")
	r, _ := http.NewRequest(method, origin+path, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", csrf)

	response, raw := liveResponse(t, browser, r)
	var value productResult
	if json.Unmarshal([]byte(raw), &value) != nil || strings.Contains(raw, "isolated-synthetic-secret") || strings.Contains(raw, `"grant":`) {
		t.Fatal("invalid product response", response.StatusCode)
	}
	return response.StatusCode, value
}
func TestActualSakhiCreditProduct(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_PRODUCT") != "1" {
		t.Skip("running independent SakhiCredit product required")
	}
	origin := os.Getenv("GRAMSTACK_TEST_PRODUCT_URL")
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, Timeout: 35 * time.Second}
	r, _ := http.NewRequest("GET", origin+"/api/session", nil)
	response, raw := liveResponse(t, browser, r)
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if response.StatusCode != 200 || json.Unmarshal([]byte(raw), &session) != nil || session.CSRF == "" {
		t.Fatal("product session missing")
	}
	csrf := session.CSRF
	// Create each case with an independent product idempotency key.
	create := func(key string) productResult {
		r, _ := http.NewRequest("POST", origin+"/api/applications", strings.NewReader(`{"member_reference":"member-12345","include_savings":true}`))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		response, raw := liveResponse(t, browser, r)
		var app productResult
		if response.StatusCode != 201 || json.Unmarshal([]byte(raw), &app) != nil || app.State != "WAITING" || app.RequestID == "" {
			t.Fatal("product failed to create real platform request", response.StatusCode, raw)
		}
		return app
	}
	happy := create("product-happy")
	_, portal, values, _ := reviewExistingRequest(t, base, happy.RequestID)
	values.Set("decision", "approve")
	values.Set("confirmation", "yes")
	if response, _ := liveResponse(t, portal, decisionRequest(base, happy.RequestID, values)); response.StatusCode != 200 {
		t.Fatal("product consent approval failed")
	}
	awaitRequestState(t, base, liveToken(t), happy.RequestID, "CONSENTED")
	if status, app := productCall(t, browser, "POST", "/api/applications/"+happy.ID+"/refresh", csrf); status != 200 || app.State != "READY" {
		t.Fatal("product approval progress missing", status, app.State)
	}
	before := sourceReads(t)
	status, completed := productCall(t, browser, "POST", "/api/applications/"+happy.ID+"/retrieve", csrf)
	if status != 200 || completed.State != "COMPLETE" || completed.Result == nil || string(completed.Result.Data["savings_summary"]) != `{"amount":12500.75,"currency":"INR"}` || completed.Result.ReceiptID == "" || sourceReads(t) != before+1 {
		t.Fatal("product protected result failed", status, completed.State)
	}
	if status, again := productCall(t, browser, "POST", "/api/applications/"+happy.ID+"/retrieve", csrf); status != 200 || again.Result == nil || sourceReads(t) != before+1 {
		t.Fatal("product retry repeated disclosure")
	}
	rejected := create("product-rejected")
	_, rejectBrowser, rejectValues, _ := reviewExistingRequest(t, base, rejected.RequestID)
	rejectValues.Set("decision", "reject")
	if response, _ := liveResponse(t, rejectBrowser, decisionRequest(base, rejected.RequestID, rejectValues)); response.StatusCode != 200 {
		t.Fatal("rejection failed")
	}
	awaitRequestState(t, base, liveToken(t), rejected.RequestID, "REJECTED")
	if status, app := productCall(t, browser, "POST", "/api/applications/"+rejected.ID+"/refresh", csrf); status != 200 || app.State != "BLOCKED" {
		t.Fatal("product rejection not surfaced", status, app.State)
	}
	cancelled := create("product-cancelled")
	if status, app := productCall(t, browser, "POST", "/api/applications/"+cancelled.ID+"/cancel", csrf); status != 200 || app.State != "CANCELLED" {
		t.Fatal("product cancellation failed", status, app.State)
	}
	if sourceReads(t) != before+1 {
		t.Fatal("stopped applications fetched source data")
	}
	offline := create("product-offline")
	_, offlineBrowser, offlineValues, _ := reviewExistingRequest(t, base, offline.RequestID)
	offlineValues.Set("decision", "approve")
	offlineValues.Set("confirmation", "yes")
	if response, _ := liveResponse(t, offlineBrowser, decisionRequest(base, offline.RequestID, offlineValues)); response.StatusCode != 200 {
		t.Fatal("outage case approval failed")
	}
	awaitRequestState(t, base, liveToken(t), offline.RequestID, "CONSENTED")
	if status, app := productCall(t, browser, "POST", "/api/applications/"+offline.ID+"/refresh", csrf); status != 200 || app.State != "READY" {
		t.Fatal("outage case not ready")
	}
	gate := filepath.Join(os.Getenv("GRAMSTACK_TEST_SOURCE_READ_GATE"), "offline")
	if err := os.WriteFile(gate, []byte("test fault"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(gate) })
	status, failed := productCall(t, browser, "POST", "/api/applications/"+offline.ID+"/retrieve", csrf)
	if err := os.Remove(gate); err != nil {
		t.Fatal(err)
	}
	if status != 200 || failed.State != "UNKNOWN" || failed.Result != nil {
		t.Fatal("product outage was not explicit", status, failed.State)
	}
	reads := sourceReads(t)
	if status, _ := productCall(t, browser, "POST", "/api/applications/"+offline.ID+"/retrieve", csrf); status != 409 || sourceReads(t) != reads {
		t.Fatal("uncertain product retried source")
	}
	u, _ := url.Parse(origin)
	cookies := browser.Jar.Cookies(u)
	if len(cookies) != 1 {
		t.Fatal("unexpected product cookies")
	}
	checkpoint := productCheckpoint{happy.ID, completed.Result.ReceiptID, csrf, *cookies[0]}
	rawBytes, _ := json.Marshal(checkpoint)
	if err := os.WriteFile(os.Getenv("GRAMSTACK_TEST_PRODUCT_RESUME"), rawBytes, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: independent product -> real Hydra request -> identity/consent -> sender-bound single retrieval -> protected cached result; rejection, cancellation and source outage surfaced without reopening a fetch")
}
func TestActualSakhiCreditProductRestart(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_PRODUCT") != "1" {
		t.Skip("restarted independent product required")
	}
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_TEST_PRODUCT_RESUME"))
	var saved productCheckpoint
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.ID == "" {
		t.Fatal("product restart checkpoint missing", err)
	}
	origin := os.Getenv("GRAMSTACK_TEST_PRODUCT_URL")
	u, _ := url.Parse(origin)
	jar, _ := cookiejar.New(nil)
	saved.Cookie.Path = "/"
	jar.SetCookies(u, []*http.Cookie{&saved.Cookie})
	browser := &http.Client{Jar: jar, Timeout: 35 * time.Second}
	before := sourceReads(t)
	status, app := productCall(t, browser, "POST", "/api/applications/"+saved.ID+"/retrieve", saved.CSRF)
	if status != 200 || app.State != "COMPLETE" || app.Result == nil || app.Result.ReceiptID != saved.Receipt || sourceReads(t) != before {
		t.Fatal("product restart fetched again or lost approved result", status, app.State)
	}
	t.Log("PASS: product and platform process restart preserved the encrypted approved result, browser ownership and receipt without another native read")
}
