package consentmanager

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type Signer interface {
	Sign(context.Context, []byte) (string, error)
}
type localSigner struct{ signer jose.Signer }

func NewES256Signer(keyID string, key *ecdsa.PrivateKey) (Signer, error) {
	if !identifier.MatchString(keyID) || key == nil || key.Curve != elliptic.P256() || key.D == nil || key.X == nil || key.Y == nil || key.D.Sign() <= 0 || key.D.Cmp(elliptic.P256().Params().N) >= 0 {
		return nil, errors.New("configured P-256 signing key and key ID required")
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	copy, err := x509.ParseECPrivateKey(raw)
	if err != nil {
		return nil, err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: copy}, (&jose.SignerOptions{}).WithType(ConsentType).WithHeader("kid", keyID))
	if err != nil {
		return nil, err
	}
	return &localSigner{signer}, nil
}
func (s *localSigner) Sign(ctx context.Context, payload []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	object, err := s.signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return object.CompactSerialize()
}

type Verifier struct {
	issuer, statusBase string
	keys               map[string]*ecdsa.PublicKey
}

func trustedOrigin(value string) bool { return trustedConsentOrigin(value, false) }
func trustedConsentOrigin(value string, allowLoopback bool) bool {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return allowLoopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()
}

func clonePublic(key *ecdsa.PublicKey) (*ecdsa.PublicKey, error) {
	if key == nil || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil {
		return nil, errors.New("P-256 public key required")
	}
	raw, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, err
	}
	return parsed.(*ecdsa.PublicKey), nil
}

type VerifierOptions struct{ AllowLoopback bool }

func NewVerifier(issuer, statusBase string, keys map[string]*ecdsa.PublicKey) (*Verifier, error) {
	return NewVerifierWithOptions(issuer, statusBase, keys, VerifierOptions{})
}
func NewVerifierWithOptions(issuer, statusBase string, keys map[string]*ecdsa.PublicKey, options VerifierOptions) (*Verifier, error) {
	if !trustedConsentOrigin(issuer, options.AllowLoopback) || !trustedConsentOrigin(statusBase, options.AllowLoopback) || len(keys) == 0 {
		return nil, errors.New("trusted HTTPS consent issuer, status origin and verification keys required")
	}
	v := &Verifier{issuer: strings.TrimSuffix(issuer, "/"), statusBase: strings.TrimSuffix(statusBase, "/"), keys: map[string]*ecdsa.PublicKey{}}
	for id, key := range keys {
		if !identifier.MatchString(id) {
			return nil, ErrBinding
		}
		copy, err := clonePublic(key)
		if err != nil {
			return nil, err
		}
		v.keys[id] = copy
	}
	return v, nil
}
func (v *Verifier) statusEndpoint(id string) string {
	return v.statusBase + "/internal/consents/" + url.PathEscape(id) + "/status"
}
func (v *Verifier) JWKS() jose.JSONWebKeySet {
	ids := make([]string, 0, len(v.keys))
	for id := range v.keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	set := jose.JSONWebKeySet{}
	for _, id := range ids {
		key, _ := clonePublic(v.keys[id])
		set.Keys = append(set.Keys, jose.JSONWebKey{Key: key, KeyID: id, Algorithm: string(jose.ES256), Use: "sig"})
	}
	return set
}
func strictDecode(raw []byte, target any) error {
	// Our header and artefact profiles are objects of scalar fields/field arrays.
	// Reject duplicate members so different verifiers cannot interpret them differently.
	members := json.NewDecoder(bytes.NewReader(raw))
	start, err := members.Token()
	if err != nil || start != json.Delim('{') {
		return ErrBinding
	}
	seen := map[string]bool{}
	for members.More() {
		name, err := members.Token()
		if err != nil {
			return ErrBinding
		}
		key, ok := name.(string)
		if !ok || seen[key] {
			return ErrBinding
		}
		seen[key] = true
		var value json.RawMessage
		if err := members.Decode(&value); err != nil {
			return ErrBinding
		}
	}
	if _, err := members.Token(); err != nil {
		return ErrBinding
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrBinding
	}
	return nil
}

// Verify checks signature and immutable artefact validity. Live consent/use status
// must still be checked by the shared enforcement authority before retrieval.
func (v *Verifier) Verify(compact string, now time.Time) (Artefact, []byte, error) {
	var artefact Artefact
	if len(compact) > 128*1024 {
		return artefact, nil, ErrBinding
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return artefact, nil, ErrBinding
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return artefact, nil, ErrBinding
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if strictDecode(raw, &header) != nil || header.Algorithm != "ES256" || header.Type != ConsentType || v.keys[header.KeyID] == nil {
		return artefact, nil, ErrBinding
	}
	object, err := jose.ParseSignedCompact(compact, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return artefact, nil, ErrBinding
	}
	payload, err := object.Verify(v.keys[header.KeyID])
	if err != nil {
		return artefact, nil, ErrBinding
	}
	if strictDecode(payload, &artefact) != nil {
		return Artefact{}, nil, ErrBinding
	}
	for _, id := range []string{artefact.ConsentID, artefact.Consumer, artefact.Provider, artefact.Resource, artefact.Purpose, artefact.RecipientTermsVersion} {
		if !identifier.MatchString(id) {
			return Artefact{}, nil, ErrBinding
		}
	}
	for _, digest := range []string{artefact.RequestDigest, artefact.SubjectContextDigest, artefact.RecipientTermsDigest, artefact.NoticeDigest} {
		if !digestPattern.MatchString(digest) {
			return Artefact{}, nil, ErrBinding
		}
	}
	if artefact.SchemaVersion != "gramstack.consent.v1" || artefact.Issuer != v.issuer || artefact.Frequency != "ONCE" || !validFields(artefact.Fields) || artefact.ApprovedAt.IsZero() || artefact.ApprovedAt.After(now) || !artefact.ValidUntil.After(artefact.ApprovedAt) || !now.Before(artefact.ValidUntil) || artefact.ValidUntil.Sub(artefact.ApprovedAt) > 300*time.Second || artefact.StatusEndpoint != v.statusEndpoint(artefact.ConsentID) {
		return Artefact{}, nil, ErrBinding
	}
	return artefact, payload, nil
}
