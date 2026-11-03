package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/testdb"
)

func TestDeliveryCompetitionSubscribersAndAcknowledgement(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	for i := range 16 {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return outbox.Append(ctx, tx, "test", fmt.Sprint(i), 1, "test.event", map[string]any{"type": "test.event"})
		}); err != nil {
			t.Fatal(err)
		}
	}
	q, _ := outbox.NewQueue(pool, "one", []string{"test.event"}, time.Minute)
	deliveries := make(chan outbox.Delivery, 32)
	failures := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, ok, err := q.Claim(ctx)
			if err != nil {
				failures <- err
			}
			if ok {
				deliveries <- d
			}
		}()
	}
	wg.Wait()
	close(deliveries)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	seen := map[string]bool{}
	for d := range deliveries {
		if seen[d.EventID] {
			t.Fatal("concurrent worker received a live lease twice")
		}
		seen[d.EventID] = true
		if d.Attempt != 1 {
			t.Fatal("attempt count not durable")
		}
		wrong := d
		wrong.Token = "forged"
		if err := q.Ack(ctx, wrong); !errors.Is(err, outbox.ErrLease) {
			t.Fatal("forged lease acknowledged", err)
		}
		if err := q.Ack(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 16 {
		t.Fatal("work lost", len(seen))
	}
	if _, ok, err := q.Claim(ctx); err != nil || ok {
		t.Fatal("acknowledged event redelivered", err)
	}
	other, _ := outbox.NewQueue(pool, "two", []string{"test.event"}, time.Minute)
	if _, ok, err := other.Claim(ctx); err != nil || !ok {
		t.Fatal("one subscriber consumed another's event", err)
	}
	var published int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NOT NULL`).Scan(&published); err != nil || published != 0 {
		t.Fatal("subscriber rewrote global publication state", err)
	}
}
func TestDeliveryLeaseExpiryRetryAndRestart(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return outbox.Append(ctx, tx, "test", "aggregate", 1, "test.event", map[string]any{})
	}); err != nil {
		t.Fatal(err)
	}
	q, _ := outbox.NewQueue(pool, "worker", []string{"test.event"}, 500*time.Millisecond)
	first, ok, err := q.Claim(ctx)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, ok, err = q.Claim(ctx); err != nil || ok {
		t.Fatal("live lease was stolen", err)
	}
	if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.55)`); err != nil {
		t.Fatal(err)
	}
	if err = q.Ack(ctx, first); !errors.Is(err, outbox.ErrLease) {
		t.Fatal("expired lease acknowledged", err)
	}
	restarted, _ := outbox.NewQueue(pool, "worker", []string{"test.event"}, time.Minute)
	second, ok, err := restarted.Claim(ctx)
	if err != nil || !ok || second.EventID != first.EventID || second.Token == first.Token || second.Attempt != 2 {
		t.Fatal("restart lost stable event or fencing", err)
	}
	if err = q.Ack(ctx, first); !errors.Is(err, outbox.ErrLease) {
		t.Fatal("stale worker overwrote new lease", err)
	}
	if err = restarted.Retry(ctx, second, 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = restarted.Claim(ctx); err != nil || ok {
		t.Fatal("retry ignored database backoff", err)
	}
	if _, err = pool.Exec(ctx, `SELECT pg_sleep(0.55)`); err != nil {
		t.Fatal(err)
	}
	third, ok, err := restarted.Claim(ctx)
	if err != nil || !ok || third.EventID != first.EventID || third.Attempt != 3 {
		t.Fatal("retry identity changed", err)
	}
	if err = restarted.Ack(ctx, third); err != nil {
		t.Fatal(err)
	}
}
