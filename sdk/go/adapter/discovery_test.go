package adapter

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestConfiguredDiscoveryPublishesOnlyCapabilitiesAndPublicKeys(t *testing.T) {
	f := newRetrievalFixture(t)
	service := f.service(t)
	// Discovery takes an immutable metadata snapshot even if the host later mutates its config.
	key := f.config.SigningKeys["adapter-key"]
	handler := service.DiscoveryHandler()
	f.config.SigningKeys["unapproved-key"] = &ecdsa.PublicKey{}
	for _, path := range []string{ManifestPath, AdapterKeysPath} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "https://adapter.example"+path, nil))
		if w.Code != 200 || w.Header().Get(DiscoveryVersionHeader) != DiscoveryVersion || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("public discovery unavailable", path, w.Code)
		}
		if bytes.Contains(w.Body.Bytes(), []byte(`"d"`)) || bytes.Contains(w.Body.Bytes(), []byte("native-mapping")) || bytes.Contains(w.Body.Bytes(), []byte("unapproved-key")) || bytes.Contains(w.Body.Bytes(), []byte("authority_origin")) {
			t.Fatal("discovery exposed secrets or mutable metadata")
		}
		if path == ManifestPath {
			var m Manifest
			if Decode(w.Body.Bytes(), &m) != nil || m.Registry != "lokos" || m.AdapterID != "lokos-adapter" || m.Audience != "lokos-adapter" || m.ContractVersion != CoreVersion || m.Authentication != "mtls" || !slices.Equal(m.SigningKeyIDs, []string{"adapter-key"}) || len(m.Resources) != 1 || m.Resources[0].Version != "1" || len(m.Capabilities) != 2 {
				t.Fatal("manifest disagrees with configured pipeline")
			}
		} else {
			var set struct {
				Keys []PublicJWK `json:"keys"`
			}
			if json.Unmarshal(w.Body.Bytes(), &set) != nil || len(set.Keys) != 1 {
				t.Fatal("invalid public key set")
			}
			k := set.Keys[0]
			x, ex := base64.RawURLEncoding.Strict().DecodeString(k.X)
			y, ey := base64.RawURLEncoding.Strict().DecodeString(k.Y)
			if ex != nil || ey != nil || len(x) != 32 || len(y) != 32 || new(big.Int).SetBytes(x).Cmp(key.X) != 0 || new(big.Int).SetBytes(y).Cmp(key.Y) != 0 || k.Algorithm != "ES256" || k.Use != "sig" {
				t.Fatal("public key coordinates changed")
			}
		}
	}
	if len(f.attempts.values) != 0 || f.provider.calls.Load() != 0 || f.recorded.Load() != 0 {
		t.Fatal("metadata read performed protected work")
	}
}
