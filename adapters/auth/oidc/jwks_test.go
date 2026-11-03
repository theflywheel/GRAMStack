package oidcadapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicJWKTransformationIsBoundedAndExact(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"public keys", `{"keys":[{"kty":"RSA","kid":"id","alg":"PS256","use":"sig","n":"public-modulus","e":"AQAB","x5c":["invalid-certificate"],"x5t":"old-thumbprint"}]}`, true},
		{"duplicate fields", `{"keys":[],"keys":[{}]}`, false},
		{"no keys", `{"keys":[]}`, false},
		{"broken JSON", `{"keys":`, false},
		{"oversize", strings.Repeat(" ", (1<<20)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			transport := publicJWKTransport{endpoint: server.URL + "/jwks", base: http.DefaultTransport}
			request, err := http.NewRequestWithContext(context.Background(), "GET", server.URL+"/jwks", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(request)
			if !tc.valid {
				if err == nil {
					response.Body.Close()
					t.Fatal("invalid key set accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var set struct {
				Keys []map[string]any `json:"keys"`
			}
			if json.NewDecoder(response.Body).Decode(&set) != nil {
				t.Fatal("malformed transformed set")
			}
			key := set.Keys[0]
			if key["x5c"] != nil || key["x5t"] != nil || key["n"] != "public-modulus" || key["kid"] != "id" || key["alg"] != "PS256" || key["use"] != "sig" {
				t.Fatal("transform changed signature trust fields")
			}
			request, err = http.NewRequestWithContext(context.Background(), "GET", server.URL+"/unrelated", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err = transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			raw, err := io.ReadAll(response.Body)
			if err != nil || string(raw) != tc.body {
				t.Fatal("unrelated endpoint response modified")
			}
		})
	}
}
