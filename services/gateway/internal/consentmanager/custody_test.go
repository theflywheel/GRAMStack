package consentmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/custody"
)

type custodyFixture struct {
	authorizationFixture
	custodian     *custody.Service
	custodyConfig custody.Config
	key           *ecdsa.PrivateKey
	event         custody.Event
	expiresAt     time.Time
}

func setupCustody(t *testing.T) custodyFixture { return setupCustodyLifetime(t, 60) }
func setupCustodyLifetime(t *testing.T, seconds int64) custodyFixture {
	t.Helper()
	f := setupAuthorization(t)
	if seconds != 60 {
		p := f.policy
		p.Version = "short"
		p.MaxSeconds = seconds
		f.install(t, p, "v1")
	}
	grant := f.issue(t, "key")
	r, err := f.service.Redeem(context.Background(), "lokos-adapter", f.redemptionInput(grant.Grant, "attempt-1"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := custody.Config{Pool: f.pool, Scopes: f.service, ReleaseProfile: "batch", Adapters: []custody.Adapter{{ID: "lokos-adapter", Audience: "lokos-adapter", PeerName: "lokos-adapter.gramstack.internal", Keys: map[string]*ecdsa.PublicKey{"adapter-key": &key.PublicKey}}}}
	service, err := custody.New(config)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := f.service.VerifyGrant(grant.Grant, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	e := custody.Event{EventID: "event-" + rand.Text(), RequestID: claims.RequestID, Digest: claims.Digest, ConsentID: claims.ConsentID, GrantJTI: claims.JTI, AdapterID: "lokos-adapter", AttemptID: r.AttemptID, RedemptionID: r.ID, Fields: slices.Clone(claims.Fields), Outcome: "release_ready", Commitment: cm.Digest([]byte("salted canonical resource commitment")), OccurredAt: time.Now().UTC()}
	return custodyFixture{f, service, config, key, e, grant.ExpiresAt}
}
func signEvidence(t *testing.T, key *ecdsa.PrivateKey, value any, typ string, extra map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", "adapter-key")
	for k, v := range extra {
		options.WithHeader(jose.HeaderKey(k), v)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, options)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cm.Canonical(value)
	if err != nil {
		t.Fatal(err)
	}
	object, err := signer.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := object.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return compact
}
func (f custodyFixture) counts(t *testing.T, want int) {
	t.Helper()
	for _, query := range []string{`SELECT count(*) FROM evidence_receipts`, `SELECT count(*) FROM evidence_anchor_state`, `SELECT count(*) FROM outbox WHERE aggregate_type='evidence'`} {
		var n int
		if err := f.pool.QueryRow(context.Background(), query).Scan(&n); err != nil || n != want {
			t.Fatal("evidence atomicity failed", n, want, err)
		}
	}
}
func TestCustodyConcurrentImmutableReceiptAndHistoricalRetry(t *testing.T) {
	f := setupCustody(t)
	ctx := context.Background()
	signed := signEvidence(t, f.key, f.event, custody.EvidenceType, nil)
	var group sync.WaitGroup
	acks := make(chan custody.Acknowledgement, 20)
	failures := make(chan error, 20)
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			ack, err := f.custodian.Record(ctx, "lokos-adapter", signed)
			if err != nil {
				failures <- err
			} else {
				acks <- ack
			}
		}()
	}
	group.Wait()
	close(acks)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var first custody.Acknowledgement
	for ack := range acks {
		if first.ReceiptID != "" && (ack.ReceiptID != first.ReceiptID || ack.Digest != first.Digest || !ack.AcceptedAt.Equal(first.AcceptedAt) || ack.AnchorStatus != first.AnchorStatus) {
			t.Fatal("duplicate evidence acquired another receipt")
		}
		first = ack
	}
	if first.ReceiptID == "" || first.Digest != cm.Digest([]byte(signed)) || first.AnchorStatus != "pending" {
		t.Fatal("invalid acknowledgement")
	}
	f.counts(t, 1)
	requestState(t, f.fixture, "EVIDENCE_COMMITTED")
	var stored string
	if err := f.pool.QueryRow(ctx, `SELECT jws FROM evidence_receipts WHERE receipt_id=$1`, first.ReceiptID).Scan(&stored); err != nil || stored != signed {
		t.Fatal("custody changed signed bytes", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE evidence_receipts SET jws='changed' WHERE receipt_id=$1`, first.ReceiptID); err == nil {
		t.Fatal("immutable receipt changed")
	}
	if _, err := (enforcement.Repository{Pool: f.pool}).Withdraw(ctx, f.notice.ConsentRequestID, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DisablePolicy(ctx, az.PolicyKey{Registry: "lokos", Consumer: "sakhicredit", Resource: "shg.membership", Purpose: "credit_eligibility"}, "v1"); err != nil {
		t.Fatal(err)
	}
	restarted, err := custody.New(f.custodyConfig)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := restarted.Record(ctx, "lokos-adapter", signed)
	if err != nil || retry.ReceiptID != first.ReceiptID || !retry.AcceptedAt.Equal(first.AcceptedAt) {
		t.Fatal("lost acknowledgement retry changed evidence", err)
	}
	changed := f.event
	changed.Outcome = "failed"
	changed.Commitment = ""
	if _, err = restarted.Record(ctx, "lokos-adapter", signEvidence(t, f.key, changed, custody.EvidenceType, nil)); !errors.Is(err, custody.ErrConflict) {
		t.Fatal("event ID changed meaning", err)
	}
	changed = f.event
	changed.EventID = rand.Text()
	if _, err = restarted.Record(ctx, "lokos-adapter", signEvidence(t, f.key, changed, custody.EvidenceType, nil)); !errors.Is(err, custody.ErrConflict) {
		t.Fatal("another release event created authority", err)
	}
	f.counts(t, 1)
	for _, event := range []string{"release_started", "response_written"} {
		var version int64
		if err := f.pool.QueryRow(ctx, `SELECT version FROM access_requests WHERE request_id=$1`, f.event.RequestID).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if _, err := (access.Repository{Pool: f.pool}).Advance(ctx, f.event.RequestID, version, event); err != nil {
			t.Fatal(err)
		}
	}
	if ack, err := restarted.Record(ctx, "lokos-adapter", signed); err != nil || ack.ReceiptID != first.ReceiptID {
		t.Fatal("terminal history retry failed", err)
	}
	requestState(t, f.fixture, "COMPLETED")
}
func TestCustodyScopeSubstitutionDenied(t *testing.T) {
	f := setupCustody(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		edit func(*custody.Event)
	}{
		{"request", func(e *custody.Event) { e.RequestID = "other-request" }},
		{"digest", func(e *custody.Event) { e.Digest = cm.Digest([]byte("other")) }},
		{"consent", func(e *custody.Event) { e.ConsentID = "other-consent" }},
		{"grant", func(e *custody.Event) { e.GrantJTI = "other-grant" }},
		{"adapter", func(e *custody.Event) { e.AdapterID = "other-adapter" }},
		{"attempt", func(e *custody.Event) { e.AttemptID = "other-attempt" }},
		{"redemption", func(e *custody.Event) { e.RedemptionID = "other-redemption" }},
		{"fields", func(e *custody.Event) { e.Fields = []string{"membership_status"} }},
		{"unknown field", func(e *custody.Event) { e.Fields = []string{"private_source_field"} }},
		{"duplicates", func(e *custody.Event) { e.Fields = []string{"membership_status", "membership_status"} }},
		{"missing commitment", func(e *custody.Event) { e.Commitment = "" }},
		{"future", func(e *custody.Event) { e.OccurredAt = time.Now().Add(time.Hour) }},
		{"predates admission", func(e *custody.Event) { e.OccurredAt = time.Now().Add(-time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := f.event
			tc.edit(&e)
			if _, err := f.custodian.Record(ctx, "lokos-adapter", signEvidence(t, f.key, e, custody.EvidenceType, nil)); err == nil {
				t.Fatal("scope substitution accepted")
			}
			f.counts(t, 0)
		})
	}
	for _, tc := range []struct {
		typ   string
		extra map[string]any
	}{{"gramstack-grant+jwt", nil}, {custody.EvidenceType, map[string]any{"jku": "https://attacker.example/keys"}}} {
		if _, err := f.custodian.Record(ctx, "lokos-adapter", signEvidence(t, f.key, f.event, tc.typ, tc.extra)); !errors.Is(err, custody.ErrDenied) {
			t.Fatal("JOSE trust changed", err)
		}
	}
	foreign, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.custodian.Record(ctx, "lokos-adapter", signEvidence(t, foreign, f.event, custody.EvidenceType, nil)); !errors.Is(err, custody.ErrDenied) {
		t.Fatal("unapproved signing key accepted", err)
	}
	raw, _ := json.Marshal(f.event)
	var unknown map[string]any
	json.Unmarshal(raw, &unknown)
	unknown["private_payload"] = "must-not-be-stored"
	if _, err = f.custodian.Record(ctx, "lokos-adapter", signEvidence(t, f.key, unknown, custody.EvidenceType, nil)); !errors.Is(err, custody.ErrDenied) {
		t.Fatal("unknown evidence claim accepted", err)
	}
	f.counts(t, 0)
	requestState(t, f.fixture, "REDEEMING")
}
func TestCustodyTransactionFailureNeverAcknowledgesPartialWrites(t *testing.T) {
	for _, fault := range []string{"anchor state", "request outbox"} {
		t.Run(fault, func(t *testing.T) {
			f := setupCustody(t)
			ctx := context.Background()
			signed := signEvidence(t, f.key, f.event, custody.EvidenceType, nil)
			switch fault {
			case "anchor state":
				if _, err := f.pool.Exec(ctx, `CREATE FUNCTION fail_evidence_anchor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test fault'; END $$; CREATE TRIGGER fail_evidence_anchor BEFORE INSERT ON evidence_anchor_state FOR EACH ROW EXECUTE FUNCTION fail_evidence_anchor()`); err != nil {
					t.Fatal(err)
				}
			case "request outbox":
				if _, err := f.pool.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES('custody-fault','request',$1,7,'fault','{}')`, f.event.RequestID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.custodian.Record(ctx, "lokos-adapter", signed); err == nil {
				t.Fatal("faulty transaction acknowledged")
			}
			f.counts(t, 0)
			requestState(t, f.fixture, "REDEEMING")
			switch fault {
			case "anchor state":
				if _, err := f.pool.Exec(ctx, `DROP TRIGGER fail_evidence_anchor ON evidence_anchor_state; DROP FUNCTION fail_evidence_anchor()`); err != nil {
					t.Fatal(err)
				}
			case "request outbox":
				if _, err := f.pool.Exec(ctx, `DELETE FROM outbox WHERE event_id='custody-fault'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.custodian.Record(ctx, "lokos-adapter", signed); err != nil {
				t.Fatal("retry after rollback failed", err)
			}
			f.counts(t, 1)
			requestState(t, f.fixture, "EVIDENCE_COMMITTED")
		})
	}
}
func TestCustodyHTTPProducerBoundary(t *testing.T) {
	f := setupCustody(t)
	signed := signEvidence(t, f.key, f.event, custody.EvidenceType, nil)
	body, _ := json.Marshal(map[string]string{"jws": signed})
	for _, peer := range []string{"", "gateway.gramstack.internal", "lokos-adapter.gramstack.internal"} {
		r := httptest.NewRequest("POST", "https://gateway.example/internal/v1/evidence", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Gramstack-Contract-Version", "0.3.0")
		r.Header.Set("X-Forwarded-Client-Cert", "lokos-adapter.gramstack.internal")
		if peer != "" {
			cert := &x509.Certificate{DNSNames: []string{peer}}
			r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}, PeerCertificates: []*x509.Certificate{cert}}
		}
		w := httptest.NewRecorder()
		f.custodian.Handler().ServeHTTP(w, r)
		want := 403
		if peer == "lokos-adapter.gramstack.internal" {
			want = 201
		}
		if w.Code != want || strings.Contains(w.Body.String(), signed) {
			t.Fatal("producer trust boundary failed", w.Code, want)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Gramstack-Contract-Version") != "0.3.0" {
			t.Fatal("custody response controls missing")
		}
	}
	f.counts(t, 1)
}

func TestCustodyRecordsHistoricalAdmissionAfterActualGrantExpiry(t *testing.T) {
	f := setupCustodyLifetime(t, 2)
	ctx := context.Background()
	signed := signEvidence(t, f.key, f.event, custody.EvidenceType, nil)
	time.Sleep(max(0, time.Until(f.expiresAt)) + 10*time.Millisecond)
	var grant string
	if err := f.pool.QueryRow(ctx, `SELECT jws FROM retrieval_grants WHERE request_id=$1`, f.event.RequestID).Scan(&grant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.VerifyGrant(grant, time.Now().Unix()); !errors.Is(err, az.ErrExpired) {
		t.Fatal("test grant did not expire", err)
	}
	if _, err := f.custodian.Record(ctx, "lokos-adapter", signed); err != nil {
		t.Fatal("historical admission evidence lost at token expiry", err)
	}
	f.counts(t, 1)
	requestState(t, f.fixture, "EVIDENCE_COMMITTED")
}
func TestCustodyRejectsInvalidHistoricalAdmissionTimestamp(t *testing.T) {
	f := setupCustody(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE disclosure_reservations SET reserved_at=$2 WHERE redemption_id=$1`, f.event.RedemptionID, f.expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.custodian.Record(ctx, "lokos-adapter", signEvidence(t, f.key, f.event, custody.EvidenceType, nil)); !errors.Is(err, az.ErrBinding) {
		t.Fatal("out-of-bounds admission history accepted", err)
	}
	f.counts(t, 0)
}
