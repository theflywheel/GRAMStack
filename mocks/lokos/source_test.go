package lokosmock

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNativeVerificationPolicyAndServiceBoundary(t *testing.T) {
	bindings := map[string]string{"member-12345": "pairwise-sunita", "member-67890": "pairwise-rani"}
	source, err := New(Config{Issuer: "https://identity.example", AdapterIdentity: "adapter.example", DemoEnabled: true, Bindings: bindings})
	if err != nil {
		t.Fatal(err)
	}
	bindings["member-12345"] = "pairwise-rani" // Mutating the caller's map cannot change source policy.
	good := func() map[string]any {
		return map[string]any{"hint": map[string]string{"type": "member_reference", "value": "member-12345"}, "principal": map[string]any{"context_id": "context-1", "issuer": "https://identity.example", "provider_subject": "pairwise-sunita", "authentication_transaction_id": "authentication-1", "authenticated_at": time.Now().Add(-time.Second), "expires_at": time.Now().Add(time.Minute)}}
	}
	for _, tc := range []struct {
		name, role string
		edit       func(map[string]any)
		raw        string
		status     int
	}{
		{name: "approved fixture", role: "adapter.example", status: 200},
		{name: "wrong person", role: "adapter.example", edit: func(v map[string]any) { v["principal"].(map[string]any)["provider_subject"] = "pairwise-rani" }, status: 403},
		{name: "untrusted issuer", role: "adapter.example", edit: func(v map[string]any) { v["principal"].(map[string]any)["issuer"] = "https://other.example" }, status: 403},
		{name: "expired", role: "adapter.example", edit: func(v map[string]any) { v["principal"].(map[string]any)["expires_at"] = time.Now().Add(-time.Second) }, status: 403},
		{name: "stale authentication", role: "adapter.example", edit: func(v map[string]any) {
			v["principal"].(map[string]any)["authenticated_at"] = time.Now().Add(-6 * time.Minute)
		}, status: 403},
		{name: "locator alone", role: "adapter.example", raw: `{"hint":{"type":"member_reference","value":"member-12345"}}`, status: 403},
		{name: "ambiguous input", role: "adapter.example", raw: `{"principal":{},"principal":{}}`, status: 400},
		{name: "unknown input", role: "adapter.example", raw: `{"unexpected":true}`, status: 400},
		{name: "direct gateway", role: "gateway.example", status: 403},
		{name: "no transport proof", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := good()
			if tc.edit != nil {
				tc.edit(value)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if tc.raw != "" {
				raw = []byte(tc.raw)
			}
			request := httptest.NewRequest("POST", "https://native.example/native/v1/verify-subject", strings.NewReader(string(raw)))
			if tc.role != "" {
				certificate := &x509.Certificate{DNSNames: []string{tc.role}}
				request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}}}
			}
			response := httptest.NewRecorder()
			source.Handler().ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("got %d want %d", response.Code, tc.status)
			}
			if tc.status == 200 && (strings.Contains(response.Body.String(), "savings") || !strings.Contains(response.Body.String(), `"demo":true`)) {
				t.Fatal("source verification exposed data or concealed its demo status")
			}
		})
	}
	if _, err = New(Config{Issuer: "https://identity.example", AdapterIdentity: "adapter.example", Bindings: bindings}); err == nil {
		t.Fatal("source did not require explicit demo mode")
	}
	if _, err = New(Config{Issuer: "https://identity.example", AdapterIdentity: "adapter.example", DemoEnabled: true}); err == nil {
		t.Fatal("source did not require explicit subject bindings")
	}
	// The health check still requires the adapter's service identity.
	request := httptest.NewRequest(http.MethodGet, "https://native.example/healthz", nil)
	response := httptest.NewRecorder()
	source.Handler().ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("native health exposed a service bypass")
	}
}
