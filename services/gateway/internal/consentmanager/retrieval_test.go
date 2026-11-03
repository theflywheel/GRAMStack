package consentmanager_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/discovery"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/custody"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/retrieval"
)

type gatewayFixture struct {
	authorizationFixture
	gateway     *retrieval.Service
	custodian   *custody.Service
	claims      az.Grant
	compact     string
	certificate *x509.Certificate
	calls       atomic.Int32
	mode        string
	key         *ecdsa.PrivateKey
}

func setupGatewayRetrieval(t *testing.T, mode string) *gatewayFixture {
	t.Helper()
	f := setupAuthorization(t)
	cert := &x509.Certificate{Raw: []byte("unit-original-consumer-certificate")}
	sum := sha256.Sum256(cert.Raw)
	thumb := base64.RawURLEncoding.EncodeToString(sum[:])
	f.config.Presenters = map[string]string{"sakhicredit": thumb}
	service, err := az.New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	issued := f.issue(t, "gateway-key")
	claims, err := service.VerifyGrant(issued.Grant, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	custodian, err := custody.New(custody.Config{Pool: f.pool, Scopes: service, ReleaseProfile: "batch", Adapters: []custody.Adapter{{ID: "lokos-adapter", Audience: "lokos-adapter", PeerName: "lokos-adapter.gramstack.internal", Keys: map[string]*ecdsa.PublicKey{"adapter-key": &key.PublicKey}}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &gatewayFixture{authorizationFixture: f, custodian: custodian, claims: claims, compact: issued.Grant, certificate: cert, mode: mode, key: key}
	if mode == "policy denied" {
		if err := service.DisablePolicy(context.Background(), az.PolicyKey{Registry: "lokos", Consumer: "sakhicredit", Resource: "shg.membership", Purpose: "credit_eligibility"}, "v1"); err != nil {
			t.Fatal(err)
		}
	}
	adapter := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fixture.adapter(t, w, r) }))
	t.Cleanup(adapter.Close)
	pair := adapter.TLS.Certificates[0]
	secret, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})
	parsed, _ := url.Parse(adapter.URL)
	approved, err := discovery.Approve(discovery.Descriptor{Version: discovery.Version, Origin: adapter.URL, Registry: "lokos", AdapterID: "lokos-adapter", Audience: "lokos-adapter", VerificationMethod: "test-registry-approved", Resources: []discovery.Resource{{Name: "shg.membership", Version: "1", Fields: claims.Fields}}, Keys: map[string]*ecdsa.PublicKey{"adapter-key": &key.PublicKey}}, false)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := discovery.New(public)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(metadata.Close)
	gateway, err := retrieval.New(retrieval.Config{Discovery: metadata, Pool: f.pool, Authority: service, Receipts: custodian, Endpoints: []retrieval.Endpoint{{Registry: "lokos", AdapterID: "lokos-adapter", Audience: "lokos-adapter", Origin: adapter.URL, ServerName: parsed.Hostname(), Discovery: approved}}, CA: public, Certificate: public, Key: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}), MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	fixture.gateway = gateway
	return fixture
}
func (f *gatewayFixture) adapter(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == discovery.ManifestPath || r.URL.Path == discovery.KeysPath {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		w.Header().Set(discovery.VersionHeader, discovery.Version)
		if f.mode == "metadata offline" {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == discovery.ManifestPath {
			m := discovery.Manifest{AdapterID: "lokos-adapter", Registry: "lokos", Audience: "lokos-adapter", ContractVersion: "0.3.0", Authentication: "mtls", SigningKeyIDs: []string{"adapter-key"}, VerificationMethods: []string{"test-registry-approved"}, Capabilities: []string{"atomic-redemption", "evidence-before-release"}, Resources: []discovery.Resource{{Name: "shg.membership", Version: "1", Fields: f.claims.Fields}}}
			if f.mode == "metadata wrong resource" {
				m.Resources[0].Version = "2"
			}
			json.NewEncoder(w).Encode(m)
			return
		}
		key := &f.key.PublicKey
		if f.mode == "metadata wrong key" {
			other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			key = &other.PublicKey
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": []discovery.PublicJWK{{KeyType: "EC", Curve: "P-256", X: base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))), Y: base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))), ID: "adapter-key", Algorithm: "ES256", Use: "sig"}}})
		return
	}
	f.calls.Add(1)
	var input struct {
		AttemptID string       `json:"attempt_id"`
		Grant     string       `json:"grant"`
		Digest    string       `json:"request_digest"`
		Presenter az.Presenter `json:"presenter"`
	}
	if r.Method != "POST" || r.URL.Path != "/internal/v1/retrievals" || json.NewDecoder(r.Body).Decode(&input) != nil {
		http.Error(w, "invalid fixture request", 400)
		return
	}
	reservation, err := f.service.Redeem(r.Context(), "lokos-adapter", az.Redemption{AttemptID: input.AttemptID, Grant: input.Grant, RequestID: f.claims.RequestID, Digest: input.Digest, Presenter: input.Presenter})
	if err != nil || !reservation.First {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		w.Header().Set("Content-Type", "application/json")
		status := 409
		if errors.Is(err, az.ErrDenied) {
			status = 403
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"error":{"code":"AUTHORIZATION_DENIED","message":"Denied","request_id":"test","retryable":false}}`))
		return
	}
	if f.mode == "adapter error" {
		http.Error(w, "private fixture details", 503)
		return
	}
	receipt := "missing-receipt"
	if f.mode != "missing receipt" {
		event := custody.Event{EventID: "event-" + input.AttemptID, RequestID: f.claims.RequestID, Digest: f.claims.Digest, ConsentID: f.claims.ConsentID, GrantJTI: f.claims.JTI, AdapterID: "lokos-adapter", AttemptID: input.AttemptID, RedemptionID: reservation.ID, Fields: f.claims.Fields, Outcome: "release_ready", Commitment: cm.Digest([]byte("unit projection commitment")), OccurredAt: time.Now().UTC()}
		ack, err := f.custodian.Record(r.Context(), "lokos-adapter", signEvidence(t, f.key, event, custody.EvidenceType, nil))
		if err != nil {
			http.Error(w, "fixture custody failed", 503)
			return
		}
		receipt = ack.ReceiptID
	}
	if f.mode == "wrong receipt" {
		receipt = "wrong-receipt"
	}
	data := map[string]any{"membership_status": "active", "savings_summary": map[string]any{"amount": 12500.75, "currency": "INR"}}
	if f.mode == "extra field" {
		data["private_field"] = "must-not-leak"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Gramstack-Contract-Version", "0.3.0")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"request_id": f.claims.RequestID, "data": data, "receipt_id": receipt})
}
func (f *gatewayFixture) request() *http.Request {
	r := httptest.NewRequest("GET", "https://gateway.example/v1/access-requests/"+f.claims.RequestID+"/data", nil)
	r.Header.Set("Authorization", "Bearer "+f.compact)
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{f.certificate}}, PeerCertificates: []*x509.Certificate{f.certificate}}
	return r
}

type brokenResponse struct {
	headers http.Header
	status  int
}

func (w *brokenResponse) Header() http.Header    { return w.headers }
func (w *brokenResponse) WriteHeader(status int) { w.status = status }
func (w *brokenResponse) Write(body []byte) (int, error) {
	return len(body) / 2, errors.New("test partial write")
}
func TestGatewayReleaseGatesAndAmbiguousWrites(t *testing.T) {
	for _, mode := range []string{"normal", "missing receipt", "wrong receipt", "extra field", "adapter error", "partial write", "policy denied"} {
		t.Run(mode, func(t *testing.T) {
			f := setupGatewayRetrieval(t, mode)
			var status int
			var body string
			if mode == "partial write" {
				writer := &brokenResponse{headers: http.Header{}}
				f.gateway.Handler().ServeHTTP(writer, f.request())
				status = writer.status
			} else {
				writer := httptest.NewRecorder()
				f.gateway.Handler().ServeHTTP(writer, f.request())
				status, body = writer.Code, writer.Body.String()
			}
			expectedStatus, expectedState := 503, "DELIVERY_UNKNOWN"
			if mode == "normal" {
				expectedStatus, expectedState = 200, "COMPLETED"
			} else if mode == "partial write" {
				expectedStatus = 200
			} else if mode == "policy denied" {
				expectedStatus, expectedState = 403, "FAILED"
			}
			if status != expectedStatus {
				t.Fatal("unexpected release result", status, expectedStatus)
			}
			requestState(t, f.fixture, expectedState)
			if mode != "normal" && mode != "partial write" && (strings.Contains(body, "membership_status") || strings.Contains(body, "must-not-leak") || strings.Contains(body, "private fixture")) {
				t.Fatal("failed response leaked resource or source details")
			}
			replay := httptest.NewRecorder()
			f.gateway.Handler().ServeHTTP(replay, f.request())
			if replay.Code != 409 || f.calls.Load() != 1 {
				t.Fatal("failed or completed disclosure redispatched", replay.Code, f.calls.Load())
			}
		})
	}
}
func TestGatewayCheckpointRollbackWithholdsResponse(t *testing.T) {
	for _, checkpoint := range []string{"dispatch", "release", "finish"} {
		t.Run(checkpoint, func(t *testing.T) {
			f := setupGatewayRetrieval(t, "normal")
			ctx := context.Background()
			aggregate, version := "retrieval", int64(1)
			if checkpoint == "release" {
				aggregate, version = "request", 8
			} else if checkpoint == "finish" {
				aggregate, version = "request", 9
			}
			if _, err := f.pool.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES('gateway-fault',$1,$2,$3,'test.fault','{}')`, aggregate, f.claims.RequestID, version); err != nil {
				t.Fatal(err)
			}
			writer := httptest.NewRecorder()
			f.gateway.Handler().ServeHTTP(writer, f.request())
			want := 503
			if checkpoint == "finish" {
				want = 200
			}
			if writer.Code != want {
				t.Fatal("checkpoint fault result", writer.Code, want)
			}
			switch checkpoint {
			case "dispatch":
				var count int
				if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM gateway_retrieval_attempts WHERE request_id=$1`, f.claims.RequestID).Scan(&count); err != nil || count != 0 || f.calls.Load() != 0 {
					t.Fatal("dispatch rollback admitted an adapter call", err)
				}
				requestState(t, f.fixture, "AUTHORIZED")
			case "release":
				if strings.Contains(writer.Body.String(), "membership_status") {
					t.Fatal("release checkpoint failure wrote plaintext")
				}
				requestState(t, f.fixture, "EVIDENCE_COMMITTED")
			case "finish":
				requestState(t, f.fixture, "RELEASE_ATTEMPTED")
			}
			if _, err := f.pool.Exec(ctx, `DELETE FROM outbox WHERE event_id='gateway-fault'`); err != nil {
				t.Fatal(err)
			}
			retry := httptest.NewRecorder()
			f.gateway.Handler().ServeHTTP(retry, f.request())
			if checkpoint == "dispatch" {
				if retry.Code != 200 || f.calls.Load() != 1 {
					t.Fatal("rolled-back dispatch was not safely retryable")
				}
			} else if retry.Code != 409 || f.calls.Load() != 1 {
				t.Fatal("ambiguous checkpoint redispatched data", retry.Code)
			}
		})
	}
}
func TestGatewayAbandonedDispatchRecoveryNeverFetches(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "before admission", true: "after admission"}[admitted], func(t *testing.T) {
			f := setupGatewayRetrieval(t, "normal")
			ctx := context.Background()
			attempt := "abandoned-attempt"
			if admitted {
				if _, err := f.service.Redeem(ctx, "lokos-adapter", az.Redemption{AttemptID: attempt, Grant: f.compact, RequestID: f.claims.RequestID, Digest: f.claims.Digest, Presenter: az.Presenter{Consumer: f.claims.Consumer, Thumbprint: f.claims.Confirmation.Thumbprint}}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.pool.Exec(ctx, `INSERT INTO gateway_retrieval_attempts(request_id,attempt_id,consumer_id,grant_digest,presenter_thumbprint,deadline) VALUES($1,$2,$3,$4,$5,clock_timestamp()-interval '1 second')`, f.claims.RequestID, attempt, f.claims.Consumer, cm.Digest([]byte(f.compact)), f.claims.Confirmation.Thumbprint); err != nil {
				t.Fatal(err)
			}
			if err := f.gateway.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			want := "FAILED"
			if admitted {
				want = "DELIVERY_UNKNOWN"
			}
			requestState(t, f.fixture, want)
			var version int64
			if err := f.pool.QueryRow(ctx, `SELECT version FROM access_requests WHERE request_id=$1`, f.claims.RequestID).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := f.gateway.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			var after int64
			if err := f.pool.QueryRow(ctx, `SELECT version FROM access_requests WHERE request_id=$1`, f.claims.RequestID).Scan(&after); err != nil || after != version {
				t.Fatal("recovery duplicated lifecycle", err)
			}
			response := httptest.NewRecorder()
			f.gateway.Handler().ServeHTTP(response, f.request())
			if response.Code != 409 || f.calls.Load() != 0 {
				t.Fatal("abandoned attempt resumed source execution", response.Code)
			}
			if _, err := f.pool.Exec(ctx, `UPDATE gateway_retrieval_attempts SET deadline=clock_timestamp()+interval '1 minute' WHERE request_id=$1`, f.claims.RequestID); err == nil {
				t.Fatal("dispatch lease was silently renewable")
			}
		})
	}
}

func TestDiscoveryDenialDoesNotClaimOrDispatch(t *testing.T) {
	for _, mode := range []string{"metadata offline", "metadata wrong resource", "metadata wrong key"} {
		t.Run(mode, func(t *testing.T) {
			f := setupGatewayRetrieval(t, mode)
			ctx := context.Background()
			w := httptest.NewRecorder()
			f.gateway.Handler().ServeHTTP(w, f.request())
			if w.Code != 503 || f.calls.Load() != 0 {
				t.Fatal("unapproved metadata reached protected adapter", w.Code)
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM gateway_retrieval_attempts WHERE request_id=$1`, f.claims.RequestID).Scan(&count); err != nil || count != 0 {
				t.Fatal("discovery failure consumed a dispatch", err)
			}
			requestState(t, f.fixture, "AUTHORIZED")
			f.mode = "normal"
			again := httptest.NewRecorder()
			f.gateway.Handler().ServeHTTP(again, f.request())
			if again.Code != 200 || f.calls.Load() != 1 {
				t.Fatal("metadata recovery could not safely use the unclaimed request", again.Code)
			}
		})
	}
}
