package outbox

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrLease = errors.New("outbox delivery lease lost")

// Queue retains per-subscriber acknowledgements. Payloads stay in the outbox;
// handlers use stable aggregate IDs to re-read their authoritative state.
type Queue struct {
	pool       *pgxpool.Pool
	subscriber string
	eventTypes []string
	lease      time.Duration
}
type Delivery struct {
	EventID, AggregateID, EventType, Token string
	AggregateVersion, Attempt              int64
}

func NewQueue(pool *pgxpool.Pool, subscriber string, eventTypes []string, lease time.Duration) (*Queue, error) {
	if pool == nil || subscriber == "" || len(subscriber) > 128 || len(eventTypes) == 0 || len(eventTypes) > 32 || lease < time.Millisecond || lease > 5*time.Minute {
		return nil, errors.New("bounded outbox subscriber, event types and lease required")
	}
	return &Queue{pool, subscriber, append([]string(nil), eventTypes...), lease}, nil
}
func (q *Queue) Claim(ctx context.Context) (Delivery, bool, error) {
	var d Delivery
	err := pgx.BeginFunc(ctx, q.pool, func(tx pgx.Tx) error {
		// Bound catch-up work; concurrent seeders use the same primary key safely.
		_, err := tx.Exec(ctx, `INSERT INTO outbox_deliveries(subscriber,event_id) SELECT $1,e.event_id FROM outbox e LEFT JOIN outbox_deliveries d ON d.subscriber=$1 AND d.event_id=e.event_id WHERE e.event_type=ANY($2) AND d.event_id IS NULL ORDER BY e.created_at,e.event_id LIMIT 32 ON CONFLICT DO NOTHING`, q.subscriber, q.eventTypes)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT e.event_id,e.aggregate_id,e.event_type,e.aggregate_version,d.attempts FROM outbox_deliveries d JOIN outbox e USING(event_id) WHERE d.subscriber=$1 AND e.event_type=ANY($2) AND d.acknowledged_at IS NULL AND d.available_at<=clock_timestamp() AND (d.lease_until IS NULL OR d.lease_until<=clock_timestamp()) ORDER BY d.available_at,e.created_at,e.event_id FOR UPDATE OF d SKIP LOCKED LIMIT 1`, q.subscriber, q.eventTypes).Scan(&d.EventID, &d.AggregateID, &d.EventType, &d.AggregateVersion, &d.Attempt)
		if err != nil {
			return err
		}
		d.Token = rand.Text()
		d.Attempt++
		_, err = tx.Exec(ctx, `UPDATE outbox_deliveries SET lease_token=$3,lease_until=clock_timestamp()+$4*interval '1 millisecond',attempts=attempts+1 WHERE subscriber=$1 AND event_id=$2`, q.subscriber, d.EventID, d.Token, q.lease.Milliseconds())
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, false, nil
	}
	return d, err == nil, err
}
func (q *Queue) Ack(ctx context.Context, d Delivery) error {
	result, err := q.pool.Exec(ctx, `UPDATE outbox_deliveries SET acknowledged_at=clock_timestamp(),lease_token=NULL,lease_until=NULL WHERE subscriber=$1 AND event_id=$2 AND lease_token=$3 AND acknowledged_at IS NULL AND lease_until>clock_timestamp()`, q.subscriber, d.EventID, d.Token)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrLease
	}
	return nil
}
func (q *Queue) Retry(ctx context.Context, d Delivery, delay time.Duration) error {
	if delay < 0 || delay > time.Hour {
		return errors.New("bounded retry delay required")
	}
	result, err := q.pool.Exec(ctx, `UPDATE outbox_deliveries SET available_at=clock_timestamp()+$4*interval '1 millisecond',lease_token=NULL,lease_until=NULL WHERE subscriber=$1 AND event_id=$2 AND lease_token=$3 AND acknowledged_at IS NULL AND lease_until>clock_timestamp()`, q.subscriber, d.EventID, d.Token, delay.Milliseconds())
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrLease
	}
	return nil
}
