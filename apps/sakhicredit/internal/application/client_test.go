package application

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResultContractRejectsScopeAndRepresentationChanges(t *testing.T) {
	good := Result{"request-1", map[string]json.RawMessage{"membership_status": json.RawMessage(`"active"`), "savings_summary": json.RawMessage(`{"amount":12500.75,"currency":"INR"}`)}, "receipt-1"}
	input := Input{"member-12345", true}
	if err := good.validate("request-1", input); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*Result){
		func(r *Result) { r.RequestID = "other" }, func(r *Result) { r.ReceiptID = "" },
		func(r *Result) { r.Data["private_field"] = json.RawMessage(`true`) },
		func(r *Result) { delete(r.Data, "savings_summary") },
		func(r *Result) { r.Data["membership_status"] = json.RawMessage(`"unknown"`) },
		func(r *Result) { r.Data["savings_summary"] = json.RawMessage(`{"amount":12500.75,"currency":"USD"}`) },
	} {
		changed := good
		changed.Data = maps.Clone(good.Data)
		edit(&changed)
		if changed.validate("request-1", input) == nil {
			t.Fatal("invalid disclosure accepted")
		}
	}
	for _, amount := range []string{`-1`, `"12500.75"`, `null`, `1e1000`} {
		changed := good
		changed.Data = maps.Clone(good.Data)
		changed.Data["savings_summary"] = json.RawMessage(`{"amount":` + amount + `,"currency":"INR"}`)
		if changed.validate("request-1", input) == nil {
			t.Fatal("invalid rupee representation accepted", amount)
		}
	}
	// The platform contract allows nonnegative rupees without a two-decimal cap.
	for _, amount := range []string{`0`, `12500.751`, `1e100`} {
		value := good
		value.Data = maps.Clone(good.Data)
		value.Data["savings_summary"] = json.RawMessage(`{"amount":` + amount + `,"currency":"INR"}`)
		if value.validate("request-1", input) != nil {
			t.Fatal("valid platform amount rejected", amount)
		}
	}
	if good.validate("request-1", Input{"member-12345", false}) == nil {
		t.Fatal("extra unrequested field accepted")
	}
}
func TestClientOriginsRedirectsAndRevokedTokenRecovery(t *testing.T) {
	var tokens, requests, redirects atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	defer target.Close()
	var mode atomic.Value
	mode.Store("unauthorized")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			tokens.Add(1)
			writeJSON(w, 200, map[string]any{"access_token": "secret-token", "token_type": "bearer", "expires_in": 300})
			return
		}
		requests.Add(1)
		switch mode.Load().(string) {
		case "unauthorized":
			w.WriteHeader(401)
		case "redirect":
			http.Redirect(w, r, target.URL, 302)
		default:
			w.Header().Set("Gramstack-Contract-Version", "0.3.0")
			writeJSON(w, 200, PlatformStatus{RequestID: "request-1", Status: "CONSENT_PENDING", Version: 3, NextAction: "review_consent", ExpiresAt: time.Unix(1900000000, 0).UTC()})
		}
	}))
	defer server.Close()
	ca, cert, key, _ := certificates(t)
	config := ClientConfig{PublicOrigin: server.URL, DataOrigin: "https://private.example", TokenURL: server.URL + "/oauth2/token", ClientID: "consumer-one", ClientSecret: "test-secret", DataServerName: "gateway.test", CA: ca, Certificate: cert, Key: key, AllowLoopback: true}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(context.Background(), "request-1", false); err == nil || tokens.Load() != 1 || requests.Load() != 1 {
		t.Fatal("unauthorized call replayed")
	}
	mode.Store("success")
	if _, err := client.Status(context.Background(), "request-1", false); err != nil || tokens.Load() != 2 || requests.Load() != 2 {
		t.Fatal("revoked token was not replaced on explicit retry", err)
	}
	mode.Store("redirect")
	if _, err := client.Status(context.Background(), "request-1", false); err == nil || redirects.Load() != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
	for _, edit := range []func(*ClientConfig){
		func(c *ClientConfig) { c.AllowLoopback = false },
		func(c *ClientConfig) { c.PublicOrigin = "http://remote.example" },
		func(c *ClientConfig) { c.PublicOrigin += "/arbitrary" },
		func(c *ClientConfig) { c.PublicOrigin += "?" },
		func(c *ClientConfig) { c.TokenURL = "https://user:password@example.test/token" },
		func(c *ClientConfig) { c.DataOrigin = server.URL },
		func(c *ClientConfig) { c.Key = []byte("wrong") },
	} {
		changed := config
		edit(&changed)
		if _, err := NewClient(changed); err == nil {
			t.Fatal("untrusted client configuration accepted")
		}
	}
}
func TestStrictProductInput(t *testing.T) {
	for _, raw := range []string{`{"member_reference":"member-12345","include_savings":true,"include_savings":false}`, `{"member_reference":"member-12345"}`, `{"Member_Reference":"member-12345","include_savings":true}`, `{"member_reference":"member-12345","include_savings":true,"scope":"all"}`} {
		var input Input
		if decode([]byte(raw), &input) == nil {
			t.Fatal("ambiguous product input accepted", raw)
		}
	}
	for _, member := range []string{"", "member-12345/other", strings.Repeat("x", 1000)} {
		if (Input{member, true}).validate() == nil {
			t.Fatal("invalid locator accepted")
		}
	}
}
