package custody

import (
	"bytes"
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

func decode(raw []byte, target any) error {
	if len(raw) > 256*1024 {
		return ErrDenied
	}
	if _, err := jcs.Transform(raw); err != nil {
		return ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return ErrDenied
	}
	return nil
}
func copyKeys(keys map[string]*ecdsa.PublicKey) (map[string]*ecdsa.PublicKey, error) {
	if len(keys) == 0 {
		return nil, ErrDenied
	}
	result := map[string]*ecdsa.PublicKey{}
	for kid, key := range keys {
		if !identifier.MatchString(kid) || key == nil || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil {
			return nil, ErrDenied
		}
		raw, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			return nil, ErrDenied
		}
		parsed, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			return nil, ErrDenied
		}
		result[kid] = parsed.(*ecdsa.PublicKey)
	}
	return result, nil
}
func verify(compact string, keys map[string]*ecdsa.PublicKey) (Event, error) {
	var event Event
	if len(compact) > 128*1024 {
		return event, ErrDenied
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return event, ErrDenied
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return event, ErrDenied
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if decode(raw, &header) != nil || header.Algorithm != "ES256" || header.Type != EvidenceType || keys[header.KeyID] == nil {
		return event, ErrDenied
	}
	object, err := jose.ParseSignedCompact(compact, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return event, ErrDenied
	}
	payload, err := object.Verify(keys[header.KeyID])
	if err != nil || decode(payload, &event) != nil {
		return event, ErrDenied
	}
	canonical, err := cm.Canonical(event)
	if err != nil || !bytes.Equal(canonical, payload) {
		return Event{}, ErrDenied
	}
	for _, id := range []string{event.EventID, event.RequestID, event.ConsentID, event.GrantJTI, event.AdapterID, event.AttemptID, event.RedemptionID} {
		if !identifier.MatchString(id) {
			return Event{}, ErrDenied
		}
	}
	if !digestPattern.MatchString(event.Digest) || event.OccurredAt.IsZero() || len(event.Fields) == 0 || len(event.Fields) > 128 {
		return Event{}, ErrDenied
	}
	seen := map[string]bool{}
	for _, field := range event.Fields {
		if !identifier.MatchString(field) || seen[field] {
			return Event{}, ErrDenied
		}
		seen[field] = true
	}
	switch event.Outcome {
	case "release_ready":
		if !digestPattern.MatchString(event.Commitment) {
			return Event{}, ErrDenied
		}
	case "failed", "delivery_unknown":
		if event.Commitment != "" && !digestPattern.MatchString(event.Commitment) {
			return Event{}, ErrDenied
		}
	default:
		return Event{}, ErrDenied
	}
	return event, nil
}
