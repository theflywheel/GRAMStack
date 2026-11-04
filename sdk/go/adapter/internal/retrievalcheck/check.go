package retrievalcheck

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Fixture supplies fresh request bodies on each call. Valid and InvalidGrant
// carry the configured gateway TLS identity. InvalidGrant has a rejected grant
// for the same request. Replay changes only the attempt ID.
type Fixture struct {
	Handler      http.Handler
	Valid        func() *http.Request
	InvalidGrant func() *http.Request
	Replay       func() *http.Request
	Reads        func() int
	Acknowledged func() int
	Field        string
	Value        json.RawMessage
	ReceiptID    string
}

// Run checks rejection before source work, one projected release, and replay
// refusal using synthetic data and a freshly configured handler.
func Run(t *testing.T, f Fixture) {
	t.Helper()
	reads, acknowledged := f.Reads(), f.Acknowledged()
	serve := func(request *http.Request) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		f.Handler.ServeHTTP(response, request)
		return response
	}
	rejected := func(request *http.Request, status int, code string, expectedReads, expectedAcknowledgements int) {
		t.Helper()
		response := serve(request)
		var body map[string]map[string]json.RawMessage
		if response.Code != status || json.Unmarshal(response.Body.Bytes(), &body) != nil ||
			len(body) != 1 || string(body["error"]["code"]) != `"`+code+`"` ||
			f.Reads() != expectedReads || f.Acknowledged() != expectedAcknowledgements {
			t.Fatalf("expected HTTP %d without disclosure or protected work, got %d", status, response.Code)
		}
	}

	request := f.Valid()
	request.TLS = nil
	request.Header.Set("X-Forwarded-Client-Cert", "forged gateway identity")
	rejected(request, http.StatusForbidden, "UNTRUSTED_SERVICE", reads, acknowledged)
	rejected(f.InvalidGrant(), http.StatusForbidden, "AUTHORIZATION_DENIED", reads, acknowledged)

	response := serve(f.Valid())
	var result struct {
		Data      map[string]json.RawMessage `json:"data"`
		ReceiptID string                     `json:"receipt_id"`
	}
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
		json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Data) != 1 ||
		string(result.Data[f.Field]) != string(f.Value) || result.ReceiptID != f.ReceiptID ||
		f.Reads() != reads+1 || f.Acknowledged() != acknowledged+1 {
		t.Fatalf("expected one projected disclosure and one custody acknowledgement, got HTTP %d", response.Code)
	}
	rejected(f.Valid(), http.StatusConflict, "ATTEMPT_ALREADY_CLAIMED", reads+1, acknowledged+1)
	rejected(f.Replay(), http.StatusConflict, "ATTEMPT_ALREADY_CLAIMED", reads+1, acknowledged+1)
}
