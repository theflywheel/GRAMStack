// Package consentworker recovers consent work from committed metadata outbox
// events. SQL authority, not delivery order or a lease, determines permission.
package consentworker

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

type Worker struct {
	pool                   *pgxpool.Pool
	consents               *cm.Service
	signing, notifications *outbox.Queue
}

func New(pool *pgxpool.Pool, consents *cm.Service) (*Worker, error) {
	if pool == nil || consents == nil {
		return nil, errors.New("consent worker dependencies required")
	}
	signing, err := outbox.NewQueue(pool, "consent.signing.v1", []string{"consent.approval_recorded"}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	notifications, err := outbox.NewQueue(pool, "gateway.consent.v1", []string{"consent.active", "consent.rejected", "consent.withdrawn", "consent.cancelled", "consent.expired"}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	return &Worker{pool, consents, signing, notifications}, nil
}
func (w *Worker) activate(ctx context.Context, id string) error {
	status, err := w.consents.Status(ctx, id)
	if err != nil {
		return err
	}
	if status.Status != "APPROVAL_RECORDED" {
		return nil
	}
	_, err = w.consents.Activate(ctx, id)
	if errors.Is(err, cm.ErrExpired) {
		_, err = w.consents.Expire(ctx, id)
	}
	if errors.Is(err, cm.ErrConflict) || errors.Is(err, cm.ErrNotReady) {
		// A concurrent stop or another activation may have won while signing.
		current, readErr := w.consents.Status(ctx, id)
		if readErr != nil {
			return readErr
		}
		if current.Status != "APPROVAL_RECORDED" {
			return nil
		}
	}
	return err
}
func (w *Worker) reconcile(ctx context.Context, id string) error {
	var requestID string
	if err := w.pool.QueryRow(ctx, `SELECT request_id FROM consent_state WHERE consent_id=$1`, id).Scan(&requestID); err != nil {
		return err
	}
	return (access.Repository{Pool: w.pool}).Reconcile(ctx, requestID)
}

// Deliver handles one leased event. An effect followed by a lost acknowledgement
// can be replayed safely; a retry never invents fresh approval or a second artefact.
func (w *Worker) Deliver(ctx context.Context, signing bool) (bool, error) {
	queue, handler := w.notifications, w.reconcile
	if signing {
		queue, handler = w.signing, w.activate
	}
	d, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		return false, err
	}
	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = handler(operation, d.AggregateID)
	cancel()
	if err == nil {
		return true, queue.Ack(ctx, d)
	}
	delay := time.Second << min(d.Attempt-1, 6)
	if retryErr := queue.Retry(ctx, d, delay); retryErr != nil {
		return true, retryErr
	}
	return true, err
}

// Run bounds concurrency to one signing handler, one notification handler and
// one maintenance batch. Multiple processes compete through the same SQL leases.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, signing := range []bool{true, false} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				didWork, err := w.Deliver(ctx, signing)
				if err != nil && ctx.Err() == nil {
					log.Print("consent worker delivery deferred; durable retry retained")
				}
				if !didWork || err != nil {
					if !pause(ctx) {
						return
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			operation, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := w.Sweep(operation)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Print("consent worker maintenance deferred")
			}
			if !pause(ctx) {
				return
			}
		}
	}()
	wg.Wait()
}
func pause(ctx context.Context) bool {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
