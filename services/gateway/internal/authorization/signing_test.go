package authorization

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

func TestSignedDocumentsFailClosed(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]*ecdsa.PublicKey{"key": &key.PublicKey}
	signer, err := NewGrantSigner("key", key)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"aud":"adapter","client_id":"client","cnf":{"x5t#S256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"consent_ref":"consent","exp":102,"fields":["field"],"iat":100,"iss":"https://issuer.example","jti":"grant","policy_version":"v1","purpose":"purpose","request_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","request_id":"request","resource":"resource","sub":"principal","subject_context_ref":"context"}`)
	compact, err := signer.Sign(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var value Grant
	if _, err = verify(compact, GrantType, keys, &value); err != nil || value.validate() != nil {
		t.Fatal("valid grant rejected", err)
	}
	if _, err = verify(compact, PolicyType, keys, &Policy{}); err == nil {
		t.Fatal("cross-protocol signature accepted")
	}
	parts := strings.Split(compact, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(raw), `"field"`, `"secret"`, 1)))
	if _, err = verify(strings.Join(parts, "."), GrantType, keys, &Grant{}); err == nil {
		t.Fatal("payload substitution accepted")
	}
	for name, payload := range map[string]string{
		"duplicate top-level": strings.Replace(string(raw), `"jti":"grant"`, `"jti":"other","jti":"grant"`, 1),
		"duplicate nested":    strings.Replace(string(raw), `"cnf":{`, `"cnf":{"x5t#S256":"other",`, 1),
		"unknown field":       strings.Replace(string(raw), `"aud":`, `"override":true,"aud":`, 1),
		"case alias":          strings.Replace(string(raw), `"aud":`, `"Aud":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			signed, err := signer.Sign(context.Background(), []byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = verify(signed, GrantType, keys, &Grant{}); err == nil {
				t.Fatal("ambiguous signed claims accepted")
			}
		})
	}
	for name, options := range map[string]*jose.SignerOptions{
		"wrong type":  (&jose.SignerOptions{}).WithType("untrusted").WithHeader("kid", "key"),
		"unknown key": (&jose.SignerOptions{}).WithType(GrantType).WithHeader("kid", "other"),
		"key URL":     (&jose.SignerOptions{}).WithType(GrantType).WithHeader("kid", "key").WithHeader("jku", "https://attacker.invalid/keys"),
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, options)
			signed, _ := s.Sign(raw)
			token, _ := signed.CompactSerialize()
			if _, err := verify(token, GrantType, keys, &Grant{}); err == nil {
				t.Fatal("untrusted signing metadata accepted")
			}
		})
	}
	copied, err := copyKeys(keys)
	if err != nil {
		t.Fatal(err)
	}
	key.PublicKey.X.SetInt64(1)
	if _, err = verify(compact, GrantType, copied, &Grant{}); err != nil {
		t.Fatal("caller key mutation changed pinned verifier", err)
	}
	if _, err = NewGrantSigner("bad", &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}}); err == nil {
		t.Fatal("incomplete signing key accepted")
	}
	overlong := value
	overlong.ExpiresAt = overlong.IssuedAt + 301
	if overlong.validate() == nil {
		t.Fatal("unbounded grant lifetime accepted")
	}
	if _, err = SignPolicy(context.Background(), "key", key, Policy{}); err == nil {
		t.Fatal("empty policy signed")
	}
}
