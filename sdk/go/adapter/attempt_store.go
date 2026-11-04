package adapter

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/looplab/fsm"
)

var ErrAttemptExists = errors.New("retrieval attempt already exists")
var ErrAttemptConflict = errors.New("retrieval attempt transition conflict")

type Attempt struct {
	ID             string    `json:"attempt_id"`
	RequestID      string    `json:"request_id"`
	Digest         string    `json:"request_digest"`
	GrantJTI       string    `json:"grant_jti"`
	GrantDigest    string    `json:"grant_digest"`
	Consumer       string    `json:"consumer_id"`
	Thumbprint     string    `json:"presenter_thumbprint"`
	State          string    `json:"state"`
	Version        int64     `json:"version"`
	RedemptionID   string    `json:"redemption_id,omitempty"`
	ReceiptID      string    `json:"receipt_id,omitempty"`
	EvidenceDigest string    `json:"evidence_digest,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type AttemptUpdate struct {
	RedemptionID string
	Evidence     *ProtectedEvidence
	Ack          *EvidenceAcknowledgement
}
type AttemptStore interface {
	Claim(context.Context, Attempt) (Attempt, error)
	Get(context.Context, string) (Attempt, error)
	Advance(context.Context, string, int64, string, AttemptUpdate) (Attempt, error)
}
type SQLAttemptStore struct {
	db      *sql.DB
	adapter string
	aead    cipher.AEAD
}

func NewSQLAttemptStore(db *sql.DB, adapter string, key []byte) (*SQLAttemptStore, error) {
	if !identifier.MatchString(adapter) {
		return nil, ErrAttemptConflict
	}
	context, err := NewSQLContextStore(db, key)
	if err != nil {
		return nil, err
	}
	return &SQLAttemptStore{db, adapter, context.aead}, nil
}

const attemptSchema = `CREATE TABLE adapter_attempts(adapter_id text NOT NULL,attempt_id text NOT NULL,request_id text NOT NULL,request_digest text NOT NULL,grant_jti text NOT NULL,grant_digest text NOT NULL,consumer_id text NOT NULL,presenter_thumbprint text NOT NULL,state text NOT NULL,version bigint NOT NULL,redemption_id text NOT NULL DEFAULT '',receipt_id text NOT NULL DEFAULT '',evidence_digest text NOT NULL DEFAULT '',protected_evidence bytea,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(adapter_id,attempt_id),UNIQUE(adapter_id,request_id),UNIQUE(adapter_id,grant_jti))`

// Ready checks the installed retrieval schema without applying migrations.
func (s *SQLAttemptStore) Ready(ctx context.Context) error {
	return checkSchema(ctx, s.db, 2, attemptSchema)
}
func (s *SQLAttemptStore) Migrate(ctx context.Context) error {
	return migrateSchema(ctx, s.db, 2, attemptSchema)
}

const attemptColumns = `attempt_id,request_id,request_digest,grant_jti,grant_digest,consumer_id,presenter_thumbprint,state,version,redemption_id,receipt_id,evidence_digest,updated_at`

func readAttempt(row *sql.Row) (Attempt, error) {
	var a Attempt
	err := row.Scan(&a.ID, &a.RequestID, &a.Digest, &a.GrantJTI, &a.GrantDigest, &a.Consumer, &a.Thumbprint, &a.State, &a.Version, &a.RedemptionID, &a.ReceiptID, &a.EvidenceDigest, &a.UpdatedAt)
	return a, err
}
func (s *SQLAttemptStore) Get(ctx context.Context, id string) (Attempt, error) {
	return readAttempt(s.db.QueryRowContext(ctx, `SELECT `+attemptColumns+` FROM adapter_attempts WHERE adapter_id=$1 AND attempt_id=$2`, s.adapter, id))
}
func (s *SQLAttemptStore) Claim(ctx context.Context, a Attempt) (Attempt, error) {
	for _, id := range []string{a.ID, a.RequestID, a.GrantJTI, a.Consumer} {
		if !identifier.MatchString(id) {
			return Attempt{}, ErrAttemptConflict
		}
	}
	if !digestPattern.MatchString(a.Digest) || !digestPattern.MatchString(a.GrantDigest) || !thumbprintPattern.MatchString(a.Thumbprint) {
		return Attempt{}, ErrAttemptConflict
	}
	row := s.db.QueryRowContext(ctx, `INSERT INTO adapter_attempts(adapter_id,attempt_id,request_id,request_digest,grant_jti,grant_digest,consumer_id,presenter_thumbprint,state,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'CLAIMED',1) ON CONFLICT DO NOTHING RETURNING `+attemptColumns, s.adapter, a.ID, a.RequestID, a.Digest, a.GrantJTI, a.GrantDigest, a.Consumer, a.Thumbprint)
	result, err := readAttempt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, ErrAttemptExists
	}
	return result, err
}
func attemptTransition(ctx context.Context, state, event string) (string, error) {
	machine := fsm.NewFSM(state, fsm.Events{
		{Name: "admitted", Src: []string{"CLAIMED"}, Dst: "ADMITTED"},
		{Name: "source_started", Src: []string{"ADMITTED"}, Dst: "FETCHING"},
		{Name: "evidence_prepared", Src: []string{"FETCHING"}, Dst: "EVIDENCE_PREPARED"},
		{Name: "evidence_committed", Src: []string{"EVIDENCE_PREPARED"}, Dst: "EVIDENCE_COMMITTED"},
		{Name: "release_started", Src: []string{"EVIDENCE_COMMITTED"}, Dst: "RELEASE_ATTEMPTED"},
		{Name: "response_written", Src: []string{"RELEASE_ATTEMPTED"}, Dst: "COMPLETED"},
		{Name: "failed", Src: []string{"CLAIMED", "ADMITTED", "FETCHING", "EVIDENCE_PREPARED"}, Dst: "FAILED"},
		{Name: "ambiguous", Src: []string{"FETCHING", "EVIDENCE_PREPARED", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED"}, Dst: "DELIVERY_UNKNOWN"},
	}, nil)
	if err := machine.Event(ctx, event); err != nil {
		return "", ErrAttemptConflict
	}
	return machine.Current(), nil
}
func evidenceAAD(adapter string, a Attempt) []byte {
	return []byte(adapter + "\x00" + a.ID + "\x00" + a.RequestID + "\x00" + a.Digest + "\x00" + a.GrantDigest + "\x00" + a.GrantJTI + "\x00" + a.Consumer + "\x00" + a.Thumbprint + "\x00" + a.RedemptionID)
}
func (s *SQLAttemptStore) Advance(ctx context.Context, id string, version int64, event string, u AttemptUpdate) (Attempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	a, err := readAttempt(tx.QueryRowContext(ctx, `SELECT `+attemptColumns+` FROM adapter_attempts WHERE adapter_id=$1 AND attempt_id=$2 FOR UPDATE`, s.adapter, id))
	if err != nil {
		return Attempt{}, err
	}
	if a.Version != version {
		return Attempt{}, ErrAttemptConflict
	}
	next, err := attemptTransition(ctx, a.State, event)
	if err != nil {
		return Attempt{}, err
	}
	var sealed []byte
	if event == "admitted" {
		if !identifier.MatchString(u.RedemptionID) {
			return Attempt{}, ErrAttemptConflict
		}
		a.RedemptionID = u.RedemptionID
	} else if u.RedemptionID != "" {
		return Attempt{}, ErrAttemptConflict
	}
	if event == "evidence_prepared" {
		if u.Evidence == nil || u.Evidence.JWS == "" || len(u.Evidence.Salt) != 32 {
			return Attempt{}, ErrAttemptConflict
		}
		raw, err := json.Marshal(u.Evidence)
		if err != nil {
			return Attempt{}, err
		}
		nonce := make([]byte, s.aead.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return Attempt{}, err
		}
		sealed = s.aead.Seal(nonce, nonce, raw, evidenceAAD(s.adapter, a))
		a.EvidenceDigest = Digest([]byte(u.Evidence.JWS))
	} else if u.Evidence != nil {
		return Attempt{}, ErrAttemptConflict
	}
	if event == "evidence_committed" {
		if u.Ack == nil || !identifier.MatchString(u.Ack.ReceiptID) || u.Ack.Digest != a.EvidenceDigest || u.Ack.AcceptedAt.IsZero() || (u.Ack.AnchorStatus != "pending" && u.Ack.AnchorStatus != "anchored") {
			return Attempt{}, ErrAttemptConflict
		}
		a.ReceiptID = u.Ack.ReceiptID
	} else if u.Ack != nil {
		return Attempt{}, ErrAttemptConflict
	}
	err = tx.QueryRowContext(ctx, `UPDATE adapter_attempts SET state=$3,version=version+1,redemption_id=$4,receipt_id=$5,evidence_digest=$6,protected_evidence=coalesce($7,protected_evidence),updated_at=clock_timestamp() WHERE adapter_id=$1 AND attempt_id=$2 AND version=$8 RETURNING updated_at`, s.adapter, id, next, a.RedemptionID, a.ReceiptID, a.EvidenceDigest, sealed, version).Scan(&a.UpdatedAt)
	if err != nil {
		return Attempt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Attempt{}, err
	}
	a.State = next
	a.Version++
	return a, nil
}

// Evidence returns protected recovery material, never a cached business response.
func (s *SQLAttemptStore) Evidence(ctx context.Context, id string) (ProtectedEvidence, error) {
	var payload []byte
	a, err := s.Get(ctx, id)
	if err != nil {
		return ProtectedEvidence{}, err
	}
	if err = s.db.QueryRowContext(ctx, `SELECT protected_evidence FROM adapter_attempts WHERE adapter_id=$1 AND attempt_id=$2`, s.adapter, id).Scan(&payload); err != nil {
		return ProtectedEvidence{}, err
	}
	n := s.aead.NonceSize()
	if len(payload) < n {
		return ProtectedEvidence{}, ErrAttemptConflict
	}
	raw, err := s.aead.Open(nil, payload[:n], payload[n:], evidenceAAD(s.adapter, a))
	var value ProtectedEvidence
	if err != nil || json.Unmarshal(raw, &value) != nil || len(value.Salt) != 32 || Digest([]byte(value.JWS)) != a.EvidenceDigest {
		return ProtectedEvidence{}, ErrAttemptConflict
	}
	return value, nil
}
