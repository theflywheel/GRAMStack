package oidcadapter

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestOIDCAssertionsFailClosed(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	var mu sync.Mutex
	claims := map[string]any{}
	wrongKey, redirectToken, demo, invalidCert := false, false, false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}, "gramstack_demo": demo})
		case "/jwks":
			jwk := jose.JSONWebKey{Key: &key.PublicKey, KeyID: "key-one", Algorithm: "RS256", Use: "sig"}
			raw, _ := json.Marshal(jwk)
			var fields map[string]any
			json.Unmarshal(raw, &fields)
			if invalidCert {
				fields["x5c"] = []string{"invalid-certificate"}
			}
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{fields}})
		case "/token":
			if redirectToken {
				w.Header().Set("Location", "https://attacker.example/token")
				w.WriteHeader(302)
				return
			}
			client, secret, ok := r.BasicAuth()
			if r.ParseForm() != nil || !ok || client != "client" || secret != "synthetic-secret" || r.PostForm.Get("code_verifier") != strings.Repeat("v", 43) {
				t.Error("token exchange lost client or PKCE binding")
			}
			signing := key
			if wrongKey {
				signing = other
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signing}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key-one"))
			if err != nil {
				t.Error(err)
				return
			}
			raw, _ := json.Marshal(claims)
			signed, err := signer.Sign(raw)
			if err != nil {
				t.Error(err)
				return
			}
			compact, err := signed.CompactSerialize()
			if err != nil {
				t.Error(err)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-access-token", "token_type": "Bearer", "id_token": compact})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	p, err := New(context.Background(), Profile{SchemaVersion: "gramstack.auth.oidc/v1", Issuer: issuer, ClientID: "client", TokenAuthMethod: "client_secret_basic", IDTokenAlgorithms: []string{"RS256"}}, Credentials{ClientSecret: "synthetic-secret"}, issuer+"/callback", Options{AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	good := func() map[string]any {
		return map[string]any{"iss": issuer, "sub": "opaque-user", "aud": "client", "iat": time.Now().Add(-time.Second).Unix(), "exp": time.Now().Add(time.Minute).Unix(), "auth_time": time.Now().Add(-time.Second).Unix(), "amr": []string{"otp"}, "nonce": strings.Repeat("n", 32)}
	}
	mu.Lock()
	claims = good()
	mu.Unlock()
	if _, err = p.Complete(context.Background(), "code", testTransaction()); err != nil {
		t.Fatal("valid ID token rejected:", err)
	}
	for _, tc := range []struct {
		name, key string
		value     any
	}{{"issuer", "iss", "https://attacker.example"}, {"audience", "aud", "other-client"}, {"multiple audience", "aud", []string{"client", "other-client"}}, {"nonce", "nonce", "wrong-nonce"}, {"authorised party", "azp", "other-client"}, {"future validity", "nbf", time.Now().Add(time.Hour).Unix()}, {"expiry", "exp", time.Now().Add(-time.Second).Unix()}, {"future issuance", "iat", time.Now().Add(time.Hour).Unix()}, {"missing issuance", "iat", 0}, {"empty subject", "sub", ""}, {"missing auth time", "auth_time", 0}, {"stale auth time", "auth_time", time.Now().Add(-10 * time.Minute).Unix()}, {"future auth time", "auth_time", time.Now().Add(time.Minute).Unix()}} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			claims = good()
			claims[tc.key] = tc.value
			mu.Unlock()
			if _, err := p.Complete(context.Background(), "code", testTransaction()); err == nil {
				t.Fatal("invalid ID token accepted")
			}
		})
	}
	mu.Lock()
	claims = good()
	wrongKey = true
	mu.Unlock()
	if _, err = p.Complete(context.Background(), "code", testTransaction()); err == nil {
		t.Fatal("wrong signature accepted")
	}
	mu.Lock()
	wrongKey = false
	redirectToken = true
	mu.Unlock()
	if _, err = p.Complete(context.Background(), "code", testTransaction()); err == nil {
		t.Fatal("token endpoint redirect followed")
	}
	mu.Lock()
	redirectToken = false
	demo = true
	mu.Unlock()
	if _, err = New(context.Background(), Profile{SchemaVersion: "gramstack.auth.oidc/v1", Issuer: issuer, ClientID: "client", TokenAuthMethod: "client_secret_basic", IDTokenAlgorithms: []string{"RS256"}}, Credentials{ClientSecret: "synthetic-secret"}, issuer+"/callback", Options{AllowLoopback: true}); err == nil {
		t.Fatal("fictional IdP accepted without explicit demo permission")
	}
	profile := Profile{SchemaVersion: "gramstack.auth.oidc/v1", Issuer: issuer, ClientID: "client", TokenAuthMethod: "client_secret_basic", IDTokenAlgorithms: []string{"RS256"}, JWKSCertificateMetadata: "ignore"}
	if _, err = New(context.Background(), profile, Credentials{ClientSecret: "synthetic-secret"}, issuer+"/callback", Options{AllowLoopback: true}); err == nil {
		t.Fatal("demo compatibility enabled without explicit permission")
	}
	mu.Lock()
	invalidCert = true
	claims = good()
	mu.Unlock()
	compatible, err := New(context.Background(), profile, Credentials{ClientSecret: "synthetic-secret"}, issuer+"/callback", Options{AllowLoopback: true, AllowDemo: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = compatible.Complete(context.Background(), "code", testTransaction()); err != nil {
		t.Fatal("public JWK signature rejected:", err)
	}
	mu.Lock()
	wrongKey = true
	mu.Unlock()
	if _, err = compatible.Complete(context.Background(), "code", testTransaction()); err == nil {
		t.Fatal("metadata compatibility bypassed signature validation")
	}
}

func testTransaction() Transaction {
	return Transaction{ID: "transaction-123456", State: strings.Repeat("s", 32), Nonce: strings.Repeat("n", 32), Verifier: strings.Repeat("v", 43)}
}

func TestPrivateKeyJWTAndPS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	credentials := Credentials{PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})}
	var issuer string
	var mu sync.Mutex
	seen := map[string]bool{}
	var useWrongAlg bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks", "id_token_signing_alg_values_supported": []string{"PS256", "RS256"}})
		case "/jwks":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &idKey.PublicKey, KeyID: "id-key", Use: "sig"}}})
		case "/token":
			if r.ParseForm() != nil {
				t.Error("form parse")
				w.WriteHeader(400)
				return
			}
			if r.Header.Get("Authorization") != "" || r.PostForm.Get("client_secret") != "" || r.PostForm.Get("client_id") != "client" || r.PostForm.Get("code_verifier") != testTransaction().Verifier || r.PostForm.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
				t.Error("incorrect assertion or PKCE transport")
				w.WriteHeader(400)
				return
			}
			signed, err := jose.ParseSigned(r.PostForm.Get("client_assertion"), []jose.SignatureAlgorithm{jose.PS256})
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if len(signed.Signatures) != 1 || signed.Signatures[0].Header.KeyID != "client-key" {
				t.Error("missing signing key binding")
			}
			raw, err := signed.Verify(&key.PublicKey)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			var c struct {
				Issuer   string `json:"iss"`
				Subject  string `json:"sub"`
				Audience string `json:"aud"`
				ID       string `json:"jti"`
				Issued   int64  `json:"iat"`
				Expires  int64  `json:"exp"`
			}
			if json.Unmarshal(raw, &c) != nil || c.Issuer != "client" || c.Subject != "client" || c.Audience != issuer+"/token" || len(c.ID) < 26 || seen[c.ID] || c.Issued > time.Now().Unix() || c.Expires-c.Issued != 60 || c.Expires <= time.Now().Unix() {
				t.Error("invalid/reused client assertion claims")
				w.WriteHeader(400)
				return
			}
			seen[c.ID] = true
			alg := jose.PS256
			if useWrongAlg {
				alg = jose.RS256
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: idKey}, (&jose.SignerOptions{}).WithHeader("kid", "id-key"))
			if err != nil {
				t.Error(err)
				return
			}
			raw, _ = json.Marshal(map[string]any{"iss": issuer, "sub": "pairwise-person", "aud": "client", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "nonce": testTransaction().Nonce, "auth_time": time.Now().Unix(), "amr": []string{"otp"}, "acr": "upstream-acr"})
			sig, err := signer.Sign(raw)
			if err != nil {
				t.Error(err)
				return
			}
			compact, err := sig.CompactSerialize()
			if err != nil {
				t.Error(err)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-token", "token_type": "Bearer", "id_token": compact})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	profile := Profile{SchemaVersion: "gramstack.auth.oidc/v1", Issuer: issuer, ClientID: "client", TokenAuthMethod: "private_key_jwt", AssertionAudience: "token_endpoint", IDTokenAlgorithms: []string{"PS256"}, AssertionKeyID: "client-key"}
	p, err := New(context.Background(), profile, credentials, issuer+"/callback", Options{AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := p.Begin(testTransaction())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if q.Get("state") != testTransaction().State || q.Get("nonce") != testTransaction().Nonce || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("prompt") != "login" || q.Get("max_age") != "0" {
		t.Fatal("missing transaction or freshness binding")
	}
	for i := 0; i < 2; i++ {
		principal, err := p.Complete(context.Background(), "code", testTransaction())
		if err != nil {
			t.Fatal(err)
		}
		if principal.Issuer != issuer || principal.Subject != "pairwise-person" || principal.TransactionID != testTransaction().ID || principal.Assurance != "upstream-acr" || len(principal.Methods) != 1 || principal.Methods[0] != "otp" {
			t.Fatal("normalization lost verified provenance")
		}
	}
	mu.Lock()
	useWrongAlg = true
	mu.Unlock()
	if _, err = p.Complete(context.Background(), "code", testTransaction()); err == nil {
		t.Fatal("unpinned ID token algorithm accepted")
	}
	for _, tc := range []struct {
		name string
		edit func(*Profile, *Credentials)
	}{
		{"version", func(p *Profile, c *Credentials) { p.SchemaVersion = "v2" }},
		{"unsigned", func(p *Profile, c *Credentials) { p.IDTokenAlgorithms = []string{"none"} }},
		{"no algorithms", func(p *Profile, c *Credentials) { p.IDTokenAlgorithms = nil }},
		{"mixed credentials", func(p *Profile, c *Credentials) { c.ClientSecret = "secret" }},
		{"missing key", func(p *Profile, c *Credentials) { c.PrivateKeyPEM = nil }},
		{"missing key id", func(p *Profile, c *Credentials) { p.AssertionKeyID = "" }},
		{"unknown method", func(p *Profile, c *Credentials) { p.TokenAuthMethod = "client_secret_post" }},
		{"missing assertion audience", func(p *Profile, c *Credentials) { p.AssertionAudience = "" }},
		{"untrusted assertion audience", func(p *Profile, c *Credentials) { p.AssertionAudience = "https://other.example" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr, cr := profile, credentials
			tc.edit(&pr, &cr)
			if _, err := New(context.Background(), pr, cr, issuer+"/callback", Options{AllowLoopback: true}); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
	invalid := testTransaction()
	invalid.Nonce = ""
	if _, err = p.Begin(invalid); err == nil {
		t.Fatal("unbound challenge accepted")
	}
	if _, err = p.Complete(context.Background(), "code", invalid); err == nil {
		t.Fatal("unbound completion accepted")
	}
}
