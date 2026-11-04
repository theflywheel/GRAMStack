package conformance

import (
	"crypto/ecdsa"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

// CheckDiscovery checks the exact public metadata of a running test adapter
// against a fixture defined independently of its HTTP responses. Use synthetic
// data and an origin without a trailing slash.
func CheckDiscovery(t *testing.T, origin string, expected adapter.Manifest, keys map[string]*ecdsa.PublicKey) {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	get := func(path string, target any) {
		t.Helper()
		response, err := client.Get(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" || response.StatusCode != http.StatusOK ||
			response.Header.Get("Cache-Control") != "no-store" ||
			response.Header.Get("Gramstack-Contract-Version") != adapter.CoreVersion ||
			response.Header.Get(adapter.DiscoveryVersionHeader) != adapter.DiscoveryVersion {
			t.Fatalf("%s has invalid discovery status or headers: %d", path, response.StatusCode)
		}
		const limit = 1 << 20
		body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if err != nil || len(body) > limit || adapter.Decode(body, target) != nil {
			t.Fatalf("%s returned invalid, oversized or unexpected metadata", path)
		}
	}

	var manifest adapter.Manifest
	get(adapter.ManifestPath, &manifest)
	if !reflect.DeepEqual(manifest, expected) {
		t.Fatal("discovery manifest differs from the approved fixture")
	}

	var set struct {
		Keys []adapter.PublicJWK `json:"keys"`
	}
	get(adapter.AdapterKeysPath, &set)
	if len(set.Keys) != len(keys) || len(set.Keys) != len(expected.SigningKeyIDs) || len(set.Keys) == 0 {
		t.Fatal("discovery key count differs from the approved fixture")
	}
	seen := make(map[string]bool, len(set.Keys))
	for _, key := range set.Keys {
		approved := keys[key.ID]
		if approved == nil || seen[key.ID] || key.KeyType != "EC" || key.Curve != "P-256" ||
			key.Algorithm != "ES256" || key.Use != "sig" ||
			key.X != base64.RawURLEncoding.EncodeToString(approved.X.FillBytes(make([]byte, 32))) ||
			key.Y != base64.RawURLEncoding.EncodeToString(approved.Y.FillBytes(make([]byte, 32))) {
			t.Fatalf("invalid, duplicate or unapproved discovery key %q", key.ID)
		}
		seen[key.ID] = true
	}
	for _, id := range expected.SigningKeyIDs {
		if !seen[id] {
			t.Fatalf("advertised key %q is missing", id)
		}
	}
	for _, path := range []string{"/internal/v1/subject-verifications", "/internal/v1/retrievals"} {
		response, err := client.Post(origin+path, "application/json", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("public listener did not hide %s: %d", path, response.StatusCode)
		}
	}
}
