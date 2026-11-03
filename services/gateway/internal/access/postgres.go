package access

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

var (
	ErrVersion        = errors.New("request version conflict")
	ErrNotFound       = errors.New("request not found")
	ErrAdmitted       = errors.New("disclosure admission cutoff has passed")
	ErrNotCancellable = errors.New("request cannot be cancelled in this state")
)

type Repository struct{ Pool *pgxpool.Pool }
type Request struct {
	ID, ConsumerID, Digest, Status string
	Version                        int64
	ExpiresAt                      time.Time
}

func (r Repository) Create(ctx context.Context, id, consumer, digest string, expires time.Time) error {
	if id == "" || consumer == "" || !expires.After(time.Now()) {
		return errors.New("request identity, consumer and future deadline required")
	}
	_, err := r.Pool.Exec(ctx, `INSERT INTO access_requests(request_id,consumer_id,request_digest,status,expires_at) VALUES($1,$2,$3,'AUTHENTICATION_PENDING',$4)`, id, consumer, digest, expires)
	return err
}
func (r Repository) Advance(ctx context.Context, id string, expected int64, event string) (Request, error) {
	var result Request
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		var err error
		result, err = AdvanceTx(ctx, tx, id, expected, event)
		return err
	})
	if err != nil {
		return Request{}, err
	}
	return result, nil
}

// AdvanceTx shares request CAS and outbox with other authoritative SQL writes.
func AdvanceTx(ctx context.Context, tx pgx.Tx, id string, expected int64, event string) (Request, error) {
	if event == "cancelled" {
		return Request{}, ErrNotCancellable
	}
	var result Request
	err := func() error {
		if err := tx.QueryRow(ctx, `SELECT request_id,consumer_id,request_digest,status,version,expires_at FROM access_requests WHERE request_id=$1 FOR UPDATE`, id).Scan(&result.ID, &result.ConsumerID, &result.Digest, &result.Status, &result.Version, &result.ExpiresAt); err != nil {
			return err
		}
		if result.Version != expected {
			return ErrVersion
		}
		next, err := Transition(ctx, result.Status, event)
		if err != nil {
			return err
		}
		// Database time decides admission deadlines. After admission, recording
		// evidence/release outcomes remains legal even when that deadline passes.
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		beforeAdmission := result.Status == "AUTHENTICATION_PENDING" || result.Status == "BINDING_PENDING" || result.Status == "CONSENT_PENDING" || result.Status == "CONSENTED" || result.Status == "AUTHORIZED"
		if !now.Before(result.ExpiresAt) && beforeAdmission && event != "expired" && event != "failed" && event != "withdrawn" && event != "rejected" {
			return errors.New("request deadline exceeded")
		}
		result.Status = next
		result.Version++
		if _, err = tx.Exec(ctx, `UPDATE access_requests SET status=$2,version=$3 WHERE request_id=$1`, id, result.Status, result.Version); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, "request", id, result.Version, "request.transitioned", map[string]any{"request_id": id, "request_digest": result.Digest, "status": result.Status})
	}()
	if err != nil {
		return Request{}, err
	}
	return result, nil
}

// Cancel accepts a consumer identity established by the transport authentication
// layer. Request then consent is the same lock order used by consent creation.
// The consent lock serializes against activation, withdrawal and redemption.
func (r Repository) Cancel(ctx context.Context, id, consumer string) (Request, error) {
	var result Request
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT request_id,consumer_id,request_digest,status,version,expires_at FROM access_requests WHERE request_id=$1 AND consumer_id=$2 FOR UPDATE`, id, consumer).Scan(&result.ID, &result.ConsumerID, &result.Digest, &result.Status, &result.Version, &result.ExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if result.Status == "CANCELLED" {
			return nil
		}
		var consentID, status, use string
		var version int64
		err = tx.QueryRow(ctx, `SELECT consent_id,status,use_state,status_version FROM consent_state WHERE request_id=$1 FOR UPDATE`, id).Scan(&consentID, &status, &use, &version)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if use == "RESERVED" || result.Status == "REDEEMING" || result.Status == "EVIDENCE_COMMITTED" || result.Status == "RELEASE_ATTEMPTED" || result.Status == "COMPLETED" || result.Status == "DELIVERY_UNKNOWN" {
			return ErrAdmitted
		}
		next, err := Transition(ctx, result.Status, "cancelled")
		if err != nil {
			return ErrNotCancellable
		}
		if consentID != "" && (status == "PENDING" || status == "APPROVAL_RECORDED" || status == "ACTIVE") {
			cancelled, err := consentmanager.Transition(ctx, status, "cancel")
			if err != nil {
				return err
			}
			version++
			if _, err = tx.Exec(ctx, `UPDATE consent_state SET status=$2,status_version=$3 WHERE consent_id=$1`, consentID, cancelled, version); err != nil {
				return err
			}
			// This is an internal cancellation event, distinct from a subject's withdrawal.
			if err = outbox.Append(ctx, tx, "consent", consentID, version, "consent.cancelled", map[string]any{"consent_id": consentID, "access_request_id": id, "request_digest": result.Digest, "type": "consent.cancelled", "actor_type": "CONSUMER"}); err != nil {
				return err
			}
		}
		result.Status = next
		result.Version++
		if _, err = tx.Exec(ctx, `UPDATE access_requests SET status=$2,version=$3 WHERE request_id=$1`, id, result.Status, result.Version); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, "request", id, result.Version, "request.cancelled", map[string]any{"request_id": id, "request_digest": result.Digest, "status": result.Status, "actor_type": "CONSUMER"})
	})
	if err != nil {
		return Request{}, err
	}
	return result, nil
}
