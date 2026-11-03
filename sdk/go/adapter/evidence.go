package adapter

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type EvidenceEvent struct {
	EventID            string    `json:"event_id"`
	RequestID          string    `json:"request_id"`
	Digest             string    `json:"request_digest"`
	ConsentID          string    `json:"consent_id"`
	GrantJTI           string    `json:"grant_jti"`
	AdapterID          string    `json:"adapter_id"`
	AttemptID          string    `json:"attempt_id"`
	RedemptionID       string    `json:"redemption_id"`
	Fields             []string  `json:"fields"`
	Outcome            string    `json:"outcome"`
	ResponseCommitment string    `json:"response_commitment,omitempty"`
	OccurredAt         time.Time `json:"occurred_at"`
}
type SignedEnvelope struct {
	JWS string `json:"jws"`
}
type EvidenceAcknowledgement struct {
	ReceiptID    string    `json:"receipt_id"`
	Digest       string    `json:"evidence_digest"`
	AcceptedAt   time.Time `json:"accepted_at"`
	AnchorStatus string    `json:"anchor_status"`
}

// ProtectedEvidence contains no business response. Salt custody stays private.
type ProtectedEvidence struct {
	JWS  string `json:"jws"`
	Salt []byte `json:"salt,omitempty"`
}
type EvidenceSigner struct{ signer jose.Signer }

func NewEvidenceSigner(kid string, key *ecdsa.PrivateKey) (*EvidenceSigner, error) {
	if !identifier.MatchString(kid) || key == nil || key.Curve != elliptic.P256() || key.D == nil || key.X == nil || key.Y == nil || key.D.Sign() <= 0 || key.D.Cmp(elliptic.P256().Params().N) >= 0 {
		return nil, ErrGrant
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, ErrGrant
	}
	copied, err := x509.ParseECPrivateKey(raw)
	if err != nil {
		return nil, ErrGrant
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: copied}, (&jose.SignerOptions{}).WithType(jose.ContentType(EvidenceType)).WithHeader("kid", kid))
	if err != nil {
		return nil, err
	}
	return &EvidenceSigner{signer}, nil
}
func validEvidence(e EvidenceEvent) bool {
	for _, id := range []string{e.EventID, e.RequestID, e.ConsentID, e.GrantJTI, e.AdapterID, e.AttemptID, e.RedemptionID} {
		if !identifier.MatchString(id) {
			return false
		}
	}
	if !digestPattern.MatchString(e.Digest) || !validFields(e.Fields) || e.OccurredAt.IsZero() {
		return false
	}
	switch e.Outcome {
	case "release_ready":
		return digestPattern.MatchString(e.ResponseCommitment)
	case "failed", "delivery_unknown":
		return e.ResponseCommitment == "" || digestPattern.MatchString(e.ResponseCommitment)
	default:
		return false
	}
}
func (s *EvidenceSigner) Sign(ctx context.Context, event EvidenceEvent) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validEvidence(event) {
		return "", errors.New("invalid evidence event")
	}
	raw, err := Canonical(event)
	if err != nil {
		return "", err
	}
	object, err := s.signer.Sign(raw)
	if err != nil {
		return "", err
	}
	return object.CompactSerialize()
}
func VerifyEvidence(compact string, keys map[string]*ecdsa.PublicKey) (EvidenceEvent, error) {
	var event EvidenceEvent
	if _, err := verifyDocument(compact, EvidenceType, keys, &event); err != nil {
		return event, err
	}
	if !validEvidence(event) {
		return EvidenceEvent{}, ErrGrant
	}
	return event, nil
}
func responseCommitment(data Data) (string, []byte, error) {
	raw, err := Canonical(data)
	if err != nil {
		return "", nil, err
	}
	salt := make([]byte, 32)
	if _, err = rand.Read(salt); err != nil {
		return "", nil, err
	}
	return Digest(append(salt[:len(salt):len(salt)], raw...)), salt, nil
}
