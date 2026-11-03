package authorization

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/gowebpki/jcs"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type Signer interface {
	Sign(context.Context, []byte) (string, error)
}
type signer struct{ value jose.Signer }

func newSigner(kind, kid string, key *ecdsa.PrivateKey) (Signer, error) {
	if !idPattern.MatchString(kid) || key == nil || key.Curve != elliptic.P256() || key.D == nil || key.X == nil || key.Y == nil || key.D.Sign() <= 0 || key.D.Cmp(elliptic.P256().Params().N) >= 0 {
		return nil, ErrBinding
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, ErrBinding
	}
	copied, err := x509.ParseECPrivateKey(raw)
	if err != nil {
		return nil, ErrBinding
	}
	value, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: copied}, (&jose.SignerOptions{}).WithType(jose.ContentType(kind)).WithHeader("kid", kid))
	if err != nil {
		return nil, ErrBinding
	}
	return signer{value}, nil
}
func NewGrantSigner(kid string, key *ecdsa.PrivateKey) (Signer, error) {
	return newSigner(GrantType, kid, key)
}
func (s signer) Sign(ctx context.Context, raw []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	object, err := s.value.Sign(raw)
	if err != nil {
		return "", err
	}
	return object.CompactSerialize()
}
func SignPolicy(ctx context.Context, kid string, key *ecdsa.PrivateKey, p Policy) (string, error) {
	if p.validate() != nil {
		return "", ErrBinding
	}
	s, err := newSigner(PolicyType, kid, key)
	if err != nil {
		return "", err
	}
	raw, err := cm.Canonical(p)
	if err != nil {
		return "", err
	}
	return s.Sign(ctx, raw)
}
func decode(raw []byte, dst any) error {
	if len(raw) > 128*1024 {
		return ErrBinding
	}
	if _, err := jcs.Transform(raw); err != nil {
		return ErrBinding
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrBinding
	}
	return nil
}
func copyKeys(keys map[string]*ecdsa.PublicKey) (map[string]*ecdsa.PublicKey, error) {
	if len(keys) == 0 {
		return nil, ErrBinding
	}
	result := map[string]*ecdsa.PublicKey{}
	for kid, key := range keys {
		if !idPattern.MatchString(kid) || key == nil || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil {
			return nil, ErrBinding
		}
		raw, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			return nil, ErrBinding
		}
		copied, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			return nil, ErrBinding
		}
		result[kid] = copied.(*ecdsa.PublicKey)
	}
	return result, nil
}
func verify(compact, kind string, keys map[string]*ecdsa.PublicKey, dst any) ([]byte, error) {
	if len(compact) > 128*1024 {
		return nil, ErrBinding
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, ErrBinding
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return nil, ErrBinding
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if decode(raw, &header) != nil || header.Algorithm != "ES256" || header.Type != kind || keys[header.KeyID] == nil {
		return nil, ErrBinding
	}
	object, err := jose.ParseSignedCompact(compact, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, ErrBinding
	}
	payload, err := object.Verify(keys[header.KeyID])
	if err != nil || decode(payload, dst) != nil {
		return nil, ErrBinding
	}
	canonical, err := cm.Canonical(dst)
	if err != nil || !bytes.Equal(canonical, payload) {
		return nil, ErrBinding
	}
	return payload, nil
}
