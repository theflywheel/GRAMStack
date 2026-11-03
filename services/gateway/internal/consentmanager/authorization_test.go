package consentmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/testdb"
)

type authSubjects struct {
	mu     sync.RWMutex
	values map[string]cm.SubjectContext
	err    error
}

func (s *authSubjects) Resolve(_ context.Context, id string) (cm.SubjectContext, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return cm.SubjectContext{}, s.err
	}
	value, ok := s.values[id]
	if !ok {
		return value, cm.ErrBinding
	}
	return value, nil
}

type authorizationFixture struct {
	fixture
	service   *az.Service
	config    az.Config
	policy    az.Policy
	policyKey *ecdsa.PrivateKey
	subjects  *authSubjects
	notice    cm.Notice
}

func setupAuthorization(t *testing.T) authorizationFixture {
	t.Helper()
	f := setupPool(t, testdb.New(t))
	subject := f.config.Subjects.(subjects).value
	source := &authSubjects{values: map[string]cm.SubjectContext{subject.ContextID: subject}}
	f.config.Subjects = source
	f.service, _ = cm.New(f.config)
	n := prepareAuthorization(t, f)
	grantKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policyKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := az.NewGrantSigner("grant-key", grantKey)
	if err != nil {
		t.Fatal(err)
	}
	c := az.Config{Pool: f.pool, Subjects: source, Terms: f.config.Terms, Consents: f.config.Verifier, Signer: signer, GrantKeys: map[string]*ecdsa.PublicKey{"grant-key": &grantKey.PublicKey}, PolicyKeys: map[string]map[string]*ecdsa.PublicKey{"lokos": {"source-key": &policyKey.PublicKey}}, Adapters: map[string]az.Adapter{"lokos": {Audience: "lokos-adapter", PeerName: "lokos-adapter.gramstack.internal"}}, Presenters: map[string]string{"sakhicredit": strings.Repeat("a", 43), "other": strings.Repeat("b", 43)}, Issuer: "https://gateway.example"}
	service, err := az.New(c)
	if err != nil {
		t.Fatal(err)
	}
	p := az.Policy{Registry: "lokos", Version: "v1", Consumer: "sakhicredit", Resource: "shg.membership", Purpose: "credit_eligibility", Fields: []string{"membership_status", "savings_summary"}, RequireSubject: true, RequireConsent: true, MaxSeconds: 60, MaxDisclosures: 1}
	signed, err := az.SignPolicy(context.Background(), "source-key", policyKey, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.InstallPolicy(context.Background(), signed, ""); err != nil {
		t.Fatal(err)
	}
	return authorizationFixture{f, service, c, p, policyKey, source, n}
}
func prepareAuthorization(t *testing.T, f fixture) cm.Notice {
	t.Helper()
	raw, err := cm.Canonical(f.input.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := cm.Canonical(f.input.Snapshot.Request)
	if _, err = f.pool.Exec(context.Background(), `INSERT INTO accepted_requests(request_id,consumer_id,idempotency_digest,body_digest,snapshot_bytes) VALUES($1,$2,$3,$4,$5)`, f.input.Snapshot.RequestID, f.input.Snapshot.ConsumerID, cm.Digest([]byte(rand.Text())), cm.Digest(body), raw); err != nil {
		t.Fatal(err)
	}
	n := create(t, f)
	approve(t, f, n)
	if _, err = f.service.Activate(context.Background(), n.ConsentRequestID); err != nil {
		t.Fatal(err)
	}
	if err = (access.Repository{Pool: f.pool}).Reconcile(context.Background(), f.input.Snapshot.RequestID); err != nil {
		t.Fatal(err)
	}
	return n
}
func (f authorizationFixture) issue(t *testing.T, key string) az.GrantResponse {
	t.Helper()
	result, err := f.service.Issue(context.Background(), "sakhicredit", f.input.Snapshot.RequestID, key)
	if err != nil {
		t.Fatal("grant issuance failed", err)
	}
	return result
}
func (f authorizationFixture) redemptionInput(grant, attempt string) az.Redemption {
	return az.Redemption{AttemptID: attempt, Grant: grant, RequestID: f.input.Snapshot.RequestID, Digest: f.input.RequestDigest, Presenter: az.Presenter{Consumer: "sakhicredit", Thumbprint: strings.Repeat("a", 43)}}
}
func (f authorizationFixture) install(t *testing.T, p az.Policy, expected string) {
	t.Helper()
	signed, err := az.SignPolicy(context.Background(), "source-key", f.policyKey, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.InstallPolicy(context.Background(), signed, expected); err != nil {
		t.Fatal(err)
	}
}
func (f authorizationFixture) count(t *testing.T, table string, want int) {
	t.Helper()
	var count int
	switch table {
	case "grants":
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM retrieval_grants`).Scan(&count); err != nil {
			t.Fatal(err)
		}
	case "reservations":
		if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM disclosure_reservations`).Scan(&count); err != nil {
			t.Fatal(err)
		}
	}
	if count != want {
		t.Fatal("unexpected "+table, count, want)
	}
}
func TestGrantConcurrencyOwnershipAndSignerIndependentRetries(t *testing.T) {
	f := setupAuthorization(t)
	ctx := context.Background()
	if _, err := f.service.Issue(ctx, "other", f.input.Snapshot.RequestID, "key"); !errors.Is(err, access.ErrNotFound) {
		t.Fatal("grant ownership bypassed", err)
	}
	results := make(chan az.GrantResponse, 12)
	failures := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := f.service.Issue(ctx, "sakhicredit", f.input.Snapshot.RequestID, rand.Text())
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
	var original string
	for result := range results {
		if original != "" && original != result.Grant {
			t.Fatal("concurrent issuance created different grants")
		}
		original = result.Grant
	}
	f.count(t, "grants", 1)
	requestState(t, f.fixture, "AUTHORIZED")
	grant, err := f.service.VerifyGrant(original, time.Now().Unix())
	if err != nil || grant.Digest != f.input.RequestDigest || grant.ConsentID != f.notice.ConsentRequestID || grant.ExpiresAt-grant.IssuedAt > 60 || grant.Confirmation.Thumbprint != strings.Repeat("a", 43) {
		t.Fatal("issued grant scope or bounds changed", err)
	}
	config := f.config
	config.Signer = failingSigner{}
	restarted, err := az.New(config)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := restarted.Issue(ctx, "sakhicredit", f.input.Snapshot.RequestID, "retry-key")
	if err != nil || retry.Grant != original {
		t.Fatal("idempotent grant depended on a new signature", err)
	}
	if _, err = f.service.VerifyGrant(original, grant.ExpiresAt); !errors.Is(err, az.ErrExpired) {
		t.Fatal("expiry accepted", err)
	}
	if _, err = f.service.VerifyGrant(original, grant.IssuedAt-1); !errors.Is(err, az.ErrExpired) {
		t.Fatal("future grant accepted", err)
	}
}
func TestCurrentPolicyRevocationReplacementAndRollbackDenial(t *testing.T) {
	f := setupAuthorization(t)
	g := f.issue(t, "key")
	ctx := context.Background()
	policy := f.policy
	policy.Version = "v2"
	policy.Fields = []string{"membership_status"}
	f.install(t, policy, "v1")
	if _, err := f.service.Redeem(ctx, "lokos-adapter", f.redemptionInput(g.Grant, "attempt")); !errors.Is(err, az.ErrDenied) {
		t.Fatal("stale source policy admitted a grant", err)
	}
	if _, err := f.service.Issue(ctx, "sakhicredit", f.input.Snapshot.RequestID, "key"); !errors.Is(err, az.ErrDenied) {
		t.Fatal("policy-narrowed request got a grant", err)
	}
	signed, _ := az.SignPolicy(ctx, "source-key", f.policyKey, f.policy)
	if err := f.service.InstallPolicy(ctx, signed, "v2"); !errors.Is(err, az.ErrConflict) {
		t.Fatal("old policy version resurrected", err)
	}
	changed := policy
	changed.Fields = []string{"membership_status", "savings_summary"}
	signed, _ = az.SignPolicy(ctx, "source-key", f.policyKey, changed)
	if err := f.service.InstallPolicy(ctx, signed, "v2"); !errors.Is(err, az.ErrConflict) {
		t.Fatal("same policy version changed meaning", err)
	}
	f.count(t, "reservations", 0)
	f2 := setupAuthorization(t)
	g2 := f2.issue(t, "key")
	if err := f2.service.DisablePolicy(ctx, az.PolicyKey{Registry: "lokos", Consumer: "sakhicredit", Resource: "shg.membership", Purpose: "credit_eligibility"}, "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f2.service.Redeem(ctx, "lokos-adapter", f2.redemptionInput(g2.Grant, "attempt")); !errors.Is(err, az.ErrDenied) {
		t.Fatal("disabled policy admitted retrieval", err)
	}
	signed, _ = az.SignPolicy(ctx, "source-key", f2.policyKey, f2.policy)
	if err := f2.service.InstallPolicy(ctx, signed, ""); err != nil {
		t.Fatal("idempotent startup could not preserve policy revocation", err)
	}
	if _, err := f2.service.Redeem(ctx, "lokos-adapter", f2.redemptionInput(g2.Grant, "after-reload")); !errors.Is(err, az.ErrDenied) {
		t.Fatal("startup re-enabled revoked policy", err)
	}
}
func TestRedemptionBindingsRollbackAndMetadataRetry(t *testing.T) {
	f := setupAuthorization(t)
	g := f.issue(t, "key")
	ctx := context.Background()
	input := f.redemptionInput(g.Grant, "attempt")
	for _, tc := range []struct {
		name     string
		edit     func(*az.Redemption)
		audience string
	}{
		{"wrong audience", func(*az.Redemption) {}, "other-adapter"},
		{"wrong presenter", func(i *az.Redemption) { i.Presenter.Thumbprint = strings.Repeat("b", 43) }, "lokos-adapter"},
		{"wrong consumer", func(i *az.Redemption) { i.Presenter.Consumer = "other" }, "lokos-adapter"},
		{"wrong request", func(i *az.Redemption) { i.RequestID = "other" }, "lokos-adapter"},
		{"wrong digest", func(i *az.Redemption) { i.Digest = cm.Digest([]byte("other")) }, "lokos-adapter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := input
			tc.edit(&v)
			if _, err := f.service.Redeem(ctx, tc.audience, v); err == nil {
				t.Fatal("substituted redemption admitted")
			}
		})
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES('fault','request',$1,6,'test.injected','{}')`, f.input.Snapshot.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Redeem(ctx, "lokos-adapter", input); err == nil {
		t.Fatal("reservation ignored request outbox failure")
	}
	f.count(t, "reservations", 0)
	requestState(t, f.fixture, "AUTHORIZED")
	if _, err := f.pool.Exec(ctx, `DELETE FROM outbox WHERE event_id='fault'`); err != nil {
		t.Fatal(err)
	}
	first, err := f.service.Redeem(ctx, "lokos-adapter", input)
	if err != nil || !first.First {
		t.Fatal("valid redemption failed", err)
	}
	requestState(t, f.fixture, "REDEEMING")
	f.count(t, "reservations", 1)
	if _, err = (enforcement.Repository{Pool: f.pool}).Withdraw(ctx, f.notice.ConsentRequestID, 0); err != nil {
		t.Fatal(err)
	}
	if err = f.service.DisablePolicy(ctx, az.PolicyKey{Registry: "lokos", Consumer: "sakhicredit", Resource: "shg.membership", Purpose: "credit_eligibility"}, "v1"); err != nil {
		t.Fatal(err)
	}
	f.subjects.mu.Lock()
	f.subjects.err = cm.ErrBinding
	f.subjects.mu.Unlock()
	retry, err := f.service.Redeem(ctx, "lokos-adapter", input)
	if err != nil || retry.First || retry.ID != first.ID {
		t.Fatal("historical acknowledgement became a new admission or failed", err)
	}
	input.AttemptID = "second"
	if _, err = f.service.Redeem(ctx, "lokos-adapter", input); !errors.Is(err, enforcement.ErrConsumed) {
		t.Fatal("new attempt reused consumed grant", err)
	}
	f.count(t, "reservations", 1)
}
func TestGrantStopsAndPolicyChangeDuringSigning(t *testing.T) {
	for _, operation := range []string{"withdraw", "cancel", "policy"} {
		t.Run(operation, func(t *testing.T) {
			f := setupAuthorization(t)
			config := f.config
			paused := pausedSigner{config.Signer, make(chan struct{}), make(chan struct{})}
			config.Signer = paused
			service, err := az.New(config)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := service.Issue(ctx, "sakhicredit", f.input.Snapshot.RequestID, "key"); result <- err }()
			select {
			case <-paused.started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch operation {
			case "withdraw":
				_, err = (enforcement.Repository{Pool: f.pool}).Withdraw(ctx, f.notice.ConsentRequestID, 0)
			case "cancel":
				_, err = (access.Repository{Pool: f.pool}).Cancel(ctx, f.input.Snapshot.RequestID, "sakhicredit")
			case "policy":
				p := f.policy
				p.Version = "v2"
				signed, _ := az.SignPolicy(ctx, "source-key", f.policyKey, p)
				err = f.service.InstallPolicy(ctx, signed, "v1")
			}
			close(paused.resume)
			if err != nil {
				t.Fatal(err)
			}
			if err = <-result; err == nil {
				t.Fatal("late signature revived authority")
			}
			f.count(t, "grants", 0)
		})
	}
}
func TestGrantAndAdmissionRollbackAndSubjectSubstitution(t *testing.T) {
	f := setupAuthorization(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES('fault','request',$1,5,'test.injected','{}')`, f.input.Snapshot.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Issue(ctx, "sakhicredit", f.input.Snapshot.RequestID, "key"); err == nil {
		t.Fatal("grant ignored outbox failure")
	}
	f.count(t, "grants", 0)
	requestState(t, f.fixture, "CONSENTED")
	if _, err := f.pool.Exec(ctx, `DELETE FROM outbox WHERE event_id='fault'`); err != nil {
		t.Fatal(err)
	}
	g := f.issue(t, "key")
	f.subjects.mu.Lock()
	value := f.subjects.values[f.input.SubjectContextRef]
	value.SourceMappingRef = "substituted-source"
	f.subjects.values[value.ContextID] = value
	f.subjects.mu.Unlock()
	if _, err := f.service.Redeem(ctx, "lokos-adapter", f.redemptionInput(g.Grant, "attempt")); !errors.Is(err, az.ErrBinding) {
		t.Fatal("changed subject context admitted", err)
	}
	f.count(t, "reservations", 0)
}

func TestAuthorizationConcurrentAdmissionAndWithdrawal(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "admissions", true: "withdrawal race"}[stop], func(t *testing.T) {
			f := setupAuthorization(t)
			g := f.issue(t, "key")
			ctx := context.Background()
			start := make(chan struct{})
			var wg sync.WaitGroup
			var admitted atomic.Int32
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					v, err := f.service.Redeem(ctx, "lokos-adapter", f.redemptionInput(g.Grant, rand.Text()))
					if err == nil {
						if !v.First {
							t.Error("new attempt returned historical acknowledgement")
						}
						admitted.Add(1)
					} else if !errors.Is(err, enforcement.ErrConsumed) && !errors.Is(err, az.ErrNotReady) && !errors.Is(err, az.ErrDenied) {
						t.Error(err)
					}
				}()
			}
			var stopped enforcement.Consent
			if stop {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					var err error
					stopped, err = (enforcement.Repository{Pool: f.pool}).Withdraw(ctx, f.notice.ConsentRequestID, 0)
					if err != nil {
						t.Error(err)
					}
				}()
			}
			close(start)
			wg.Wait()
			n := admitted.Load()
			if n > 1 || (!stop && n != 1) {
				t.Fatal("admission count", n)
			}
			f.count(t, "reservations", int(n))
			if stop {
				if (stopped.Use == "RESERVED") != (n == 1) {
					t.Fatal("withdrawal cutoff disagreed with atomic admission", stopped, n)
				}
			}
		})
	}
}

type substitutedGrantSigner struct{ az.Signer }

func (s substitutedGrantSigner) Sign(ctx context.Context, payload []byte) (string, error) {
	var grant az.Grant
	if err := json.Unmarshal(payload, &grant); err != nil {
		return "", err
	}
	grant.Fields = []string{"unapproved_private_field"}
	raw, err := cm.Canonical(grant)
	if err != nil {
		return "", err
	}
	return s.Signer.Sign(ctx, raw)
}
func TestGrantRejectsSignerScopeSubstitutionAndUntrustedPolicy(t *testing.T) {
	f := setupAuthorization(t)
	ctx := context.Background()
	config := f.config
	config.Signer = substitutedGrantSigner{config.Signer}
	service, err := az.New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Issue(ctx, "sakhicredit", f.input.Snapshot.RequestID, "key"); !errors.Is(err, az.ErrSigning) {
		t.Fatal("valid signature changed the requested scope", err)
	}
	f.count(t, "grants", 0)
	foreign, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := f.policy
	policy.Version = "v2"
	signed, err := az.SignPolicy(ctx, "source-key", foreign, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.InstallPolicy(ctx, signed, "v1"); !errors.Is(err, az.ErrBinding) {
		t.Fatal("unapproved source key replaced trusted policy", err)
	}
	f.issue(t, "valid-after-denial")
}
func TestExpiredGrantCannotAdmitButKeepsHistoricalAcknowledgement(t *testing.T) {
	for _, admit := range []bool{false, true} {
		t.Run(map[bool]string{false: "new admission", true: "historical acknowledgement"}[admit], func(t *testing.T) {
			f := setupAuthorization(t)
			p := f.policy
			p.Version = "short"
			p.MaxSeconds = 2
			f.install(t, p, "v1")
			g := f.issue(t, "short")
			input := f.redemptionInput(g.Grant, "same-attempt")
			ctx := context.Background()
			var first az.Reservation
			if admit {
				var err error
				first, err = f.service.Redeem(ctx, "lokos-adapter", input)
				if err != nil || !first.First {
					t.Fatal("initial admission failed", err)
				}
			}
			time.Sleep(max(0, time.Until(g.ExpiresAt)) + 10*time.Millisecond)
			retry, err := f.service.Redeem(ctx, "lokos-adapter", input)
			if admit {
				if err != nil || retry.First || retry.ID != first.ID {
					t.Fatal("expired retry created authority or lost metadata", err)
				}
			} else if !errors.Is(err, az.ErrExpired) {
				t.Fatal("expired grant admitted", err)
			}
		})
	}
}
