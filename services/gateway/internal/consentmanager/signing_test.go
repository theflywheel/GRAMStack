package consentmanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	jose "github.com/go-jose/go-jose/v4"
	"strings"
	"testing"
	"time"
)

func testAuthority(t *testing.T) (Signer, *Verifier, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewES256Signer("consent-key-1", key)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier("https://consent.example", "https://consent.example", map[string]*ecdsa.PublicKey{"consent-key-1": &key.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	return signer, verifier, key
}
func testArtefact(now time.Time) Artefact {
	digest := "sha256:" + strings.Repeat("a", 64)
	return Artefact{ConsentID: "consent-1", SchemaVersion: "gramstack.consent.v1", Issuer: "https://consent.example", RequestDigest: digest, SubjectContextDigest: digest, Consumer: "sakhicredit", Provider: "lokos", Resource: "shg.membership", Fields: []string{"membership_status"}, Purpose: "credit_eligibility", Frequency: "ONCE", RecipientTermsVersion: "terms-v1", RecipientTermsDigest: digest, NoticeDigest: digest, ApprovedAt: now.Add(-time.Second), ValidUntil: now.Add(time.Minute), StatusEndpoint: "https://consent.example/internal/consents/consent-1/status"}
}
func TestArtefactSignaturesAndTrust(t *testing.T) {
	signer, verifier, key := testAuthority(t)
	now := time.Now().UTC()
	artefact := testArtefact(now)
	payload, _ := Canonical(artefact)
	compact, err := signer.Sign(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	got, verified, err := verifier.Verify(compact, now)
	if err != nil || got.ConsentID != artefact.ConsentID || string(verified) != string(payload) {
		t.Fatalf("%+v %v", got, err)
	}
	parts := strings.Split(compact, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"consumer":"attacker"}`))
	if _, _, err := verifier.Verify(strings.Join(parts, "."), now); err == nil {
		t.Fatal("tampered payload accepted")
	}
	for _, change := range []func(*Artefact){
		func(a *Artefact) { a.Issuer = "https://attacker.example" },
		func(a *Artefact) { a.StatusEndpoint = "https://attacker.example/status" },
		func(a *Artefact) { a.ValidUntil = now.Add(-time.Millisecond) },
		func(a *Artefact) { a.ApprovedAt = now.Add(time.Second) },
		func(a *Artefact) { a.Fields = []string{"membership_status", "membership_status"} },
		func(a *Artefact) { a.RequestDigest = "sha256:fake" },
	} {
		changed := artefact
		change(&changed)
		raw, _ := Canonical(changed)
		token, err := signer.Sign(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := verifier.Verify(token, now); err == nil {
			t.Fatalf("invalid signed artefact accepted: %+v", changed)
		}
	}
	for _, header := range []struct{ typ, kid string }{{"gramstack-evidence+jwt", "consent-key-1"}, {ConsentType, "untrusted-key"}} {
		other, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType(jose.ContentType(header.typ)).WithHeader("kid", header.kid))
		if err != nil {
			t.Fatal(err)
		}
		signed, _ := other.Sign(payload)
		token, _ := signed.CompactSerialize()
		if _, _, err := verifier.Verify(token, now); err == nil {
			t.Fatal("wrong key ID or protocol accepted")
		}
	}
	hs, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte(strings.Repeat("a", 32))}, (&jose.SignerOptions{}).WithType(ConsentType).WithHeader("kid", "consent-key-1"))
	signed, _ := hs.Sign(payload)
	token, _ := signed.CompactSerialize()
	if _, _, err := verifier.Verify(token, now); err == nil {
		t.Fatal("algorithm substitution accepted")
	}
	_, wrongVerifier, _ := testAuthority(t)
	if _, _, err := wrongVerifier.Verify(compact, now); err == nil {
		t.Fatal("wrong public key accepted")
	}

	duplicate := append(append([]byte{}, payload[:len(payload)-1]...), []byte(`,"consumer":"attacker"}`)...)
	duplicateToken, err := signer.Sign(context.Background(), duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifier.Verify(duplicateToken, now); err == nil {
		t.Fatal("duplicate claims accepted")
	}
	remote, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType(ConsentType).WithHeader("kid", "consent-key-1").WithHeader("jku", "https://attacker.example/keys"))
	if err != nil {
		t.Fatal(err)
	}
	remoteObject, _ := remote.Sign(payload)
	remoteToken, _ := remoteObject.CompactSerialize()
	if _, _, err := verifier.Verify(remoteToken, now); err == nil {
		t.Fatal("request-controlled key URL accepted")
	}

	raw, err := json.Marshal(verifier.JWKS())
	if err != nil {
		t.Fatal(err)
	}
	var jwks struct{ Keys []map[string]any }
	if err = json.Unmarshal(raw, &jwks); err != nil {
		t.Fatal(err)
	}
	if len(jwks.Keys) != 1 || jwks.Keys[0]["d"] != nil || jwks.Keys[0]["use"] != "sig" || jwks.Keys[0]["alg"] != "ES256" {
		t.Fatalf("unexpected public key set: %s", raw)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := signer.Sign(ctx, payload); err == nil {
		t.Fatal("canceled signing accepted")
	}
}
func TestCanonicalSnapshotDigest(t *testing.T) {
	raw, err := Canonical(map[string]any{"z": 1, "a": []string{"b", "a"}})
	if err != nil || string(raw) != `{"a":["b","a"],"z":1}` {
		t.Fatalf("%s %v", raw, err)
	}
}

func TestSigningConfigurationFailsClosed(t *testing.T) {
	malformed := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}}
	if _, err := NewES256Signer("key", malformed); err == nil {
		t.Fatal("malformed private key accepted")
	}
	if _, err := NewVerifier("https://consent.example", "https://consent.example", map[string]*ecdsa.PublicKey{"key": {Curve: elliptic.P256()}}); err == nil {
		t.Fatal("malformed verification key accepted")
	}
}
