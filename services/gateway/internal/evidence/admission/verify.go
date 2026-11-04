// Package admission validates the retained event for a single completed retrieval.
package admission

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Result struct {
	RequestID string    `json:"request_id"`
	EventID   string    `json:"authorization_event_id"`
	UsedAt    time.Time `json:"approval_used_at"`
	ReceiptID string    `json:"signed_receipt_id"`
	Used      int       `json:"authorized_retrievals_used"`
}

func Verify(ctx context.Context, pool *pgxpool.Pool, id string) (Result, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Result{}, errors.New("audit database unavailable")
	}
	defer tx.Rollback(ctx)
	var result Result
	result.RequestID = id
	err = tx.QueryRow(ctx, `
		SELECT min(o.event_id),r.reserved_at,e.receipt_id,count(*)
		FROM access_requests a
		JOIN consent_state c ON c.request_id=a.request_id AND c.use_state='RESERVED'
		JOIN disclosure_reservations r ON r.consent_id=c.consent_id
		JOIN retrieval_grants k ON k.request_id=a.request_id AND k.consent_id=c.consent_id AND k.jti=r.grant_jti
		JOIN gateway_retrieval_attempts g ON g.request_id=a.request_id AND g.attempt_id=r.attempt_id
			AND g.consumer_id=a.consumer_id AND g.presenter_thumbprint=r.presenter_thumbprint AND g.grant_digest=r.grant_digest
		JOIN evidence_receipts e ON e.receipt_id=g.receipt_id AND e.request_id=a.request_id
			AND e.request_digest=a.request_digest AND e.consent_id=c.consent_id AND e.attempt_id=r.attempt_id
			AND e.grant_jti=r.grant_jti AND e.redemption_id=r.redemption_id AND e.adapter_id=r.adapter_id
		JOIN outbox o ON o.aggregate_type='request' AND o.aggregate_id=a.request_id
			AND o.event_type='request.transitioned' AND o.payload->>'status'='REDEEMING'
		WHERE a.request_id=$1 AND a.status='COMPLETED' AND g.state='COMPLETED' AND e.outcome='release_ready'
		GROUP BY r.reserved_at,e.receipt_id
		HAVING count(*)=1 AND bool_and(o.payload->>'request_id'=a.request_id
			AND o.payload->>'request_digest'=a.request_digest AND o.payload->>'event_id'=o.event_id
			AND o.created_at>=r.reserved_at AND o.created_at<=e.accepted_at)`, id).
		Scan(&result.EventID, &result.UsedAt, &result.ReceiptID, &result.Used)
	if err != nil {
		return Result{}, errors.New("completed retrieval does not have one consistent admission event and receipt")
	}
	result.UsedAt = result.UsedAt.UTC()
	return result, tx.Commit(ctx)
}
