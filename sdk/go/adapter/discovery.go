package adapter

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
)

const DiscoveryVersion = "0.1.0"
const DiscoveryVersionHeader = "Gramstack-Adapter-Discovery-Version"
const ManifestPath = "/.well-known/gramstack-adapter"
const AdapterKeysPath = "/.well-known/gramstack-adapter/jwks.json"

type Manifest struct {
	AdapterID           string     `json:"adapter_id"`
	Registry            string     `json:"registry"`
	ContractVersion     string     `json:"contract_version"`
	Audience            string     `json:"audience"`
	Authentication      string     `json:"authentication"`
	SigningKeyIDs       []string   `json:"signing_key_ids"`
	VerificationMethods []string   `json:"verification_methods"`
	Capabilities        []string   `json:"capabilities"`
	Resources           []Resource `json:"resources"`
}
type PublicJWK struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
	ID        string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
}

// DiscoveryHandler is public metadata for an actually configured retrieval
// service. It exposes neither private keys, source records nor authority URLs.
func (s *RetrievalService) DiscoveryHandler() http.Handler {
	ids := make([]string, 0, len(s.config.SigningKeys))
	for id := range s.config.SigningKeys {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	resource := s.resource
	resource.Fields = slices.Clone(resource.Fields)
	manifest := Manifest{AdapterID: s.config.AdapterID, Registry: s.config.Registry, ContractVersion: CoreVersion, Audience: s.config.Grants.audience, Authentication: "mtls", SigningKeyIDs: ids, VerificationMethods: []string{s.config.VerificationMethod}, Capabilities: []string{"atomic-redemption", "evidence-before-release"}, Resources: []Resource{resource}}
	keys := struct {
		Keys []PublicJWK `json:"keys"`
	}{Keys: []PublicJWK{}}
	for _, id := range ids {
		p := s.config.SigningKeys[id]
		keys.Keys = append(keys.Keys, PublicJWK{KeyType: "EC", Curve: "P-256", X: base64.RawURLEncoding.EncodeToString(p.X.FillBytes(make([]byte, 32))), Y: base64.RawURLEncoding.EncodeToString(p.Y.FillBytes(make([]byte, 32))), ID: id, Algorithm: "ES256", Use: "sig"})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+ManifestPath, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(manifest) })
	mux.HandleFunc("GET "+AdapterKeysPath, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(keys) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Gramstack-Contract-Version", CoreVersion)
		w.Header().Set(DiscoveryVersionHeader, DiscoveryVersion)
		mux.ServeHTTP(w, r)
	})
}
