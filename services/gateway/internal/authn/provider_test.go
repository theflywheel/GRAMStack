package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	wrongKey, redirectToken, demo := false, false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}, "gramstack_demo": demo})
		case "/jwks":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "key-one", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if redirectToken {
				w.Header().Set("Location", "https://attacker.example/token")
				w.WriteHeader(302)
				return
			}
			client, secret, ok := r.BasicAuth()
			if r.ParseForm() != nil || !ok || client != "client" || secret != "synthetic-secret" || r.PostForm.Get("code_verifier") != "expected-verifier" {
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
	p, err := newProvider(context.Background(), ProviderConfig{Issuer: issuer, ClientID: "client", ClientSecret: "synthetic-secret"}, issuer+"/callback", true)
	if err != nil {
		t.Fatal(err)
	}
	good := func() map[string]any {
		return map[string]any{"iss": issuer, "sub": "opaque-user", "aud": "client", "iat": time.Now().Add(-time.Second).Unix(), "exp": time.Now().Add(time.Minute).Unix(), "nonce": "expected-nonce"}
	}
	mu.Lock()
	claims = good()
	mu.Unlock()
	if _, err = p.complete(context.Background(), "code", "expected-nonce", "expected-verifier"); err != nil {
		t.Fatal("valid ID token rejected:", err)
	}
	for _, tc := range []struct {
		name, key string
		value     any
	}{{"issuer", "iss", "https://attacker.example"}, {"audience", "aud", "other-client"}, {"multiple audience", "aud", []string{"client", "other-client"}}, {"nonce", "nonce", "wrong-nonce"}, {"authorised party", "azp", "other-client"}, {"future validity", "nbf", time.Now().Add(time.Hour).Unix()}, {"expiry", "exp", time.Now().Add(-time.Second).Unix()}, {"future issuance", "iat", time.Now().Add(time.Hour).Unix()}, {"empty subject", "sub", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			claims = good()
			claims[tc.key] = tc.value
			mu.Unlock()
			if _, err := p.complete(context.Background(), "code", "expected-nonce", "expected-verifier"); err == nil {
				t.Fatal("invalid ID token accepted")
			}
		})
	}
	mu.Lock()
	claims = good()
	wrongKey = true
	mu.Unlock()
	if _, err = p.complete(context.Background(), "code", "expected-nonce", "expected-verifier"); err == nil {
		t.Fatal("wrong signature accepted")
	}
	mu.Lock()
	wrongKey = false
	redirectToken = true
	mu.Unlock()
	if _, err = p.complete(context.Background(), "code", "expected-nonce", "expected-verifier"); err == nil {
		t.Fatal("token endpoint redirect followed")
	}
	mu.Lock()
	redirectToken = false
	demo = true
	mu.Unlock()
	if _, err = newProvider(context.Background(), ProviderConfig{Issuer: issuer, ClientID: "client", ClientSecret: "synthetic-secret"}, issuer+"/callback", true); err == nil {
		t.Fatal("fictional IdP accepted without explicit demo permission")
	}
}
