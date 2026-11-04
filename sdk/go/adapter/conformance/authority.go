package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

// AuthorityClient is the adapter-facing redemption and evidence client.
type AuthorityClient interface {
	adapter.Authority
	adapter.EvidenceCustodian
}

// CheckAuthorityClient verifies a client's HTTPS wire contract against a
// fictional authority. The factory must use the supplied test TLS client.
func CheckAuthorityClient(t *testing.T, factory func(string, *http.Client) (AuthorityClient, error)) {
	t.Helper()
	var calls atomic.Int32
	var mode atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1000); w.WriteHeader(500) }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.Header.Get("Gramstack-Contract-Version") != adapter.CoreVersion || r.Header.Get("Content-Type") != "application/json" {
			t.Error("authority request did not pin contract")
		}
		success := http.StatusOK
		if r.URL.Path == "/internal/v1/evidence" {
			success = http.StatusCreated
		}
		switch mode.Load() {
		case 1:
			http.Redirect(w, r, target.URL, 307)
			return
		case 2:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Gramstack-Contract-Version", adapter.CoreVersion)
			w.WriteHeader(success)
			w.Write([]byte(`{"redemption_id":"one","redemption_id":"two"}`))
			return
		case 403, 404, 409, 410, 503:
			w.WriteHeader(int(mode.Load()))
			return
		case 4:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Gramstack-Contract-Version", "0.0.0")
			w.WriteHeader(success)
			w.Write([]byte(`{"redemption_id":"reservation-1"}`))
			return
		case 5:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Gramstack-Contract-Version", adapter.CoreVersion)
			w.WriteHeader(success)
			w.Write([]byte(strings.Repeat(" ", 32769)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Gramstack-Contract-Version", adapter.CoreVersion)
		if r.URL.Path == "/internal/v1/grants/redeem" {
			var input adapter.RedemptionRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.AttemptID != "attempt-1" {
				t.Error("redemption body changed")
			}
			json.NewEncoder(w).Encode(adapter.Reservation{ID: "reservation-1", AttemptID: input.AttemptID, First: true, ReservedAt: time.Now()})
			return
		}
		if r.URL.Path != "/internal/v1/evidence" {
			t.Error("unexpected authority route")
		}
		var envelope adapter.SignedEnvelope
		if json.NewDecoder(r.Body).Decode(&envelope) != nil || envelope.JWS != "signed-evidence" {
			t.Error("evidence envelope changed")
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(adapter.EvidenceAcknowledgement{ReceiptID: "receipt-1", Digest: adapter.Digest([]byte(envelope.JWS)), AcceptedAt: time.Now(), AnchorStatus: "pending"})
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Timeout = 3 * time.Second
	client, err := factory(server.URL, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := client.Redeem(context.Background(), adapter.RedemptionRequest{AttemptID: "attempt-1"}); err != nil || !v.First || v.ID != "reservation-1" || v.AttemptID != "attempt-1" || v.ReservedAt.IsZero() {
		t.Fatal("valid redemption response rejected", err)
	}
	if v, err := client.Record(context.Background(), "signed-evidence"); err != nil || v.ReceiptID != "receipt-1" || v.AnchorStatus != "pending" || v.Digest != adapter.Digest([]byte("signed-evidence")) || v.AcceptedAt.IsZero() {
		t.Fatal("valid evidence ack rejected", err)
	}
	cases := []struct {
		mode int32
		want error
	}{
		{1, adapter.ErrRetrievalDependency},
		{2, adapter.ErrRetrievalDependency},
		{4, adapter.ErrRetrievalDependency},
		{5, adapter.ErrRetrievalDependency},
		{403, adapter.ErrGrant},
		{404, adapter.ErrGrant},
		{409, adapter.ErrAttemptExists},
		{410, adapter.ErrGrantExpired},
		{503, adapter.ErrRetrievalDependency},
	}
	for _, tc := range cases {
		mode.Store(tc.mode)
		if _, err = client.Redeem(context.Background(), adapter.RedemptionRequest{AttemptID: "attempt-1"}); !errors.Is(err, tc.want) {
			t.Fatalf("redemption failure %d: got %v, want %v", tc.mode, err, tc.want)
		}
		if _, err = client.Record(context.Background(), "signed-evidence"); !errors.Is(err, tc.want) {
			t.Fatalf("evidence failure %d: got %v, want %v", tc.mode, err, tc.want)
		}
	}
	if calls.Load() != int32(2+2*len(cases)) {
		t.Fatal("authority request redirected or retried", calls.Load())
	}
	if _, err = factory("http://127.0.0.1:1234", httpClient); err == nil {
		t.Fatal("cleartext authority accepted")
	}
}
