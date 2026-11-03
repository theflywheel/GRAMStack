package discovery

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func fixture(t *testing.T) (Manifest, map[string]any, Descriptor) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	resources := []Resource{{Name: "shg.membership", Version: "1", Fields: []string{"membership_status", "savings_summary"}}}
	m := Manifest{AdapterID: "lokos-adapter", Registry: "lokos", Audience: "lokos-adapter", ContractVersion: "0.3.0", Authentication: "mtls", SigningKeyIDs: []string{"source-key"}, VerificationMethods: []string{"registry.approved-v1"}, Capabilities: []string{"atomic-redemption", "evidence-before-release"}, Resources: resources}
	set := map[string]any{"keys": []PublicJWK{{KeyType: "EC", Curve: "P-256", X: base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), Y: base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))), ID: "source-key", Algorithm: "ES256", Use: "sig"}}}
	d := Descriptor{Version: Version, AdapterID: m.AdapterID, Registry: m.Registry, Audience: m.Audience, VerificationMethod: m.VerificationMethods[0], Resources: []Resource{{Name: resources[0].Name, Version: "1", Fields: slices.Clone(resources[0].Fields)}}, Keys: map[string]*ecdsa.PublicKey{"source-key": &key.PublicKey}}
	return m, set, d
}
func metadataHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(VersionHeader, Version)
	w.Header().Set("Gramstack-Contract-Version", "0.3.0")
	w.Header().Set("Cache-Control", "no-store")
}
func TestDiscoveryRejectsMetadataAndKeySubstitution(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(map[string]any, map[string]any)
	}{
		{"valid", func(map[string]any, map[string]any) {}},
		{"adapter", func(m, k map[string]any) { m["adapter_id"] = "other" }},
		{"registry", func(m, k map[string]any) { m["registry"] = "other" }},
		{"audience", func(m, k map[string]any) { m["audience"] = "other" }},
		{"contract", func(m, k map[string]any) { m["contract_version"] = "9.0.0" }},
		{"authentication", func(m, k map[string]any) { m["authentication"] = "none" }},
		{"capability", func(m, k map[string]any) { m["capabilities"] = []string{"atomic-redemption"} }},
		{"method", func(m, k map[string]any) { m["verification_methods"] = []string{"unknown"} }},
		{"unknown key", func(m, k map[string]any) { m["signing_key_ids"] = []string{"unknown"} }},
		{"duplicate key IDs", func(m, k map[string]any) { m["signing_key_ids"] = []string{"source-key", "source-key"} }},
		{"resource version", func(m, k map[string]any) { m["resources"].([]any)[0].(map[string]any)["version"] = "2" }},
		{"fields missing", func(m, k map[string]any) {
			m["resources"].([]any)[0].(map[string]any)["fields"] = []string{"membership_status"}
		}},
		{"duplicate resource", func(m, k map[string]any) { r := m["resources"].([]any); m["resources"] = append(r, r[0]) }},
		{"remote key URL", func(m, k map[string]any) { m["jwks_url"] = "https://attacker.example/keys" }},
		{"case alias", func(m, k map[string]any) { m["Adapter_ID"] = m["adapter_id"]; delete(m, "adapter_id") }},
		{"private key", func(m, k map[string]any) { k["keys"].([]any)[0].(map[string]any)["d"] = "private" }},
		{"wrong curve", func(m, k map[string]any) { k["keys"].([]any)[0].(map[string]any)["crv"] = "P-384" }},
		{"wrong algorithm", func(m, k map[string]any) { k["keys"].([]any)[0].(map[string]any)["alg"] = "HS256" }},
		{"wrong use", func(m, k map[string]any) { k["keys"].([]any)[0].(map[string]any)["use"] = "enc" }},
		{"wrong key ID", func(m, k map[string]any) { k["keys"].([]any)[0].(map[string]any)["kid"] = "unknown" }},
		{"wrong coordinate", func(m, k map[string]any) {
			k["keys"].([]any)[0].(map[string]any)["x"] = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
		}},
		{"missing keys", func(m, k map[string]any) { k["keys"] = []any{} }},
		{"extra key", func(m, k map[string]any) { keys := k["keys"].([]any); k["keys"] = append(keys, keys[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, keys, d := fixture(t)
			raw, _ := json.Marshal(m)
			var manifest map[string]any
			json.Unmarshal(raw, &manifest)
			raw, _ = json.Marshal(keys)
			json.Unmarshal(raw, &keys)
			test.edit(manifest, keys)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("credentials sent to public metadata")
				}
				metadataHeaders(w)
				switch r.URL.Path {
				case ManifestPath:
					json.NewEncoder(w).Encode(manifest)
				case KeysPath:
					json.NewEncoder(w).Encode(keys)
				default:
					t.Error("unapproved metadata path")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			d.Origin = server.URL
			approved, err := Approve(d, true)
			if err != nil {
				t.Fatal(err)
			}
			verifier, err := New(nil)
			if err != nil {
				t.Fatal(err)
			}
			defer verifier.Close()
			err = verifier.Check(context.Background(), approved)
			if test.name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrMetadata) {
				t.Fatal("metadata substitution accepted", err)
			}
		})
	}
}
func TestDescriptorCopiesAndDoesNotExpandAuthority(t *testing.T) {
	m, keys, d := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataHeaders(w)
		if r.URL.Path == ManifestPath {
			json.NewEncoder(w).Encode(m)
		} else {
			json.NewEncoder(w).Encode(keys)
		}
	}))
	defer server.Close()
	d.Origin = server.URL
	approved, err := Approve(d, true)
	if err != nil {
		t.Fatal(err)
	}
	d.Resources[0].Fields[0] = "unapproved"
	d.Keys["source-key"].X.SetInt64(0)
	delete(d.Keys, "source-key")
	// The served key set was materialized before mutation. Metadata resource m has its own field slice.
	verifier, _ := New(nil)
	defer verifier.Close()
	if err = verifier.Check(context.Background(), approved); err != nil {
		t.Fatal("caller changed approved trust", err)
	}
	if !approved.Allows("shg.membership", []string{"membership_status"}) || approved.Allows("shg.membership", []string{"private_record"}) || approved.Allows("other", []string{"membership_status"}) {
		t.Fatal("discovered metadata expanded approved scope")
	}
	for _, origin := range []string{"http://localhost:1234", "http://10.0.0.1", "https://example.com/metadata", "https://user:pass@example.com", "https://example.com?next=other", "https://example.com/#fragment"} {
		_, _, next := fixture(t)
		next.Origin = origin
		if _, err = Approve(next, true); !errors.Is(err, ErrDescriptor) {
			t.Fatal("unapproved origin accepted", origin)
		}
	}
	_, _, next := fixture(t)
	next.Origin = server.URL
	if _, err = Approve(next, false); !errors.Is(err, ErrDescriptor) {
		t.Fatal("production cleartext accepted")
	}
}
func TestDiscoveryRejectsRedirectsAmbiguousAndOversizedJSON(t *testing.T) {
	for _, mode := range []string{"redirect", "duplicate", "oversized", "header", "media"} {
		t.Run(mode, func(t *testing.T) {
			m, _, d := fixture(t)
			targetCalls := 0
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				metadataHeaders(w)
				switch mode {
				case "redirect":
					http.Redirect(w, r, target.URL, 307)
				case "duplicate":
					w.Write([]byte(`{"adapter_id":"one","adapter_id":"two"}`))
				case "oversized":
					w.Write(make([]byte, 65537))
				case "header":
					w.Header().Set(VersionHeader, "2.0.0")
					json.NewEncoder(w).Encode(m)
				case "media":
					w.Header().Set("Content-Type", "text/html")
					json.NewEncoder(w).Encode(m)
				}
			}))
			defer server.Close()
			d.Origin = server.URL
			a, err := Approve(d, true)
			if err != nil {
				t.Fatal(err)
			}
			v, _ := New(nil)
			defer v.Close()
			if err = v.Check(context.Background(), a); !errors.Is(err, ErrMetadata) || targetCalls != 0 {
				t.Fatal("unsafe response followed or accepted", mode, err, targetCalls)
			}
		})
	}
}
