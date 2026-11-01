package enforcement

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	"github.com/theflywheel/GRAMStack/services/gateway/migrations"
)

func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("GRAMSTACK_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = migrations.Apply(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	// A second application start must see the same migration checksum and no-op.
	if err = migrations.Apply(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
func active(t *testing.T, pool *pgxpool.Pool) VerifiedRedemption {
	t.Helper()
	id := rand.Text()
	digest := "sha256:" + strings.Repeat("a", 64)
	until := time.Now().Add(time.Minute)
	if err := (access.Repository{Pool: pool}).Create(context.Background(), id, "test-consumer", digest, until); err != nil {
		t.Fatal(err)
	}
	if err := (Repository{Pool: pool}).CreatePending(context.Background(), id, id, digest, until); err != nil {
		t.Fatal(err)
	}
	// Test fixture only: activation is not exposed as a bypass in application code.
	if _, err := pool.Exec(context.Background(), "UPDATE consent_state SET status='ACTIVE',status_version=3 WHERE consent_id=$1", id); err != nil {
		t.Fatal(err)
	}
	return VerifiedRedemption{ConsentID: id, RequestID: id, RequestDigest: digest, GrantJTI: "grant-" + id, GrantDigest: digest, AdapterID: "adapter-1", AttemptID: "attempt-" + id, PresenterThumbprint: strings.Repeat("a", 43), GrantExpiresAt: until}
}
func TestPostgresConcurrentRedemption(t *testing.T) {
	pool := database(t)
	repo := Repository{Pool: pool}
	input := active(t, pool)
	const n = 20
	results := make(chan Reservation, n)
	failures := make(chan error, n)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range n {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			value, err := repo.Redeem(context.Background(), input)
			if err != nil {
				failures <- err
			} else {
				results <- value
			}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	admitted := 0
	var redemption string
	for result := range results {
		if result.FirstRedemption {
			admitted++
		}
		if redemption != "" && redemption != result.ID {
			t.Fatal("multiple reservations")
		}
		redemption = result.ID
	}
	if admitted != 1 {
		t.Fatalf("first admissions=%d", admitted)
	}
	changed := input
	changed.AttemptID = "other-attempt"
	if _, err := repo.Redeem(context.Background(), changed); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second attempt: %v", err)
	}
	changed = input
	changed.GrantDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := repo.Redeem(context.Background(), changed); !errors.Is(err, ErrBinding) {
		t.Fatalf("changed grant: %v", err)
	}
	if _, err := repo.Withdraw(context.Background(), input.ConsentID, 0); err != nil {
		t.Fatal(err)
	}
	retry, err := repo.Redeem(context.Background(), input)
	if err != nil || retry.FirstRedemption {
		t.Fatalf("metadata retry: %+v %v", retry, err)
	}
	var count int
	if err = pool.QueryRow(context.Background(), "SELECT count(*) FROM disclosure_reservations WHERE consent_id=$1", input.ConsentID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d %v", count, err)
	}
}
func TestPostgresWithdrawalRace(t *testing.T) {
	pool := database(t)
	repo := Repository{Pool: pool}
	for range 12 {
		input := active(t, pool)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var reservation Reservation
		var redeemErr, withdrawErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; reservation, redeemErr = repo.Redeem(context.Background(), input) }()
		go func() {
			defer wg.Done()
			<-start
			_, withdrawErr = repo.Withdraw(context.Background(), input.ConsentID, 0)
		}()
		close(start)
		wg.Wait()
		if withdrawErr != nil {
			t.Fatal(withdrawErr)
		}
		var status, use string
		var count int
		if err := pool.QueryRow(context.Background(), "SELECT status,use_state FROM consent_state WHERE consent_id=$1", input.ConsentID).Scan(&status, &use); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM disclosure_reservations WHERE consent_id=$1", input.ConsentID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if status != "WITHDRAWN" {
			t.Fatal(status)
		}
		if redeemErr == nil {
			if !reservation.FirstRedemption || use != "RESERVED" || count != 1 {
				t.Fatal("reservation lost after withdrawal")
			}
		} else if !errors.Is(redeemErr, ErrInactive) || use != "AVAILABLE" || count != 0 {
			t.Fatalf("invalid race outcome: %v use=%s count=%d", redeemErr, use, count)
		}
	}
}
func TestPostgresLifecycleCASAndExpiry(t *testing.T) {
	pool := database(t)
	requests := access.Repository{Pool: pool}
	input := active(t, pool)
	if _, err := requests.Advance(context.Background(), input.RequestID, 1, "authenticated"); err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Advance(context.Background(), input.RequestID, 1, "subject_verified"); !errors.Is(err, access.ErrVersion) {
		t.Fatalf("stale version accepted: %v", err)
	}
	if _, err := requests.Advance(context.Background(), input.RequestID, 2, "grant_issued"); err == nil {
		t.Fatal("invalid FSM event accepted")
	}
	expired := active(t, pool)
	expired.GrantExpiresAt = time.Now().Add(-time.Second)
	if _, err := (Repository{Pool: pool}).Redeem(context.Background(), expired); !errors.Is(err, ErrInactive) {
		t.Fatalf("expired grant: %v", err)
	}
	var events int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox WHERE aggregate_type='request' AND aggregate_id=$1", input.RequestID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events=%d %v", events, err)
	}
}

func TestPostgresDistinctAttemptsAndRestart(t *testing.T) {
	pool := database(t)
	repo := Repository{Pool: pool}
	input := active(t, pool)
	const n = 16
	type outcome struct {
		reservation Reservation
		err         error
		input       VerifiedRedemption
	}
	results := make(chan outcome, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		candidate := input
		candidate.AttemptID = rand.Text()
		candidate.GrantJTI = rand.Text()
		wg.Add(1)
		go func(value VerifiedRedemption) {
			defer wg.Done()
			<-start
			r, err := repo.Redeem(context.Background(), value)
			results <- outcome{r, err, value}
		}(candidate)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	var winner VerifiedRedemption
	for result := range results {
		if result.err == nil {
			winners++
			winner = result.input
			if !result.reservation.FirstRedemption {
				t.Fatal("new attempt returned retry acknowledgement")
			}
		} else if !errors.Is(result.err, ErrConsumed) {
			t.Fatal(result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("distinct attempts admitted=%d", winners)
	}
	// A new connection pool has no access to process-local request metadata.
	pool.Close()
	newPool, err := pgxpool.New(context.Background(), os.Getenv("GRAMSTACK_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer newPool.Close()
	retry, err := (Repository{Pool: newPool}).Redeem(context.Background(), winner)
	if err != nil || retry.FirstRedemption {
		t.Fatalf("restart retry: %+v %v", retry, err)
	}
}

func TestPostgresOutboxFailureRollsBackReservation(t *testing.T) {
	pool := database(t)
	input := active(t, pool)
	// Force a real SQL error at the final write of the redemption transaction.
	_, err := pool.Exec(context.Background(), `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES($1,'consent',$2,4,'test.injected','{}')`, rand.Text(), input.ConsentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Repository{Pool: pool}).Redeem(context.Background(), input); err == nil {
		t.Fatal("outbox conflict ignored")
	}
	var use string
	var version int64
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT use_state,status_version FROM consent_state WHERE consent_id=$1", input.ConsentID).Scan(&use, &version); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM disclosure_reservations WHERE consent_id=$1", input.ConsentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if use != "AVAILABLE" || version != 3 || count != 0 {
		t.Fatalf("partial transaction committed: use=%s version=%d reservations=%d", use, version, count)
	}
}

func TestPostgresRequestDeadlineBeforeAndAfterAdmission(t *testing.T) {
	pool := database(t)
	requests := access.Repository{Pool: pool}
	input := active(t, pool)
	if _, err := pool.Exec(context.Background(), "UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=$1", input.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Advance(context.Background(), input.RequestID, 1, "authenticated"); err == nil {
		t.Fatal("expired request advanced")
	}
	if _, err := requests.Advance(context.Background(), input.RequestID, 1, "expired"); err != nil {
		t.Fatal(err)
	}
	admitted := active(t, pool)
	for i, event := range []string{"authenticated", "subject_verified", "consent_activated", "grant_issued", "retrieval_started"} {
		if _, err := requests.Advance(context.Background(), admitted.RequestID, int64(i+1), event); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(context.Background(), "UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=$1", admitted.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Advance(context.Background(), admitted.RequestID, 6, "evidence_committed"); err != nil {
		t.Fatalf("admitted outcome could not be recorded: %v", err)
	}
}
