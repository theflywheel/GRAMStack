package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthorityClientPinsContractAndRejectsRedirects(t *testing.T) {
	var calls atomic.Int32
	var mode atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1000); w.WriteHeader(500) }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.Header.Get("Gramstack-Contract-Version") != CoreVersion || r.Header.Get("Content-Type") != "application/json" {
			t.Error("authority request did not pin contract")
		}
		switch mode.Load() {
		case 1:
			http.Redirect(w, r, target.URL, 307)
			return
		case 2:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Gramstack-Contract-Version", CoreVersion)
			w.Write([]byte(`{"redemption_id":"one","redemption_id":"two"}`))
			return
		case 3:
			w.WriteHeader(409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Gramstack-Contract-Version", CoreVersion)
		if r.URL.Path == "/internal/v1/grants/redeem" {
			var input RedemptionRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.AttemptID != "attempt-1" {
				t.Error("redemption body changed")
			}
			json.NewEncoder(w).Encode(Reservation{ID: "reservation-1", AttemptID: input.AttemptID, First: true, ReservedAt: time.Now()})
			return
		}
		if r.URL.Path != "/internal/v1/evidence" {
			t.Error("unexpected authority route")
		}
		var envelope SignedEnvelope
		if json.NewDecoder(r.Body).Decode(&envelope) != nil || envelope.JWS != "signed-evidence" {
			t.Error("evidence envelope changed")
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(EvidenceAcknowledgement{ReceiptID: "receipt-1", Digest: Digest([]byte(envelope.JWS)), AcceptedAt: time.Now(), AnchorStatus: "pending"})
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Timeout = 3 * time.Second
	client, err := NewAuthorityClient(server.URL, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := client.Redeem(context.Background(), RedemptionRequest{AttemptID: "attempt-1"}); err != nil || !v.First {
		t.Fatal("valid redemption response rejected", err)
	}
	if v, err := client.Record(context.Background(), "signed-evidence"); err != nil || v.ReceiptID != "receipt-1" {
		t.Fatal("valid evidence ack rejected", err)
	}
	for _, m := range []int32{1, 2, 3} {
		mode.Store(m)
		if _, err = client.Redeem(context.Background(), RedemptionRequest{AttemptID: "attempt-1"}); err == nil {
			t.Fatal("unsafe response accepted", m)
		}
	}
	if calls.Load() != 5 {
		t.Fatal("authority request redirected or retried", calls.Load())
	}
	if _, err = NewAuthorityClient("http://127.0.0.1:1234", httpClient); err == nil {
		t.Fatal("cleartext authority accepted")
	}
}
