package consentmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
	"github.com/theflywheel/GRAMStack/services/gateway/migrations"
)

type subjects struct{ value cm.SubjectContext }

func (s subjects) Resolve(context.Context, string) (cm.SubjectContext, error) { return s.value, nil }

type terms struct{ text string }

func (t terms) Resolve(context.Context, string, string) (cm.Terms, error) {
	return cm.Terms{Version: "terms-v1", Text: t.text}, nil
}

type failingSigner struct{}

func (failingSigner) Sign(context.Context, []byte) (string, error) {
	return "", errors.New("simulated signing outage")
}

type changedSigner struct{ signer cm.Signer }

func (s changedSigner) Sign(ctx context.Context, raw []byte) (string, error) {
	var value map[string]any
	json.Unmarshal(raw, &value)
	value["consumer"] = "attacker"
	changed, _ := cm.Canonical(value)
	return s.signer.Sign(ctx, changed)
}

type fixture struct {
	pool      *pgxpool.Pool
	config    cm.Config
	service   *cm.Service
	input     cm.CreateRequest
	principal string
}

func setup(t *testing.T) fixture {
	t.Helper()
	dsn := os.Getenv("GRAMSTACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = migrations.Apply(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	snapshot := cm.Snapshot{RequestID: rand.Text(), ConsumerID: "sakhicredit", CreatedAt: now.Add(-time.Second), ExpiresAt: now.Add(10 * time.Minute), RecipientTermsDigest: cm.Digest([]byte("Keep approved data for the declared credit eligibility assessment only."))}
	snapshot.Request.Registry = "lokos"
	snapshot.Request.Resource = "shg.membership"
	snapshot.Request.SubjectHint.Type = "member_reference"
	snapshot.Request.SubjectHint.Value = "private-source-locator-not-for-consent-storage"
	snapshot.Request.Fields = []string{"membership_status", "savings_summary"}
	snapshot.Request.Purpose = "credit_eligibility"
	snapshot.Request.Access.Mode = "single_use"
	snapshot.Request.Access.ValidForSeconds = 300
	snapshot.Request.RecipientTerms.RetentionPolicyID = "terms-v1"
	digest, err := cm.SnapshotDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	requests := access.Repository{Pool: pool}
	if err = requests.Create(context.Background(), snapshot.RequestID, snapshot.ConsumerID, digest, snapshot.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	for i, event := range []string{"authenticated", "subject_verified"} {
		if _, err = requests.Advance(context.Background(), snapshot.RequestID, int64(i+1), event); err != nil {
			t.Fatal(err)
		}
	}
	subject := cm.SubjectContext{ContextID: rand.Text(), Registry: "lokos", RequestDigest: digest, PrincipalContextRef: rand.Text(), SourceMappingRef: rand.Text(), VerificationMethod: "test-registry-approved", VerifiedAt: now.Add(-time.Second), ExpiresAt: now.Add(8 * time.Minute)}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := cm.NewES256Signer("consent-key", key)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := cm.NewVerifier("https://consent.example", "https://consent.example", map[string]*ecdsa.PublicKey{"consent-key": &key.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	config := cm.Config{Pool: pool, Subjects: subjects{subject}, Terms: terms{"Keep approved data for the declared credit eligibility assessment only."}, Signer: signer, Verifier: verifier, InteractionOrigin: "https://gateway.example"}
	service, err := cm.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{pool, config, service, cm.CreateRequest{Snapshot: snapshot, RequestDigest: digest, SubjectContextRef: subject.ContextID, Language: "en"}, subject.PrincipalContextRef}
}
func decision(notice cm.Notice, action string) cm.Decision {
	return cm.Decision{Action: action, RequestDigest: notice.RequestDigest, NoticeDigest: notice.NoticeDigest, NoticeVersion: notice.NoticeVersion, Language: notice.Language}
}
func create(t *testing.T, f fixture) cm.Notice {
	t.Helper()
	n, err := f.service.Create(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func approve(t *testing.T, f fixture, n cm.Notice) {
	t.Helper()
	s, err := f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "approve"))
	if err != nil || s.Status != "APPROVAL_RECORDED" {
		t.Fatalf("%+v %v", s, err)
	}
}
func TestConsentNoticeDecisionAndActivation(t *testing.T) {
	f := setup(t)
	n := create(t, f)
	again, err := f.service.Create(context.Background(), f.input)
	if err != nil || again.ConsentRequestID != n.ConsentRequestID || again.Text != n.Text {
		t.Fatalf("create retry: %+v %v", again, err)
	}
	if n.NoticeDigest != cm.Digest([]byte(n.Text)) || !strings.Contains(n.Text, "savings_summary") || !strings.Contains(n.Text, "credit_eligibility") {
		t.Fatal("notice does not bind exact scope")
	}
	if _, err = f.service.GetNotice(context.Background(), n.ConsentRequestID, "other-principal"); !errors.Is(err, cm.ErrForbidden) {
		t.Fatal("wrong principal viewed notice")
	}
	if _, err = f.service.Decide(context.Background(), n.ConsentRequestID, "other-principal", decision(n, "approve")); !errors.Is(err, cm.ErrForbidden) {
		t.Fatal("wrong principal approved")
	}
	stale := decision(n, "approve")
	stale.NoticeDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err = f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, stale); !errors.Is(err, cm.ErrBinding) {
		t.Fatalf("stale notice: %v", err)
	}
	approve(t, f, n)
	if _, err = f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "reject")); !errors.Is(err, cm.ErrConflict) {
		t.Fatalf("conflicting retry: %v", err)
	}
	value, err := f.service.Activate(context.Background(), n.ConsentRequestID)
	if err != nil || value.Status.Status != "ACTIVE" {
		t.Fatalf("%+v %v", value, err)
	}
	artefact, _, err := f.config.Verifier.Verify(value.JWS, time.Now())
	if err != nil || artefact.RequestDigest != f.input.RequestDigest || artefact.Consumer != "sakhicredit" || artefact.NoticeDigest != n.NoticeDigest || artefact.ValidUntil.Sub(artefact.ApprovedAt) > 300*time.Second {
		t.Fatalf("%+v %v", artefact, err)
	}
	retry, err := f.service.Activate(context.Background(), n.ConsentRequestID)
	if err != nil || retry.JWS != value.JWS {
		t.Fatal("activation retry changed signed bytes")
	}
	status, err := f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "approve"))
	if err != nil || status.StatusVersion != 3 {
		t.Fatalf("decision retry: %+v %v", status, err)
	}
	admission, err := (enforcement.Repository{Pool: f.pool}).Redeem(context.Background(), enforcement.VerifiedRedemption{
		ConsentID: n.ConsentRequestID, RequestID: f.input.Snapshot.RequestID, RequestDigest: f.input.RequestDigest,
		GrantJTI: rand.Text(), GrantDigest: cm.Digest([]byte("synthetic-already-verified-grant")), AdapterID: "lokos-adapter", AttemptID: rand.Text(), PresenterThumbprint: strings.Repeat("a", 43), GrantExpiresAt: artefact.ValidUntil,
	})
	if err != nil || !admission.FirstRedemption {
		t.Fatalf("real consent did not admit one use: %+v %v", admission, err)
	}
	exhausted, err := f.service.Status(context.Background(), n.ConsentRequestID)
	if err != nil || exhausted.Status != "EXHAUSTED" {
		t.Fatalf("%+v %v", exhausted, err)
	}
	if _, err = (enforcement.Repository{Pool: f.pool}).Withdraw(context.Background(), n.ConsentRequestID, 4); err != nil {
		t.Fatal(err)
	}
	retry, err = f.service.Activate(context.Background(), n.ConsentRequestID)
	if err != nil || retry.Status.Status != "WITHDRAWN" || retry.JWS != value.JWS {
		t.Fatalf("withdrawn activation retry: %+v %v", retry, err)
	}
	var count int
	if err = f.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox WHERE aggregate_type='consent' AND aggregate_id=$1", n.ConsentRequestID).Scan(&count); err != nil || count != 4 {
		t.Fatalf("outbox=%d %v", count, err)
	}
	var scopeBytes []byte
	if err = f.pool.QueryRow(context.Background(), "SELECT scope_bytes FROM consent_requests WHERE consent_id=$1", n.ConsentRequestID).Scan(&scopeBytes); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scopeBytes), f.input.Snapshot.Request.SubjectHint.Value) {
		t.Fatal("raw subject hint retained in consent scope")
	}
	if _, err = f.pool.Exec(context.Background(), "UPDATE consent_notices SET notice_text='changed' WHERE consent_id=$1", n.ConsentRequestID); err == nil {
		t.Fatal("notice mutation accepted")
	}
	if _, err = f.pool.Exec(context.Background(), "UPDATE consent_artefacts SET jws='changed' WHERE consent_id=$1", n.ConsentRequestID); err == nil {
		t.Fatal("artefact mutation accepted")
	}
}
func TestConsentSignerFailureAndConcurrency(t *testing.T) {
	f := setup(t)
	n := create(t, f)
	approve(t, f, n)
	broken := f.config
	broken.Signer = failingSigner{}
	service, _ := cm.New(broken)
	if _, err := service.Activate(context.Background(), n.ConsentRequestID); !errors.Is(err, cm.ErrSigning) {
		t.Fatalf("signer error: %v", err)
	}
	bad := f.config
	bad.Signer = changedSigner{f.config.Signer}
	service, _ = cm.New(bad)
	if _, err := service.Activate(context.Background(), n.ConsentRequestID); !errors.Is(err, cm.ErrSigning) {
		t.Fatalf("scope substitution accepted: %v", err)
	}
	status, err := f.service.Status(context.Background(), n.ConsentRequestID)
	if err != nil || status.Status != "APPROVAL_RECORDED" || status.StatusVersion != 2 {
		t.Fatalf("failure activated consent: %+v %v", status, err)
	}
	const workers = 8
	results := make(chan cm.Activation, workers)
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := f.service.Activate(context.Background(), n.ConsentRequestID)
			if err != nil {
				failures <- err
			} else {
				results <- value
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	signed := ""
	for result := range results {
		if signed != "" && signed != result.JWS {
			t.Error("concurrent activations returned different stored artefacts")
		}
		signed = result.JWS
	}
	var count int
	f.pool.QueryRow(context.Background(), "SELECT count(*) FROM consent_artefacts WHERE consent_id=$1", n.ConsentRequestID).Scan(&count)
	if count != 1 {
		t.Fatal("multiple artefacts stored")
	}
}
func TestConsentRejectAndBindingFailures(t *testing.T) {
	f := setup(t)
	bad := f.input
	bad.RequestDigest = "sha256:" + strings.Repeat("a", 64)
	if _, err := f.service.Create(context.Background(), bad); !errors.Is(err, cm.ErrBinding) {
		t.Fatal("wrong request digest accepted")
	}
	config := f.config
	config.Terms = terms{"changed recipient terms"}
	service, _ := cm.New(config)
	if _, err := service.Create(context.Background(), f.input); !errors.Is(err, cm.ErrBinding) {
		t.Fatal("changed terms accepted")
	}
	resolved := f.config.Subjects.(subjects).value
	resolved.Registry = "other-registry"
	config = f.config
	config.Subjects = subjects{resolved}
	service, _ = cm.New(config)
	if _, err := service.Create(context.Background(), f.input); !errors.Is(err, cm.ErrBinding) {
		t.Fatal("wrong subject binding accepted")
	}
	n := create(t, f)
	status, err := f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "reject"))
	if err != nil || status.Status != "REJECTED" {
		t.Fatalf("%+v %v", status, err)
	}
	if _, err := f.service.Activate(context.Background(), n.ConsentRequestID); !errors.Is(err, cm.ErrNotReady) {
		t.Fatal("rejected consent activated")
	}
}

func TestConsentDecisionRacesAndOutboxRollback(t *testing.T) {
	f := setup(t)
	n := create(t, f)
	const workers = 8
	errorsFound := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "approve"))
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	var decisions int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM consent_decisions WHERE consent_id=$1", n.ConsentRequestID).Scan(&decisions); err != nil || decisions != 1 {
		t.Fatalf("decisions=%d %v", decisions, err)
	}
	injected := rand.Text()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES($1,'consent',$2,3,'test.injected','{}')`, injected, n.ConsentRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(context.Background(), n.ConsentRequestID); err == nil {
		t.Fatal("activation ignored outbox failure")
	}
	status, err := f.service.Status(context.Background(), n.ConsentRequestID)
	if err != nil || status.Status != "APPROVAL_RECORDED" || status.StatusVersion != 2 {
		t.Fatalf("partial activation: %+v %v", status, err)
	}
	var artefacts int
	if err = f.pool.QueryRow(context.Background(), "SELECT count(*) FROM consent_artefacts WHERE consent_id=$1", n.ConsentRequestID).Scan(&artefacts); err != nil || artefacts != 0 {
		t.Fatalf("artefacts=%d %v", artefacts, err)
	}
	if _, err = f.pool.Exec(context.Background(), "DELETE FROM outbox WHERE event_id=$1", injected); err != nil {
		t.Fatal(err)
	}
	activated, err := f.service.Activate(context.Background(), n.ConsentRequestID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.service.GetArtefact(context.Background(), n.ConsentRequestID)
	if err != nil || saved != activated.JWS {
		t.Fatal("read-only artefact retrieval changed bytes")
	}
}

func TestConsentDeadlines(t *testing.T) {
	f := setup(t)
	expired := f.config.Subjects.(subjects).value
	expired.VerifiedAt = time.Now().Add(-2 * time.Hour)
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	config := f.config
	config.Subjects = subjects{expired}
	service, _ := cm.New(config)
	if _, err := service.Create(context.Background(), f.input); !errors.Is(err, cm.ErrExpired) {
		t.Fatalf("expired subject context: %v", err)
	}
	n := create(t, f)
	if _, err := f.service.Expire(context.Background(), n.ConsentRequestID); !errors.Is(err, cm.ErrNotDue) {
		t.Fatal("live consent expired early")
	}
	if _, err := f.pool.Exec(context.Background(), "UPDATE consent_state SET valid_until=clock_timestamp()-interval '1 second' WHERE consent_id=$1", n.ConsentRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Decide(context.Background(), n.ConsentRequestID, f.principal, decision(n, "approve")); !errors.Is(err, cm.ErrExpired) {
		t.Fatalf("expired decision: %v", err)
	}
	status, err := f.service.Expire(context.Background(), n.ConsentRequestID)
	if err != nil || status.Status != "EXPIRED" {
		t.Fatalf("%+v %v", status, err)
	}
	duplicate, err := f.service.Expire(context.Background(), n.ConsentRequestID)
	if err != nil || duplicate.StatusVersion != status.StatusVersion {
		t.Fatal("expiry was not idempotent")
	}
	if _, err := f.service.Activate(context.Background(), n.ConsentRequestID); !errors.Is(err, cm.ErrNotReady) {
		t.Fatal("expired consent activated")
	}
}

func TestApprovedConsentCannotActivateAfterExpiry(t *testing.T) {
	f := setup(t)
	n := create(t, f)
	approve(t, f, n)
	if _, err := f.pool.Exec(context.Background(), "UPDATE consent_state SET valid_until=clock_timestamp()-interval '1 second' WHERE consent_id=$1", n.ConsentRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Activate(context.Background(), n.ConsentRequestID); !errors.Is(err, cm.ErrExpired) {
		t.Fatalf("expired approval activated: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM consent_artefacts WHERE consent_id=$1", n.ConsentRequestID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired artefacts=%d %v", count, err)
	}
	status, err := f.service.Expire(context.Background(), n.ConsentRequestID)
	if err != nil || status.Status != "EXPIRED" || status.StatusVersion != 3 {
		t.Fatalf("%+v %v", status, err)
	}
}

type unavailableSubjects struct{}

func (unavailableSubjects) Resolve(context.Context, string) (cm.SubjectContext, error) {
	return cm.SubjectContext{}, errors.New("temporary resolver outage")
}
func TestConsentResolverOutageIsNotDenial(t *testing.T) {
	f := setup(t)
	config := f.config
	config.Subjects = unavailableSubjects{}
	service, _ := cm.New(config)
	if _, err := service.Create(context.Background(), f.input); !errors.Is(err, cm.ErrUnavailable) {
		t.Fatalf("outage misclassified: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM consent_state WHERE request_id=$1", f.input.Snapshot.RequestID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("outage persisted consent: %d %v", count, err)
	}
}
