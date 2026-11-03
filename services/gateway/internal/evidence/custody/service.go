package custody

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

func New(c Config) (*Service, error) {
	if c.Pool == nil || c.Scopes == nil || len(c.Adapters) == 0 || c.ReleaseProfile != "batch" {
		return nil, errors.New("evidence dependencies and explicit durable batch profile required")
	}
	s := &Service{c.Pool, c.Scopes, map[string]Adapter{}}
	peers := map[string]bool{}
	audiences := map[string]bool{}
	for _, a := range c.Adapters {
		if !identifier.MatchString(a.ID) || !identifier.MatchString(a.Audience) || a.PeerName == "" || s.adapters[a.ID].ID != "" || peers[a.PeerName] || audiences[a.Audience] {
			return nil, ErrDenied
		}
		var err error
		a.Keys, err = copyKeys(a.Keys)
		if err != nil {
			return nil, err
		}
		s.adapters[a.ID] = a
		peers[a.PeerName] = true
		audiences[a.Audience] = true
	}
	return s, nil
}
func (s *Service) Record(ctx context.Context, adapterID, compact string) (Acknowledgement, error) {
	adapter, ok := s.adapters[adapterID]
	if !ok {
		return Acknowledgement{}, ErrDenied
	}
	event, err := verify(compact, adapter.Keys)
	if err != nil || event.AdapterID != adapter.ID {
		return Acknowledgement{}, ErrDenied
	}
	grant, reservation, err := s.scopes.EvidenceScope(ctx, adapter.Audience, event.RedemptionID)
	if err != nil {
		return Acknowledgement{}, err
	}
	if grant.RequestID != event.RequestID || grant.Digest != event.Digest || grant.ConsentID != event.ConsentID || grant.JTI != event.GrantJTI || reservation.AttemptID != event.AttemptID || !slices.Equal(grant.Fields, event.Fields) || event.OccurredAt.Before(reservation.ReservedAt.Add(-30*time.Second)) {
		return Acknowledgement{}, ErrDenied
	}
	digest := cm.Digest([]byte(compact))
	var result Acknowledgement
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// The request row serializes receipt creation and lifecycle projection. Event
		// IDs are globally immutable; collisions across requests also serialize below.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "evidence:"+event.EventID); err != nil {
			return err
		}
		var status, requestDigest string
		var version int64
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT status,request_digest,version,clock_timestamp() FROM access_requests WHERE request_id=$1 FOR UPDATE`, event.RequestID).Scan(&status, &requestDigest, &version, &now); err != nil {
			return err
		}
		if requestDigest != event.Digest || event.OccurredAt.After(now.Add(30*time.Second)) {
			return ErrDenied
		}
		var oldCompact, oldAdapter string
		err := tx.QueryRow(ctx, `SELECT receipt_id,evidence_digest,accepted_at,jws,adapter_id FROM evidence_receipts WHERE event_id=$1`, event.EventID).Scan(&result.ReceiptID, &result.Digest, &result.AcceptedAt, &oldCompact, &oldAdapter)
		if err == nil {
			if oldCompact != compact || oldAdapter != adapterID {
				return ErrConflict
			}
			result.AnchorStatus = "pending"
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if event.Outcome == "release_ready" {
			if status != "REDEEMING" {
				return ErrConflict
			}
		}
		result = Acknowledgement{ReceiptID: "rcpt_" + rand.Text(), Digest: digest, AcceptedAt: now.UTC(), AnchorStatus: "pending"}
		if _, err = tx.Exec(ctx, `INSERT INTO evidence_receipts(receipt_id,event_id,adapter_id,request_id,request_digest,consent_id,grant_jti,attempt_id,redemption_id,outcome,evidence_digest,jws,accepted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, result.ReceiptID, event.EventID, adapterID, event.RequestID, event.Digest, event.ConsentID, event.GrantJTI, event.AttemptID, event.RedemptionID, event.Outcome, digest, compact, result.AcceptedAt); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO evidence_anchor_state(receipt_id) VALUES($1)`, result.ReceiptID); err != nil {
			return err
		}
		if err = outbox.Append(ctx, tx, "evidence", result.ReceiptID, 1, "evidence.received", map[string]any{"receipt_id": result.ReceiptID, "evidence_digest": digest, "request_id": event.RequestID}); err != nil {
			return err
		}
		transition := ""
		switch event.Outcome {
		case "release_ready":
			transition = "evidence_committed"
		case "failed":
			if status == "REDEEMING" || status == "EVIDENCE_COMMITTED" {
				transition = "failed"
			} else if status == "RELEASE_ATTEMPTED" {
				transition = "delivery_uncertain"
			}
		case "delivery_unknown":
			if status == "REDEEMING" || status == "EVIDENCE_COMMITTED" || status == "RELEASE_ATTEMPTED" {
				transition = "delivery_uncertain"
			}
		}
		if transition != "" {
			_, err = access.AdvanceTx(ctx, tx, event.RequestID, version, transition)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Acknowledgement{}, err
	}
	return result, nil
}
