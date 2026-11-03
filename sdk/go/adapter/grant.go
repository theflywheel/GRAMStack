package adapter

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const CoreVersion = "0.3.0"
const GrantType = "gramstack-grant+jwt"
const EvidenceType = "gramstack-evidence+jwt"

var ErrGrant = errors.New("invalid retrieval authority")
var ErrGrantExpired = errors.New("retrieval authority expired")
var thumbprintPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type Grant struct {
	Issuer        string   `json:"iss"`
	Audience      string   `json:"aud"`
	Subject       string   `json:"sub"`
	Consumer      string   `json:"client_id"`
	JTI           string   `json:"jti"`
	IssuedAt      int64    `json:"iat"`
	ExpiresAt     int64    `json:"exp"`
	RequestID     string   `json:"request_id"`
	Digest        string   `json:"request_digest"`
	Resource      string   `json:"resource"`
	Fields        []string `json:"fields"`
	Purpose       string   `json:"purpose"`
	ConsentID     string   `json:"consent_ref"`
	ContextID     string   `json:"subject_context_ref"`
	PolicyVersion string   `json:"policy_version"`
	Confirmation  struct {
		Thumbprint string `json:"x5t#S256"`
	} `json:"cnf"`
}
type Presenter struct {
	Thumbprint string `json:"certificate_thumbprint"`
	Consumer   string `json:"authenticated_client_id"`
}
type RetrievalRequest struct {
	AttemptID string    `json:"attempt_id"`
	Snapshot  Snapshot  `json:"snapshot"`
	Digest    string    `json:"request_digest"`
	Grant     string    `json:"grant"`
	Presenter Presenter `json:"presenter"`
}
type DataResponse struct {
	RequestID string `json:"request_id"`
	Data      Data   `json:"data"`
	ReceiptID string `json:"receipt_id"`
}
type GrantVerifier struct {
	issuer, audience string
	keys             map[string]*ecdsa.PublicKey
}

func trustedOrigin(raw string, allowLoopback bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return allowLoopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()
}
func copyPublicKeys(keys map[string]*ecdsa.PublicKey) (map[string]*ecdsa.PublicKey, error) {
	if len(keys) == 0 {
		return nil, ErrGrant
	}
	result := map[string]*ecdsa.PublicKey{}
	for kid, key := range keys {
		if !identifier.MatchString(kid) || key == nil || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil {
			return nil, ErrGrant
		}
		raw, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			return nil, ErrGrant
		}
		copied, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			return nil, ErrGrant
		}
		result[kid] = copied.(*ecdsa.PublicKey)
	}
	return result, nil
}
func NewGrantVerifier(issuer, audience string, keys map[string]*ecdsa.PublicKey, allowLoopback bool) (*GrantVerifier, error) {
	if !trustedOrigin(issuer, allowLoopback) || !identifier.MatchString(audience) {
		return nil, ErrGrant
	}
	copied, err := copyPublicKeys(keys)
	if err != nil {
		return nil, err
	}
	return &GrantVerifier{strings.TrimSuffix(issuer, "/"), audience, copied}, nil
}
func verifyDocument(compact, kind string, keys map[string]*ecdsa.PublicKey, target any) ([]byte, error) {
	if len(compact) > 128*1024 {
		return nil, ErrGrant
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, ErrGrant
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return nil, ErrGrant
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if Decode(raw, &header) != nil || header.Algorithm != "ES256" || header.Type != kind || keys[header.KeyID] == nil {
		return nil, ErrGrant
	}
	object, err := jose.ParseSignedCompact(compact, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, ErrGrant
	}
	payload, err := object.Verify(keys[header.KeyID])
	if err != nil || Decode(payload, target) != nil {
		return nil, ErrGrant
	}
	canonical, err := Canonical(target)
	if err != nil || !bytes.Equal(canonical, payload) {
		return nil, ErrGrant
	}
	return payload, nil
}
func validFields(fields []string) bool {
	if len(fields) == 0 || len(fields) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !identifier.MatchString(f) || seen[f] {
			return false
		}
		seen[f] = true
	}
	return true
}
func (v *GrantVerifier) Verify(compact string, now time.Time) (Grant, error) {
	var g Grant
	if _, err := verifyDocument(compact, GrantType, v.keys, &g); err != nil {
		return Grant{}, err
	}
	for _, id := range []string{g.Subject, g.Consumer, g.JTI, g.RequestID, g.Resource, g.Purpose, g.ConsentID, g.ContextID, g.PolicyVersion} {
		if !identifier.MatchString(id) {
			return Grant{}, ErrGrant
		}
	}
	if g.Issuer != v.issuer || g.Audience != v.audience || !digestPattern.MatchString(g.Digest) || !thumbprintPattern.MatchString(g.Confirmation.Thumbprint) || !validFields(g.Fields) || g.IssuedAt < 0 || g.ExpiresAt <= g.IssuedAt || g.ExpiresAt-g.IssuedAt > 300 {
		return Grant{}, ErrGrant
	}
	if g.IssuedAt > now.Unix() || g.ExpiresAt <= now.Unix() {
		return Grant{}, ErrGrantExpired
	}
	return g, nil
}
