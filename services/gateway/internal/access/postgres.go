package access

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

var ErrVersion = errors.New("request version conflict")

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
		if !now.Before(result.ExpiresAt) && beforeAdmission && event != "expired" && event != "failed" {
			return errors.New("request deadline exceeded")
		}
		result.Status = next
		result.Version++
		if _, err = tx.Exec(ctx, `UPDATE access_requests SET status=$2,version=$3 WHERE request_id=$1`, id, result.Status, result.Version); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, "request", id, result.Version, "request.transitioned", map[string]any{"request_id": id, "request_digest": result.Digest, "status": result.Status})
	})
	if err != nil {
		return Request{}, err
	}
	return result, nil
}
