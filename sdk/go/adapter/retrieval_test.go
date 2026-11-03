package adapter

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type attemptMemory struct {
	mu        sync.Mutex
	values    map[string]Attempt
	protected map[string]ProtectedEvidence
	events    []string
	fail      string
}

func (m *attemptMemory) Claim(_ context.Context, a Attempt) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.values {
		if v.ID == a.ID || v.RequestID == a.RequestID || v.GrantJTI == a.GrantJTI {
			return Attempt{}, ErrAttemptExists
		}
	}
	a.State = "CLAIMED"
	a.Version = 1
	m.values[a.ID] = a
	return a, nil
}
func (m *attemptMemory) Get(_ context.Context, id string) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.values[id]
	if !ok {
		return a, ErrAttemptConflict
	}
	return a, nil
}
func (m *attemptMemory) Advance(ctx context.Context, id string, version int64, event string, u AttemptUpdate) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.values[id]
	if a.Version != version || event == m.fail {
		return Attempt{}, ErrAttemptConflict
	}
	state, err := attemptTransition(ctx, a.State, event)
	if err != nil {
		return Attempt{}, err
	}
	a.State = state
	a.Version++
	if u.RedemptionID != "" {
		a.RedemptionID = u.RedemptionID
	}
	if u.Evidence != nil {
		m.protected[id] = *u.Evidence
		a.EvidenceDigest = Digest([]byte(u.Evidence.JWS))
	}
	if u.Ack != nil {
		a.ReceiptID = u.Ack.ReceiptID
	}
	m.values[id] = a
	m.events = append(m.events, event)
	return a, nil
}

type retrievalProvider struct {
	calls atomic.Int32
	fetch func(context.Context, Subject, []string) (Data, error)
}

func (p *retrievalProvider) Resource() Resource {
	return Resource{Name: "shg.membership", Version: "1", Fields: []string{"membership_status", "savings_summary"}}
}
func (p *retrievalProvider) Fetch(ctx context.Context, s Subject, f []string) (Data, error) {
	p.calls.Add(1)
	return p.fetch(ctx, s, f)
}

type authorityFunc func(context.Context, RedemptionRequest) (Reservation, error)

func (f authorityFunc) Redeem(ctx context.Context, r RedemptionRequest) (Reservation, error) {
	return f(ctx, r)
}

type custodianFunc func(context.Context, string) (EvidenceAcknowledgement, error)

func (f custodianFunc) Record(ctx context.Context, s string) (EvidenceAcknowledgement, error) {
	return f(ctx, s)
}

type retrievalFixture struct {
	config   RetrievalConfig
	input    RetrievalRequest
	claims   Grant
	grantKey *ecdsa.PrivateKey
	attempts *attemptMemory
	provider *retrievalProvider
	contexts *contextMemory
	recorded atomic.Int32
}

func signGrant(t *testing.T, key *ecdsa.PrivateKey, g Grant, kind string, extra map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType(jose.ContentType(kind)).WithHeader("kid", "grant-key")
	for k, v := range extra {
		options.WithHeader(jose.HeaderKey(k), v)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, options)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Canonical(g)
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
func newRetrievalFixture(t *testing.T) *retrievalFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	evidenceKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewEvidenceSigner("adapter-key", evidenceKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewGrantVerifier("https://gateway.example", "lokos-adapter", map[string]*ecdsa.PublicKey{"grant-key": &key.PublicKey}, false)
	if err != nil {
		t.Fatal(err)
	}
	v := verificationInput()
	now := time.Now().UTC()
	g := Grant{Issuer: "https://gateway.example", Audience: "lokos-adapter", Subject: v.Principal.ContextID, Consumer: v.Snapshot.ConsumerID, JTI: "jti-1", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), RequestID: v.Snapshot.RequestID, Digest: v.RequestDigest, Resource: v.Snapshot.Request.Resource, Fields: slices.Clone(v.Snapshot.Request.Fields), Purpose: v.Snapshot.Request.Purpose, ConsentID: "consent-1", ContextID: "ctx-1", PolicyVersion: "v1"}
	g.Confirmation.Thumbprint = strings.Repeat("a", 43)
	f := &retrievalFixture{claims: g, grantKey: key, attempts: &attemptMemory{values: map[string]Attempt{}, protected: map[string]ProtectedEvidence{}}, contexts: &contextMemory{values: map[string]StoredContext{}}}
	f.input = RetrievalRequest{AttemptID: "attempt-1", Snapshot: v.Snapshot, Digest: v.RequestDigest, Grant: signGrant(t, key, g, GrantType, nil), Presenter: Presenter{Thumbprint: g.Confirmation.Thumbprint, Consumer: g.Consumer}}
	f.contexts.values[g.ContextID] = StoredContext{RequestID: g.RequestID, PrincipalDigest: Digest([]byte("private principal")), Context: SubjectContext{ContextID: g.ContextID, Registry: "lokos", RequestDigest: g.Digest, PrincipalContextRef: g.Subject, SourceMappingRef: "native-mapping", VerificationMethod: "registry.test-v1", VerifiedAt: now.Add(-time.Second), ExpiresAt: now.Add(2 * time.Minute)}}
	f.provider = &retrievalProvider{fetch: func(_ context.Context, s Subject, fields []string) (Data, error) {
		a, _ := f.attempts.Get(context.Background(), f.input.AttemptID)
		if a.State != "FETCHING" || s.Reference != "native-mapping" || !slices.Equal(fields, []string{"membership_status"}) {
			return nil, errors.New("unadmitted source read")
		}
		return Data{"membership_status": json.RawMessage(`"active"`), "savings_summary": json.RawMessage(`{"amount":12345,"currency":"INR"}`), "unrequested_private_field": json.RawMessage(`"must-not-leak"`)}, nil
	}}
	f.config = RetrievalConfig{Registry: "lokos", AdapterID: "lokos-adapter", GatewayIdentity: "gateway.example", VerificationMethod: "registry.test-v1", Provider: f.provider, Grants: verifier, Contexts: f.contexts, Attempts: f.attempts, Authority: authorityFunc(func(_ context.Context, r RedemptionRequest) (Reservation, error) {
		if r.RequestID != g.RequestID || r.Digest != g.Digest || r.Grant != f.input.Grant || r.Presenter != f.input.Presenter {
			return Reservation{}, ErrGrant
		}
		return Reservation{ID: "reservation-1", AttemptID: r.AttemptID, First: true, ReservedAt: time.Now()}, nil
	}), Custodian: custodianFunc(func(_ context.Context, compact string) (EvidenceAcknowledgement, error) {
		f.recorded.Add(1)
		event, err := VerifyEvidence(compact, map[string]*ecdsa.PublicKey{"adapter-key": &evidenceKey.PublicKey})
		if err != nil || event.Outcome != "release_ready" || event.RedemptionID != "reservation-1" {
			return EvidenceAcknowledgement{}, ErrGrant
		}
		return EvidenceAcknowledgement{ReceiptID: "receipt-1", Digest: Digest([]byte(compact)), AcceptedAt: time.Now(), AnchorStatus: "pending"}, nil
	}), Signer: signer, SigningKeys: map[string]*ecdsa.PublicKey{"adapter-key": &evidenceKey.PublicKey}, ReleaseProfile: "batch", MaxConcurrent: 64}
	return f
}
func (f *retrievalFixture) service(t *testing.T) *RetrievalService {
	t.Helper()
	s, err := NewRetrieval(f.config)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func retrievalHTTP(input RetrievalRequest) *http.Request {
	raw, _ := json.Marshal(input)
	r := httptest.NewRequest("POST", "https://adapter.example/internal/v1/retrievals", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Gramstack-Contract-Version", CoreVersion)
	cert := &x509.Certificate{DNSNames: []string{"gateway.example"}}
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}, PeerCertificates: []*x509.Certificate{cert}}
	return r
}
func TestRetrievalProjectsAfterDurableEvidenceAndNeverReplays(t *testing.T) {
	f := newRetrievalFixture(t)
	service := f.service(t)
	w := httptest.NewRecorder()
	service.Handler().ServeHTTP(w, retrievalHTTP(f.input))
	var response DataResponse
	if w.Code != 200 || Decode(w.Body.Bytes(), &response) != nil || len(response.Data) != 1 || string(response.Data["membership_status"]) != `"active"` || response.ReceiptID != "receipt-1" || f.recorded.Load() != 1 {
		t.Fatal("authorized release failed", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "must-not-leak") || strings.Contains(w.Body.String(), "12345") {
		t.Fatal("unrequested data leaked")
	}
	a, _ := f.attempts.Get(context.Background(), f.input.AttemptID)
	if a.State != "COMPLETED" || a.Version != 7 {
		t.Fatal("completion boundary missing", a.State, a.Version)
	}
	if !slices.Equal(f.attempts.events, []string{"admitted", "source_started", "evidence_prepared", "evidence_committed", "release_started", "response_written"}) {
		t.Fatal("processing order changed", f.attempts.events)
	}
	evidence := f.attempts.protected[a.ID]
	raw, _ := Canonical(response.Data)
	if len(evidence.Salt) != 32 {
		t.Fatal("private salt missing")
	}
	event, err := VerifyEvidence(evidence.JWS, f.config.SigningKeys)
	if err != nil || event.ResponseCommitment != Digest(append(slices.Clone(evidence.Salt), raw...)) {
		t.Fatal("commitment did not bind exact projected data", err)
	}
	for _, attempt := range []string{f.input.AttemptID, "different-attempt"} {
		v := f.input
		v.AttemptID = attempt
		restarted := f.service(t)
		again := httptest.NewRecorder()
		restarted.Handler().ServeHTTP(again, retrievalHTTP(v))
		if again.Code != 409 || strings.Contains(again.Body.String(), "active") {
			t.Fatal("attempt retry released data", again.Code)
		}
	}
	if f.provider.calls.Load() != 1 {
		t.Fatal("source was fetched again")
	}
}
func TestRetrievalRejectsAuthorityAndScopeBeforeClaim(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Grant, *RetrievalRequest)
	}{
		{"issuer", func(g *Grant, _ *RetrievalRequest) { g.Issuer = "https://attacker.example" }},
		{"audience", func(g *Grant, _ *RetrievalRequest) { g.Audience = "different-adapter" }},
		{"expired", func(g *Grant, _ *RetrievalRequest) {
			g.ExpiresAt = time.Now().Add(-time.Second).Unix()
			g.IssuedAt = g.ExpiresAt - 30
		}},
		{"future", func(g *Grant, _ *RetrievalRequest) {
			g.IssuedAt = time.Now().Add(time.Minute).Unix()
			g.ExpiresAt = g.IssuedAt + 30
		}},
		{"long lifetime", func(g *Grant, _ *RetrievalRequest) { g.ExpiresAt = g.IssuedAt + 301 }},
		{"scope", func(g *Grant, _ *RetrievalRequest) { g.Fields = []string{"membership_status", "savings_summary"} }},
		{"purpose", func(g *Grant, _ *RetrievalRequest) { g.Purpose = "marketing" }},
		{"request", func(g *Grant, _ *RetrievalRequest) { g.RequestID = "another-request" }},
		{"subject", func(g *Grant, _ *RetrievalRequest) { g.Subject = "another-person" }},
		{"context", func(g *Grant, _ *RetrievalRequest) { g.ContextID = "another-context" }},
		{"presenter", func(_ *Grant, r *RetrievalRequest) { r.Presenter.Thumbprint = strings.Repeat("b", 43) }},
		{"snapshot", func(_ *Grant, r *RetrievalRequest) { r.Snapshot.Request.SubjectHint.Value = "another-member" }},
		{"digest", func(_ *Grant, r *RetrievalRequest) { r.Digest = Digest([]byte("different")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRetrievalFixture(t)
			g := f.claims
			tc.change(&g, &f.input)
			f.input.Grant = signGrant(t, f.grantKey, g, GrantType, nil)
			w := httptest.NewRecorder()
			f.service(t).Handler().ServeHTTP(w, retrievalHTTP(f.input))
			if w.Code != 403 && w.Code != 410 {
				t.Fatal("invalid authority admitted", w.Code)
			}
			if len(f.attempts.values) != 0 || f.provider.calls.Load() != 0 || f.recorded.Load() != 0 {
				t.Fatal("invalid authority touched protected execution")
			}
		})
	}
	for _, tc := range []struct {
		kind  string
		extra map[string]any
	}{{EvidenceType, nil}, {GrantType, map[string]any{"jku": "https://attacker.example/keys"}}, {GrantType, map[string]any{"crit": []string{"unknown"}, "unknown": true}}} {
		f := newRetrievalFixture(t)
		f.input.Grant = signGrant(t, f.grantKey, f.claims, tc.kind, tc.extra)
		w := httptest.NewRecorder()
		f.service(t).Handler().ServeHTTP(w, retrievalHTTP(f.input))
		if w.Code != 403 || f.provider.calls.Load() != 0 {
			t.Fatal("JOSE trust substituted", w.Code)
		}
	}
}
func TestRetrievalFailureGatesWithholdPlaintext(t *testing.T) {
	for _, stage := range []string{"admission", "historical admission", "source", "source checkpoint", "evidence signing", "evidence outage", "wrong receipt digest", "synchronous pending", "release checkpoint"} {
		t.Run(stage, func(t *testing.T) {
			f := newRetrievalFixture(t)
			switch stage {
			case "admission":
				f.config.Authority = authorityFunc(func(context.Context, RedemptionRequest) (Reservation, error) {
					return Reservation{}, ErrRetrievalDependency
				})
			case "historical admission":
				f.config.Authority = authorityFunc(func(_ context.Context, r RedemptionRequest) (Reservation, error) {
					return Reservation{ID: "reservation-1", AttemptID: r.AttemptID, First: false, ReservedAt: time.Now()}, nil
				})
			case "source":
				f.provider.fetch = func(context.Context, Subject, []string) (Data, error) {
					return nil, errors.New("private source details must not leak")
				}
			case "source checkpoint":
				f.attempts.fail = "source_started"
			case "evidence signing":
				f.config.Signer = evidenceSignFunc(func(context.Context, EvidenceEvent) (string, error) { return "", errors.New("offline") })
			case "evidence outage":
				f.config.Custodian = custodianFunc(func(context.Context, string) (EvidenceAcknowledgement, error) {
					return EvidenceAcknowledgement{}, errors.New("offline")
				})
			case "wrong receipt digest":
				f.config.Custodian = custodianFunc(func(context.Context, string) (EvidenceAcknowledgement, error) {
					return EvidenceAcknowledgement{ReceiptID: "receipt-1", Digest: Digest([]byte("other")), AcceptedAt: time.Now(), AnchorStatus: "pending"}, nil
				})
			case "synchronous pending":
				f.config.ReleaseProfile = "synchronous"
			case "release checkpoint":
				f.attempts.fail = "release_started"
			}
			w := httptest.NewRecorder()
			f.service(t).Handler().ServeHTTP(w, retrievalHTTP(f.input))
			if w.Code == 200 || strings.Contains(w.Body.String(), "active") || strings.Contains(w.Body.String(), "private source") {
				t.Fatal("failure released data", w.Code)
			}
			firstCalls := f.provider.calls.Load()
			v := f.input
			v.AttemptID = "new-attempt"
			retry := httptest.NewRecorder()
			f.service(t).Handler().ServeHTTP(retry, retrievalHTTP(v))
			if retry.Code != 409 || f.provider.calls.Load() != firstCalls {
				t.Fatal("failed attempt became repeatable", retry.Code)
			}
			if (stage == "admission" || stage == "historical admission" || stage == "source checkpoint") && firstCalls != 0 {
				t.Fatal("read before durable new admission")
			}
		})
	}
}

type evidenceSignFunc func(context.Context, EvidenceEvent) (string, error)

func (f evidenceSignFunc) Sign(c context.Context, e EvidenceEvent) (string, error) { return f(c, e) }

type failedWriter struct {
	header http.Header
	status int
}

func (w *failedWriter) Header() http.Header           { return w.header }
func (w *failedWriter) WriteHeader(code int)          { w.status = code }
func (w *failedWriter) Write(raw []byte) (int, error) { return len(raw) / 2, io.ErrUnexpectedEOF }
func TestRetrievalAmbiguousWriteAndBoundedConcurrency(t *testing.T) {
	f := newRetrievalFixture(t)
	w := &failedWriter{header: http.Header{}}
	f.service(t).Handler().ServeHTTP(w, retrievalHTTP(f.input))
	a, _ := f.attempts.Get(context.Background(), f.input.AttemptID)
	if w.status != 200 || a.State != "DELIVERY_UNKNOWN" {
		t.Fatal("ambiguous delivery marked complete", a.State)
	}
	f2 := newRetrievalFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	original := f2.provider.fetch
	f2.provider.fetch = func(ctx context.Context, s Subject, fields []string) (Data, error) {
		close(entered)
		<-release
		return original(ctx, s, fields)
	}
	f2.config.MaxConcurrent = 1
	service := f2.service(t)
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		service.Handler().ServeHTTP(w, retrievalHTTP(f2.input))
		done <- w.Code
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("source gate not reached")
	}
	busy := httptest.NewRecorder()
	service.Handler().ServeHTTP(busy, retrievalHTTP(f2.input))
	if busy.Code != 429 {
		t.Fatal("unbounded concurrent execution", busy.Code)
	}
	close(release)
	if code := <-done; code != 200 {
		t.Fatal("admitted request failed", code)
	}
	var wg sync.WaitGroup
	statuses := make(chan int, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			service.Handler().ServeHTTP(w, retrievalHTTP(f2.input))
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status != 409 && status != 429 {
			t.Fatal("duplicate admitted", status)
		}
	}
	if f2.provider.calls.Load() != 1 {
		t.Fatal("concurrent replay fetched source again")
	}
}
func TestRetrievalRequiresVerifiedGatewayTransport(t *testing.T) {
	f := newRetrievalFixture(t)
	for _, unverified := range []bool{true, false} {
		r := retrievalHTTP(f.input)
		if unverified {
			r.TLS.VerifiedChains = nil
		} else {
			r.TLS = nil
		}
		r.Header.Set("X-Forwarded-Client-Cert", "gateway.example")
		w := httptest.NewRecorder()
		f.service(t).Handler().ServeHTTP(w, r)
		if w.Code != 403 || len(f.attempts.values) != 0 {
			t.Fatal("unverified service claimed attempt")
		}
	}
}

func TestGrantVerifierCopiesKeysAndRejectsSignatureSubstitution(t *testing.T) {
	f := newRetrievalFixture(t)
	key := f.grantKey
	key.X = nil
	key.Y = nil
	if _, err := f.config.Grants.Verify(f.input.Grant, time.Now()); err != nil {
		t.Fatal("caller mutation changed pinned key", err)
	}
	parts := strings.Split(f.input.Grant, ".")
	signature := []byte(parts[2])
	if signature[0] == 'A' {
		signature[0] = 'B'
	} else {
		signature[0] = 'A'
	}
	parts[2] = string(signature)
	if _, err := f.config.Grants.Verify(strings.Join(parts, "."), time.Now()); !errors.Is(err, ErrGrant) {
		t.Fatal("modified signature accepted", err)
	}
}
func TestEvidenceSignerSubstitutionCannotRelease(t *testing.T) {
	f := newRetrievalFixture(t)
	original := f.config.Signer
	f.config.Signer = evidenceSignFunc(func(ctx context.Context, e EvidenceEvent) (string, error) {
		e.Fields = []string{"savings_summary"}
		return original.Sign(ctx, e)
	})
	w := httptest.NewRecorder()
	f.service(t).Handler().ServeHTTP(w, retrievalHTTP(f.input))
	if w.Code != 503 || f.recorded.Load() != 0 || strings.Contains(w.Body.String(), "active") {
		t.Fatal("valid signature changed the disclosure evidence scope", w.Code)
	}
}

type blockingWriter struct {
	header           http.Header
	entered, release chan struct{}
}

func (w *blockingWriter) Header() http.Header { return w.header }
func (w *blockingWriter) WriteHeader(int)     {}
func (w *blockingWriter) Write(raw []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(raw), nil
}
func TestRetrievalCapacityHeldThroughResponseWrite(t *testing.T) {
	f := newRetrievalFixture(t)
	f.config.MaxConcurrent = 1
	service := f.service(t)
	writer := &blockingWriter{header: http.Header{}, entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); service.Handler().ServeHTTP(writer, retrievalHTTP(f.input)) }()
	select {
	case <-writer.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("response write gate not reached")
	}
	second := httptest.NewRecorder()
	service.Handler().ServeHTTP(second, retrievalHTTP(f.input))
	if second.Code != 429 {
		t.Error("capacity released before response finished", second.Code)
	}
	close(writer.release)
	<-done
}
