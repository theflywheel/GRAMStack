// Package anchoring drains committed evidence work without touching source data
// or disclosure permission. Publication requires the actual pinned DeDi client.
package anchoring

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/looplab/fsm"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/custody"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/dedi"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

const Subscriber = "evidence.anchoring.v1"

type Worker struct {
	pool    *pgxpool.Pool
	custody *custody.Service
	client  *dedi.Client
	queue   *outbox.Queue
}

func New(pool *pgxpool.Pool, custodian *custody.Service, client *dedi.Client) (*Worker, error) {
	if pool == nil || custodian == nil || client == nil {
		return nil, errors.New("anchoring dependencies required")
	}
	queue, err := outbox.NewQueue(pool, Subscriber, []string{"evidence.received"}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return &Worker{pool, custodian, client, queue}, nil
}

// Handle may run again after a lost acknowledgement. Retained verified proof
// avoids another publication; create-only DeDi writes cover ambiguous failures.
func (w *Worker) Handle(ctx context.Context, d outbox.Delivery) error {
	if d.EventType != "evidence.received" || d.AggregateVersion != 1 {
		return errors.New("unexpected anchoring work")
	}
	_, digest, err := w.custody.VerifyStored(ctx, d.AggregateID)
	if err != nil {
		return err
	}
	var status string
	var raw []byte
	if err = w.pool.QueryRow(ctx, `SELECT status,anchor_reference FROM evidence_anchor_state WHERE receipt_id=$1`, d.AggregateID).Scan(&status, &raw); err != nil {
		return err
	}
	if status == "anchored" {
		var saved dedi.Anchor
		if json.Unmarshal(raw, &saved) != nil {
			return errors.New("invalid retained anchor")
		}
		return w.client.VerifyAnchor(d.AggregateID, digest, saved)
	}
	if status != "pending" {
		return errors.New("unknown anchor state")
	}
	anchor, err := w.client.Publish(ctx, d.AggregateID, digest)
	if err != nil {
		return err
	}
	if err = w.client.VerifyAnchor(d.AggregateID, digest, anchor); err != nil {
		return err
	}
	raw, err = json.Marshal(anchor)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		var current string
		var existing []byte
		if err := tx.QueryRow(ctx, `SELECT status,anchor_reference FROM evidence_anchor_state WHERE receipt_id=$1 FOR UPDATE`, d.AggregateID).Scan(&current, &existing); err != nil {
			return err
		}
		if current == "anchored" {
			var saved dedi.Anchor
			if json.Unmarshal(existing, &saved) != nil {
				return errors.New("invalid retained anchor")
			}
			return w.client.VerifyAnchor(d.AggregateID, digest, saved)
		}
		machine := fsm.NewFSM(current, fsm.Events{{Name: "verified", Src: []string{"pending"}, Dst: "anchored"}}, nil)
		if err := machine.Event(ctx, "verified"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE evidence_anchor_state SET status=$2,anchor_reference=$3,updated_at=clock_timestamp() WHERE receipt_id=$1 AND status='pending'`, d.AggregateID, machine.Current(), raw); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, "anchor", d.AggregateID, 1, "evidence.anchored", map[string]any{"receipt_id": d.AggregateID, "evidence_digest": digest, "record_id": anchor.RecordID})
	})
}
func (w *Worker) Deliver(ctx context.Context) (bool, error) {
	delivery, ok, err := w.queue.Claim(ctx)
	if err != nil || !ok {
		return false, err
	}
	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = w.Handle(operation, delivery)
	cancel()
	if err == nil {
		return true, w.queue.Ack(ctx, delivery)
	}
	if retryErr := w.queue.Retry(ctx, delivery, time.Second<<min(delivery.Attempt-1, 6)); retryErr != nil {
		return true, retryErr
	}
	return true, err
}
func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := w.Deliver(ctx)
		if err != nil && ctx.Err() == nil {
			log.Print("evidence anchoring deferred; durable retry retained")
		}
		if !worked || err != nil {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
