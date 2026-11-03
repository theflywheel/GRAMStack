package access

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reconcile re-reads authority, never trusts the status in a delayed event.
// Request then consent is the same lock order as cancellation and creation.
func (r Repository) Reconcile(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		var state string
		var version int64
		var deadline, now time.Time
		if err := tx.QueryRow(ctx, `SELECT status,version,expires_at FROM access_requests WHERE request_id=$1 FOR UPDATE`, id).Scan(&state, &version, &deadline); err != nil {
			return err
		}
		if !beforeAdmission(state) {
			return nil
		}
		var consent, use string
		var until time.Time
		err := tx.QueryRow(ctx, `SELECT status,use_state,valid_until FROM consent_state WHERE request_id=$1 FOR UPDATE`, id).Scan(&consent, &use, &until)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if use == "RESERVED" {
			return nil
		}
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		event := ""
		switch {
		case consent == "WITHDRAWN":
			event = "withdrawn"
		case consent == "REJECTED":
			event = "rejected"
		case !now.Before(deadline) || consent == "EXPIRED" || consent != "" && !now.Before(until):
			event = "expired"
		case consent == "ACTIVE" && state == "CONSENT_PENDING":
			event = "consent_activated"
		}
		if event == "" {
			return nil
		}
		_, err = AdvanceTx(ctx, tx, id, version, event)
		return err
	})
}
func beforeAdmission(state string) bool {
	return state == "AUTHENTICATION_PENDING" || state == "BINDING_PENDING" || state == "CONSENT_PENDING" || state == "CONSENTED" || state == "AUTHORIZED"
}
