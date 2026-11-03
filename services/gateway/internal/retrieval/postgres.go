package retrieval

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

func (s *Service) claim(ctx context.Context, g az.Grant, compact string) (request, endpoint, error) {
	var input request
	var destination endpoint
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var raw []byte
		var digest, status, issued string
		var version int64
		var until, now time.Time
		err := tx.QueryRow(ctx, `SELECT a.snapshot_bytes,r.request_digest,r.status,r.version,r.expires_at,g.jws,clock_timestamp() FROM access_requests r JOIN accepted_requests a USING(request_id) JOIN retrieval_grants g USING(request_id) WHERE r.request_id=$1 AND r.consumer_id=$2 FOR UPDATE OF r`, g.RequestID, g.Consumer).Scan(&raw, &digest, &status, &version, &until, &issued, &now)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDenied
		}
		if err != nil {
			return err
		}
		if issued != compact || digest != g.Digest {
			return ErrDenied
		}
		if status != "AUTHORIZED" {
			return ErrConflict
		}
		if !now.Before(until) || g.ExpiresAt <= now.Unix() {
			return az.ErrExpired
		}
		var snapshot cm.Snapshot
		if decode(raw, &snapshot) != nil || cm.ValidateSnapshot(snapshot) != nil {
			return ErrDenied
		}
		actual, err := cm.SnapshotDigest(snapshot)
		if err != nil || actual != g.Digest || snapshot.RequestID != g.RequestID || snapshot.ConsumerID != g.Consumer || !snapshot.ExpiresAt.Equal(until) {
			return ErrDenied
		}
		e, ok := s.endpoints[snapshot.Request.Registry]
		if !ok || e.descriptor.Audience != g.Audience {
			return ErrDenied
		}
		destination = e
		input = request{AttemptID: "attempt_" + rand.Text(), Snapshot: snapshot, Digest: g.Digest, Grant: compact, Presenter: az.Presenter{Consumer: g.Consumer, Thumbprint: g.Confirmation.Thumbprint}}
		inserted, err := tx.Exec(ctx, `INSERT INTO gateway_retrieval_attempts(request_id,attempt_id,consumer_id,grant_digest,presenter_thumbprint,deadline) VALUES($1,$2,$3,$4,$5,clock_timestamp()+interval '60 seconds') ON CONFLICT DO NOTHING`, g.RequestID, input.AttemptID, g.Consumer, cm.Digest([]byte(compact)), g.Confirmation.Thumbprint)
		if err != nil {
			return err
		}
		if inserted.RowsAffected() != 1 {
			return ErrConflict
		}
		return outbox.Append(ctx, tx, "retrieval", g.RequestID, 1, "retrieval.dispatched", map[string]any{"request_id": g.RequestID, "attempt_id": input.AttemptID, "request_digest": g.Digest})
	})
	return input, destination, err
}

type lockedAttempt struct {
	requestStatus  string
	requestVersion int64
	state          string
	version        int64
	deadline, now  time.Time
}

func lock(ctx context.Context, tx pgx.Tx, id, attempt string) (lockedAttempt, error) {
	var v lockedAttempt
	if err := tx.QueryRow(ctx, `SELECT status,version FROM access_requests WHERE request_id=$1 FOR UPDATE`, id).Scan(&v.requestStatus, &v.requestVersion); err != nil {
		return v, err
	}
	err := tx.QueryRow(ctx, `SELECT state,version,deadline,clock_timestamp() FROM gateway_retrieval_attempts WHERE request_id=$1 AND attempt_id=$2 FOR UPDATE`, id, attempt).Scan(&v.state, &v.version, &v.deadline, &v.now)
	return v, err
}
func (s *Service) release(ctx context.Context, g az.Grant, input request, e endpoint, receipt string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		v, err := lock(ctx, tx, g.RequestID, input.AttemptID)
		if err != nil {
			return err
		}
		if v.state != "DISPATCHED" || v.requestStatus != "EVIDENCE_COMMITTED" || !v.now.Before(v.deadline) {
			return ErrConflict
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM evidence_receipts WHERE receipt_id=$1 AND adapter_id=$2 AND request_id=$3 AND request_digest=$4 AND grant_jti=$5 AND attempt_id=$6 AND outcome='release_ready')`, receipt, e.descriptor.AdapterID, g.RequestID, g.Digest, g.JTI, input.AttemptID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrDenied
		}
		next, err := transition(ctx, v.state, "release_started")
		if err != nil {
			return err
		}
		if _, err = access.AdvanceTx(ctx, tx, g.RequestID, v.requestVersion, "release_started"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE gateway_retrieval_attempts SET state=$5,version=version+1,receipt_id=$3,updated_at=clock_timestamp() WHERE request_id=$1 AND attempt_id=$2 AND version=$4`, g.RequestID, input.AttemptID, receipt, v.version, next)
		return err
	})
}
func (s *Service) finish(ctx context.Context, id, attempt string, written bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		v, err := lock(ctx, tx, id, attempt)
		if err != nil {
			return err
		}
		if v.state != "RELEASE_ATTEMPTED" {
			return nil
		}
		if v.requestStatus != "RELEASE_ATTEMPTED" {
			return ErrConflict
		}
		event := "delivery_uncertain"
		if written && v.now.Before(v.deadline) {
			event = "response_written"
		}
		next, err := transition(ctx, v.state, event)
		if err != nil {
			return err
		}
		if _, err = access.AdvanceTx(ctx, tx, id, v.requestVersion, event); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE gateway_retrieval_attempts SET state=$3,version=version+1,updated_at=clock_timestamp() WHERE request_id=$1 AND attempt_id=$2 AND version=$4`, id, attempt, next, v.version)
		return err
	})
}
func (s *Service) fail(ctx context.Context, id, attempt string, expiredOnly bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		v, err := lock(ctx, tx, id, attempt)
		if err != nil {
			return err
		}
		if v.state != "DISPATCHED" && v.state != "RELEASE_ATTEMPTED" {
			return nil
		}
		if expiredOnly && v.now.Before(v.deadline) {
			return nil
		}
		event, next := "", "FAILED"
		switch v.requestStatus {
		case "AUTHORIZED":
			event = "failed"
		case "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED":
			event, next = "delivery_uncertain", "DELIVERY_UNKNOWN"
		case "DELIVERY_UNKNOWN":
			next = "DELIVERY_UNKNOWN"
		}
		if event != "" {
			if _, err = access.AdvanceTx(ctx, tx, id, v.requestVersion, event); err != nil {
				return err
			}
		}
		dispatchEvent := "failed"
		if next == "DELIVERY_UNKNOWN" || v.state == "RELEASE_ATTEMPTED" {
			dispatchEvent = "delivery_uncertain"
		}
		next, err = transition(ctx, v.state, dispatchEvent)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE gateway_retrieval_attempts SET state=$3,version=version+1,updated_at=clock_timestamp() WHERE request_id=$1 AND attempt_id=$2 AND version=$4`, id, attempt, next, v.version)
		return err
	})
}

// Sweep classifies abandoned dispatches; it never resends an adapter request.
func (s *Service) Sweep(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT request_id,attempt_id FROM gateway_retrieval_attempts WHERE state IN ('DISPATCHED','RELEASE_ATTEMPTED') AND deadline<=clock_timestamp() ORDER BY deadline,request_id LIMIT 32`)
	if err != nil {
		return err
	}
	type item struct{ id, attempt string }
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.attempt); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		if err = s.fail(ctx, i.id, i.attempt, true); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer func() {
		s.discovery.Close()
		for _, e := range s.endpoints {
			e.client.CloseIdleConnections()
		}
	}()
	for ctx.Err() == nil {
		operation, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := s.Sweep(operation); err != nil && ctx.Err() == nil {
			log.Print("retrieval recovery deferred; occupied attempts retained")
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
