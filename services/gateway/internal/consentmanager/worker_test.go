package consentmanager_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consentworker"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/testdb"
)

func workerFixture(t *testing.T) (fixture, *consentworker.Worker) {
	t.Helper()
	f := setupPool(t, testdb.New(t))
	w, err := consentworker.New(f.pool, f.service)
	if err != nil {
		t.Fatal(err)
	}
	return f, w
}
func deliver(t *testing.T, w *consentworker.Worker, signing bool) {
	t.Helper()
	if ok, err := w.Deliver(context.Background(), signing); err != nil || !ok {
		t.Fatal("delivery unavailable", ok, err)
	}
}
func requestState(t *testing.T, f fixture, want string) int64 {
	t.Helper()
	var state string
	var version int64
	if err := f.pool.QueryRow(context.Background(), `SELECT status,version FROM access_requests WHERE request_id=$1`, f.input.Snapshot.RequestID).Scan(&state, &version); err != nil || state != want {
		t.Fatalf("request %s want %s: %v", state, want, err)
	}
	return version
}
func countArtefacts(t *testing.T, f fixture, want int) {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM consent_artefacts`).Scan(&count); err != nil || count != want {
		t.Fatal("unexpected signed artefacts", count, err)
	}
}

type countedSigner struct {
	signer cm.Signer
	calls  *atomic.Int32
}

func (s countedSigner) Sign(ctx context.Context, b []byte) (string, error) {
	s.calls.Add(1)
	return s.signer.Sign(ctx, b)
}

func TestWorkersCompeteAndRecoverCommittedEffects(t *testing.T) {
	f, _ := workerFixture(t)
	n := create(t, f)
	approve(t, f, n)
	var calls atomic.Int32
	config := f.config
	config.Signer = countedSigner{config.Signer, &calls}
	service, _ := cm.New(config)
	worker, _ := consentworker.New(f.pool, service)
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := worker.Deliver(context.Background(), true)
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if calls.Load() != 1 {
		t.Fatal("live signing lease duplicated work", calls.Load())
	}
	countArtefacts(t, f, 1)
	// Current ACTIVE state is projected only once even if its notification is replayed.
	q, _ := outbox.NewQueue(f.pool, "gateway.consent.v1", []string{"consent.active"}, 500*time.Millisecond)
	d, ok, err := q.Claim(context.Background())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err = (access.Repository{Pool: f.pool}).Reconcile(context.Background(), f.input.Snapshot.RequestID); err != nil {
		t.Fatal(err)
	}
	version := requestState(t, f, "CONSENTED")
	if _, err = f.pool.Exec(context.Background(), `SELECT pg_sleep(0.55)`); err != nil {
		t.Fatal(err)
	}
	restarted, _ := consentworker.New(f.pool, f.service)
	deliver(t, restarted, false)
	if got := requestState(t, f, "CONSENTED"); got != version {
		t.Fatal("replayed notification duplicated state")
	}
	if err = q.Ack(context.Background(), d); !errors.Is(err, outbox.ErrLease) {
		t.Fatal("old notification acknowledged after restart", err)
	}
}
func TestWorkerRecoversActivationBeforeAcknowledgement(t *testing.T) {
	f, _ := workerFixture(t)
	n := create(t, f)
	approve(t, f, n)
	ctx := context.Background()
	q, _ := outbox.NewQueue(f.pool, "consent.signing.v1", []string{"consent.approval_recorded"}, 500*time.Millisecond)
	d, ok, err := q.Claim(ctx)
	if err != nil || !ok {
		t.Fatal(err)
	}
	activation, err := f.service.Activate(ctx, n.ConsentRequestID)
	if err != nil {
		t.Fatal(err)
	}
	// The first worker disappears after its effect commits but before Ack.
	if _, err = f.pool.Exec(ctx, `SELECT pg_sleep(0.55)`); err != nil {
		t.Fatal(err)
	}
	restarted, _ := consentworker.New(f.pool, f.service)
	deliver(t, restarted, true)
	stored, err := f.service.GetArtefact(ctx, n.ConsentRequestID)
	if err != nil || stored != activation.JWS {
		t.Fatal("recovery changed immutable signed bytes", err)
	}
	if err = q.Ack(ctx, d); !errors.Is(err, outbox.ErrLease) {
		t.Fatal("lost worker acknowledged new attempt", err)
	}
	countArtefacts(t, f, 1)
}
func TestWorkerSigningOutageBackoffAndRecovery(t *testing.T) {
	f, good := workerFixture(t)
	n := create(t, f)
	approve(t, f, n)
	config := f.config
	config.Signer = failingSigner{}
	service, _ := cm.New(config)
	bad, _ := consentworker.New(f.pool, service)
	if ok, err := bad.Deliver(context.Background(), true); !ok || !errors.Is(err, cm.ErrSigning) {
		t.Fatal("signing outage lost work", ok, err)
	}
	status, err := f.service.Status(context.Background(), n.ConsentRequestID)
	if err != nil || status.Status != "APPROVAL_RECORDED" {
		t.Fatal("failed signing activated permission", err)
	}
	countArtefacts(t, f, 0)
	if ok, err := good.Deliver(context.Background(), true); ok || err != nil {
		t.Fatal("worker ignored persisted backoff", err)
	}
	if _, err = f.pool.Exec(context.Background(), `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (available_at-clock_timestamp())))+0.001) FROM outbox_deliveries WHERE subscriber='consent.signing.v1'`); err != nil {
		t.Fatal(err)
	}
	deliver(t, good, true)
	deliver(t, good, false)
	requestState(t, f, "CONSENTED")
	countArtefacts(t, f, 1)
}
func TestWorkerStopsDuringSigningAndDelayedNotifications(t *testing.T) {
	for _, operation := range []string{"withdraw", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			f, _ := workerFixture(t)
			n := create(t, f)
			approve(t, f, n)
			signer := pausedSigner{f.config.Signer, make(chan struct{}), make(chan struct{})}
			config := f.config
			config.Signer = signer
			service, _ := cm.New(config)
			w, _ := consentworker.New(f.pool, service)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := w.Deliver(ctx, true); result <- err }()
			select {
			case <-signer.started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			want := "WITHDRAWN"
			if operation == "withdraw" {
				if _, err := (enforcement.Repository{Pool: f.pool}).Withdraw(ctx, n.ConsentRequestID, 0); err != nil {
					t.Fatal(err)
				}
			} else {
				want = "CANCELLED"
				if _, err := (access.Repository{Pool: f.pool}).Cancel(ctx, f.input.Snapshot.RequestID, f.input.Snapshot.ConsumerID); err != nil {
					t.Fatal(err)
				}
			}
			close(signer.resume)
			if err := <-result; err != nil {
				t.Fatal("terminal stop could not acknowledge obsolete signing work", err)
			}
			deliver(t, w, false)
			requestState(t, f, want)
			countArtefacts(t, f, 0)
		})
	}
	// An ACTIVE notification already in flight must read the newer withdrawal.
	f, w := workerFixture(t)
	n := create(t, f)
	approve(t, f, n)
	deliver(t, w, true)
	if _, err := (enforcement.Repository{Pool: f.pool}).Withdraw(context.Background(), n.ConsentRequestID, 0); err != nil {
		t.Fatal(err)
	}
	deliver(t, w, false)
	requestState(t, f, "WITHDRAWN")
	deliver(t, w, false)
	requestState(t, f, "WITHDRAWN")
	countArtefacts(t, f, 1)
}
func TestWorkerRejectionExpiryAndConsumedCutoff(t *testing.T) {
	t.Run("reject", func(t *testing.T) {
		f, w := workerFixture(t)
		n := create(t, f)
		if _, err := f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "reject")); err != nil {
			t.Fatal(err)
		}
		deliver(t, w, false)
		requestState(t, f, "REJECTED")
		countArtefacts(t, f, 0)
	})
	for _, reserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "expiry", true: "admitted expiry"}[reserved], func(t *testing.T) {
			f, w := workerFixture(t)
			n := create(t, f)
			approve(t, f, n)
			ctx := context.Background()
			if reserved {
				deliver(t, w, true)
				deliver(t, w, false)
				state, err := (access.Repository{Pool: f.pool}).Advance(ctx, f.input.Snapshot.RequestID, 4, "grant_issued")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = (access.Repository{Pool: f.pool}).Advance(ctx, f.input.Snapshot.RequestID, state.Version, "retrieval_started"); err != nil {
					t.Fatal(err)
				}
				if _, err = (enforcement.Repository{Pool: f.pool}).Redeem(ctx, enforcement.VerifiedRedemption{ConsentID: n.ConsentRequestID, RequestID: f.input.Snapshot.RequestID, RequestDigest: n.RequestDigest, GrantJTI: "grant", GrantDigest: cm.Digest([]byte("grant")), AdapterID: "adapter", AttemptID: "attempt", PresenterThumbprint: strings.Repeat("a", 43), GrantExpiresAt: time.Now().Add(time.Minute)}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.pool.Exec(ctx, `UPDATE consent_state SET valid_until=clock_timestamp()-interval '1 second' WHERE consent_id=$1`, n.ConsentRequestID); err != nil {
				t.Fatal(err)
			}
			var sweeps sync.WaitGroup
			failures := make(chan error, 8)
			for range 8 {
				sweeps.Add(1)
				go func() { defer sweeps.Done(); failures <- w.Sweep(ctx) }()
			}
			sweeps.Wait()
			close(failures)
			for err := range failures {
				if err != nil {
					t.Fatal(err)
				}
			}
			var expiredEvents int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id=$1 AND event_type='consent.expired'`, n.ConsentRequestID).Scan(&expiredEvents); err != nil || expiredEvents != 1 {
				t.Fatal("competing sweeps duplicated expiry", expiredEvents, err)
			}
			if reserved {
				requestState(t, f, "REDEEMING")
				var use string
				if err := f.pool.QueryRow(ctx, `SELECT use_state FROM consent_state WHERE consent_id=$1`, n.ConsentRequestID).Scan(&use); err != nil || use != "RESERVED" {
					t.Fatal("expiry restored consumed use", err)
				}
			} else {
				requestState(t, f, "EXPIRED")
				deliver(t, w, true)
				countArtefacts(t, f, 0)
			}
		})
	}
}
func TestWorkerBoundedCleanupAndReconciliationRollback(t *testing.T) {
	f, w := workerFixture(t)
	n := create(t, f)
	approve(t, f, n)
	deliver(t, w, true)
	ctx := context.Background()
	// Notification effect and its own outbox are atomic even when delivery retries.
	if _, err := f.pool.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES('injected','request',$1,4,'test.injected','{}')`, f.input.Snapshot.RequestID); err != nil {
		t.Fatal(err)
	}
	if ok, err := w.Deliver(ctx, false); !ok || err == nil {
		t.Fatal("notification ignored outbox failure")
	}
	requestState(t, f, "CONSENT_PENDING")
	if _, err := f.pool.Exec(ctx, `DELETE FROM outbox WHERE event_id='injected'`); err != nil {
		t.Fatal(err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	requestState(t, f, "CONSENTED")
	// Fixture rows contain no real identity; physical TTL cleanup is still verified.
	if _, err := f.pool.Exec(ctx, `INSERT INTO browser_state(kind,key_digest,binding_digest,payload,expires_at) SELECT 'test', 'sha256:'||lpad(i::text,64,'0'), 'sha256:'||repeat('b',64),'\x00',clock_timestamp()-interval '1 second' FROM generate_series(1,140) i`); err != nil {
		t.Fatal(err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM browser_state`).Scan(&remaining); err != nil || remaining != 12 {
		t.Fatal("TTL cleanup was unbounded or skipped", remaining, err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM browser_state`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("TTL cleanup did not converge", remaining, err)
	}
	countArtefacts(t, f, 1)
}

func TestWorkerProjectsStopsAfterDeadlineAndBoundsSnapshotCleanup(t *testing.T) {
	for _, action := range []string{"withdraw", "reject"} {
		t.Run(action, func(t *testing.T) {
			f, w := workerFixture(t)
			n := create(t, f)
			ctx := context.Background()
			want := "REJECTED"
			if action == "reject" {
				if _, err := f.service.Decide(ctx, n.ConsentRequestID, f.principal, decision(n, "reject")); err != nil {
					t.Fatal(err)
				}
			} else {
				approve(t, f, n)
				want = "WITHDRAWN"
				if _, err := (enforcement.Repository{Pool: f.pool}).Withdraw(ctx, n.ConsentRequestID, 0); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.pool.Exec(ctx, `UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=$1`, f.input.Snapshot.RequestID); err != nil {
				t.Fatal(err)
			}
			deliver(t, w, false)
			requestState(t, f, want)
			countArtefacts(t, f, 0)
		})
	}
	f, w := workerFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO access_requests(request_id,consumer_id,request_digest,status,expires_at) SELECT 'cleanup-'||i,'cleanup','sha256:'||repeat('a',64),'CANCELLED',clock_timestamp()-interval '25 hours' FROM generate_series(1,40) i`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO access_requests(request_id,consumer_id,request_digest,status,expires_at) VALUES('recent','cleanup','sha256:'||repeat('a',64),'CANCELLED',clock_timestamp()-interval '23 hours'),('live','cleanup','sha256:'||repeat('a',64),'AUTHENTICATION_PENDING',clock_timestamp()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO accepted_requests(request_id,consumer_id,idempotency_digest,body_digest,snapshot_bytes) SELECT request_id,'cleanup','sha256:'||lpad(row_number() OVER()::text,64,'0'),'sha256:'||repeat('b',64),convert_to('fictional-private-hint','UTF8') FROM access_requests WHERE consumer_id='cleanup'`); err != nil {
		t.Fatal(err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM accepted_requests`).Scan(&count); err != nil || count != 10 {
		t.Fatal("snapshot cleanup exceeded its batch or retry window", count, err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM accepted_requests WHERE request_id IN ('recent','live')`).Scan(&count); err != nil || count != 2 {
		t.Fatal("cleanup removed live or recent idempotency metadata", count, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM accepted_requests`).Scan(&count); err != nil || count != 2 {
		t.Fatal("snapshot cleanup did not converge", count, err)
	}
}
