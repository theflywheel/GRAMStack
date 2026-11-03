// Package custody verifies and durably accepts scoped adapter evidence. It never
// receives response plaintext or the private response-commitment salt.
package custody

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
)

const EvidenceType = "gramstack-evidence+jwt"

var ErrDenied = errors.New("evidence scope or producer not permitted")
var ErrConflict = errors.New("immutable evidence conflict")
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Event struct {
	EventID      string    `json:"event_id"`
	RequestID    string    `json:"request_id"`
	Digest       string    `json:"request_digest"`
	ConsentID    string    `json:"consent_id"`
	GrantJTI     string    `json:"grant_jti"`
	AdapterID    string    `json:"adapter_id"`
	AttemptID    string    `json:"attempt_id"`
	RedemptionID string    `json:"redemption_id"`
	Fields       []string  `json:"fields"`
	Outcome      string    `json:"outcome"`
	Commitment   string    `json:"response_commitment,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}
type Acknowledgement struct {
	ReceiptID    string    `json:"receipt_id"`
	Digest       string    `json:"evidence_digest"`
	AcceptedAt   time.Time `json:"accepted_at"`
	AnchorStatus string    `json:"anchor_status"`
}
type ScopeReader interface {
	EvidenceScope(context.Context, string, string) (az.Grant, az.Reservation, error)
}
type Adapter struct {
	ID, Audience, PeerName string
	Keys                   map[string]*ecdsa.PublicKey
}
type Config struct {
	Pool           *pgxpool.Pool
	Scopes         ScopeReader
	Adapters       []Adapter
	ReleaseProfile string
}
type Service struct {
	pool     *pgxpool.Pool
	scopes   ScopeReader
	adapters map[string]Adapter
}
