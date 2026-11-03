package consentworker

import (
	"context"
	"errors"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

// Sweep uses DB deadlines and bounded batches. It retains consent provenance,
// acknowledged deliveries and consumed reservations for their evidence policy.
func (w *Worker) Sweep(ctx context.Context) error {
	rows, err := w.pool.Query(ctx, `SELECT c.consent_id FROM consent_state c JOIN consent_requests r USING(consent_id) WHERE c.status IN ('PENDING','APPROVAL_RECORDED','ACTIVE') AND c.valid_until<=clock_timestamp() ORDER BY c.valid_until LIMIT 32`)
	if err != nil {
		return err
	}
	var consents []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		consents = append(consents, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range consents {
		if _, err = w.consents.Expire(ctx, id); err != nil && !errors.Is(err, cm.ErrNotDue) {
			return err
		}
	}
	// Reconciliation also repairs a committed state whose notification was delayed.
	rows, err = w.pool.Query(ctx, `SELECT r.request_id FROM access_requests r LEFT JOIN consent_state c USING(request_id) WHERE r.status IN ('AUTHENTICATION_PENDING','BINDING_PENDING','CONSENT_PENDING','CONSENTED','AUTHORIZED') AND (r.expires_at<=clock_timestamp() OR c.status IN ('REJECTED','WITHDRAWN','EXPIRED') OR c.status='ACTIVE' AND r.status='CONSENT_PENDING') AND COALESCE(c.use_state,'AVAILABLE')='AVAILABLE' ORDER BY r.expires_at,r.request_id LIMIT 32`)
	if err != nil {
		return err
	}
	var requests []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		requests = append(requests, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range requests {
		if err = (access.Repository{Pool: w.pool}).Reconcile(ctx, id); err != nil {
			return err
		}
	}
	_, err = w.pool.Exec(ctx, `DELETE FROM browser_state WHERE (kind,key_digest) IN (SELECT kind,key_digest FROM browser_state WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT 128)`)
	if err != nil {
		return err
	}
	// Keep create-idempotency metadata for the explicit 24h retry window after expiry.
	_, err = w.pool.Exec(ctx, `DELETE FROM accepted_requests WHERE request_id IN (SELECT a.request_id FROM accepted_requests a JOIN access_requests r USING(request_id) WHERE r.expires_at<clock_timestamp()-interval '24 hours' AND r.status IN ('COMPLETED','DELIVERY_UNKNOWN','REJECTED','DENIED','WITHDRAWN','EXPIRED','FAILED','CANCELLED') ORDER BY r.expires_at LIMIT 32)`)
	return err
}
