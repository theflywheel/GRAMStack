package application

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SAKHI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated product PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "sakhi_" + strings.ToLower(rand.Text())
	name := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 8
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal("idempotent migration failed", err)
	}
	return pool
}
func certificates(t *testing.T) ([]byte, []byte, []byte, tls.Certificate) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "product test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	raw, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	leaf := func(serial int64, name string, usage x509.ExtKeyUsage) ([]byte, []byte) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		cert := &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		raw, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		secret, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret})
	}
	cert, key := leaf(2, "consumer-one", x509.ExtKeyUsageClientAuth)
	serverCert, serverKey := leaf(3, "gateway.test", x509.ExtKeyUsageServerAuth)
	pair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	return caPEM, cert, key, pair
}

type fixture struct {
	service                           *Service
	pool                              *pgxpool.Pool
	owner, token, csrf                string
	creates, fetches                  atomic.Int64
	createCode, grantCode, statusCode atomic.Int64
	fetchOffline                      atomic.Bool
	status                            atomic.Value
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{pool: database(t)}
	f.status.Store("CONSENTED")
	key := bytes.Repeat([]byte{7}, 32)
	store, err := NewStore(f.pool, key)
	if err != nil {
		t.Fatal(err)
	}
	f.token, f.csrf, err = store.NewSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.owner = ownerOf(f.token)
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			id, secret, ok := r.BasicAuth()
			if !ok || id != "consumer-one" || secret != "not-production" {
				w.WriteHeader(401)
				return
			}
			writeJSON(w, 200, map[string]any{"access_token": "opaque-token", "token_type": "bearer", "expires_in": 300, "scope": "gramstack.access"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer opaque-token" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		if r.URL.Path == "/v1/access-requests" {
			f.creates.Add(1)
			if code := f.createCode.Load(); code != 0 {
				w.WriteHeader(int(code))
				return
			}
			var request map[string]any
			raw, _ := io.ReadAll(r.Body)
			if json.Unmarshal(raw, &request) != nil || strings.Contains(string(raw), "not-production") {
				t.Error("bad product request")
			}
			id := "req-" + strings.TrimPrefix(r.Header.Get("Idempotency-Key"), "sakhi-create-")
			// The URL's origin is assigned below before this handler can be called.
			origin := "http://" + r.Host
			writeJSON(w, 202, Accepted{id, "AUTHENTICATION_PENDING", origin + "/v1/access-requests/" + id, origin + "/interactions/" + id, time.Unix(1900000000, 0).UTC()})
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 {
			w.WriteHeader(404)
			return
		}
		id := parts[3]
		if strings.HasSuffix(r.URL.Path, "/grant") {
			if code := f.grantCode.Load(); code != 0 {
				w.WriteHeader(int(code))
				return
			}
			writeJSON(w, 200, grantResponse{"signed-grant", time.Now().Add(time.Minute).UTC(), "Bearer"})
			return
		}
		if code := f.statusCode.Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		status := f.status.Load().(string)
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			status = "CANCELLED"
		}
		writeJSON(w, 200, PlatformStatus{id, status, 5, "wait", time.Unix(1900000000, 0).UTC()})
	}))
	t.Cleanup(public.Close)
	ca, cert, keyPEM, pair := certificates(t)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	private := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || r.TLS.PeerCertificates[0].VerifyHostname("consumer-one") != nil || r.Header.Get("Authorization") != "Bearer signed-grant" {
			w.WriteHeader(403)
			return
		}
		f.fetches.Add(1)
		if f.fetchOffline.Load() {
			w.WriteHeader(503)
			return
		}
		id := strings.Split(r.URL.Path, "/")[3]
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		writeJSON(w, 200, Result{id, map[string]json.RawMessage{"membership_status": json.RawMessage(`"active"`), "savings_summary": json.RawMessage(`{"amount":12500.75,"currency":"INR"}`)}, "receipt-private"})
	}))
	private.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{pair}}
	private.StartTLS()
	t.Cleanup(private.Close)
	client, err := NewClient(ClientConfig{PublicOrigin: public.URL, DataOrigin: private.URL, TokenURL: public.URL + "/oauth2/token", ClientID: "consumer-one", ClientSecret: "not-production", DataServerName: "gateway.test", CA: ca, Certificate: cert, Key: keyPEM, AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = NewService(store, client)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) ready(t *testing.T) Application {
	t.Helper()
	ctx := context.Background()
	app, err := f.service.Create(ctx, f.owner, "create-key", Input{"member-12345", true})
	if err != nil {
		t.Fatal(err)
	}
	app, err = f.service.Refresh(ctx, f.owner, app.ID)
	if err != nil || app.State != "READY" {
		t.Fatal(app, err)
	}
	return app
}
func TestProductConcurrentFetchEncryptedResultAndRestart(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	app := f.ready(t)
	var group sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		group.Go(func() {
			_, err := f.service.Retrieve(ctx, f.owner, app.ID)
			if err != nil && !errors.Is(err, ErrConflict) {
				results <- err
			}
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		t.Error(err)
	}
	if f.fetches.Load() != 1 {
		t.Fatal("product repeated a disclosure", f.fetches.Load())
	}
	loaded, err := f.service.Get(ctx, f.owner, app.ID)
	if err != nil || loaded.State != "COMPLETE" || loaded.Result == nil || loaded.Result.ReceiptID != "receipt-private" {
		t.Fatal("missing protected result", loaded, err)
	}
	var rawInput, rawResult []byte
	if err := f.pool.QueryRow(ctx, `SELECT input_cipher,result_cipher FROM sakhi_applications WHERE application_id=$1`, app.ID).Scan(&rawInput, &rawResult); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawInput, []byte("member-12345")) || bytes.Contains(rawResult, []byte("12500.75")) {
		t.Fatal("product stored plaintext")
	}
	store, err := NewStore(f.pool, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewService(store, f.service.client)
	again, err := restarted.Retrieve(ctx, f.owner, app.ID)
	if err != nil || again.Result == nil || f.fetches.Load() != 1 {
		t.Fatal("restart fetched again", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE sakhi_applications SET state='READY',result_cipher=NULL WHERE application_id=$1`, app.ID); err == nil {
		t.Fatal("terminal product result revived")
	}
	wrong, _ := NewStore(f.pool, bytes.Repeat([]byte{8}, 32))
	wrongService, _ := NewService(wrong, f.service.client)
	if _, err := wrongService.Get(ctx, f.owner, app.ID); err == nil {
		t.Fatal("wrong storage key accepted")
	}
	if _, err := f.service.Create(ctx, f.owner, "create-key", Input{"member-67890", true}); !errors.Is(err, ErrConflict) {
		t.Fatal("idempotency payload changed", err)
	}
	if _, err := f.service.Get(ctx, "other-owner", app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-browser result exposed", err)
	}
}
func TestProductOutageCancellationAndTerminalStatus(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.createCode.Store(503)
	app, err := f.service.Create(ctx, f.owner, "create-key", Input{"member-12345", true})
	if err == nil || app.State != "CONNECTING" {
		t.Fatal("create outage not recoverable", app, err)
	}
	f.createCode.Store(0)
	app, err = f.service.Refresh(ctx, f.owner, app.ID)
	if err != nil || app.State != "WAITING" {
		t.Fatal("create retry failed", app, err)
	}
	original := app.ID
	app, err = f.service.Create(ctx, f.owner, "create-key", Input{"member-12345", true})
	if err != nil || app.ID != original || f.creates.Load() != 2 {
		t.Fatal("created another request", err)
	}
	app, err = f.service.Cancel(ctx, f.owner, app.ID)
	if err != nil || app.State != "CANCELLED" || f.fetches.Load() != 0 {
		t.Fatal("cancel did not fence fetch", app, err)
	}
	if _, err := f.service.Retrieve(ctx, f.owner, app.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("cancelled fetch accepted", err)
	}
	for _, status := range []string{"REJECTED", "EXPIRED", "WITHDRAWN", "DENIED", "DELIVERY_UNKNOWN"} {
		next, err := f.service.Create(ctx, f.owner, "case-"+status, Input{"member-12345", true})
		if err != nil {
			t.Fatal(err)
		}
		f.status.Store(status)
		next, err = f.service.Refresh(ctx, f.owner, next.ID)
		if err != nil || next.State != productState(status) {
			t.Fatal("terminal status lost", next, err)
		}
	}
}
func TestProductFetchFailureAndSQLCheckpointNeverRetry(t *testing.T) {
	for _, scenario := range []string{"source failure", "SQL checkpoint", "abandoned process"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			app := f.ready(t)
			switch scenario {
			case "source failure":
				f.fetchOffline.Store(true)
			case "SQL checkpoint":
				if _, err := f.pool.Exec(ctx, `CREATE FUNCTION fail_product_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='COMPLETE' THEN RAISE EXCEPTION 'test fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_product_finish BEFORE UPDATE ON sakhi_applications FOR EACH ROW EXECUTE FUNCTION fail_product_finish()`); err != nil {
					t.Fatal(err)
				}
			case "abandoned process":
				if err := f.service.store.claim(ctx, f.owner, app.ID, "FETCHING"); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "abandoned process" {
				_, _ = f.service.Retrieve(ctx, f.owner, app.ID)
			}
			if _, err := f.service.Retrieve(ctx, f.owner, app.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("failed/uncertain request retried", err)
			}
			if f.fetches.Load() > 1 {
				t.Fatal("repeated source attempt")
			}
			// Advance DB time relative to the immutable deadline without changing it.
			if _, err := f.pool.Exec(ctx, `ALTER TABLE sakhi_applications DISABLE TRIGGER immutable_sakhi_application; UPDATE sakhi_applications SET fetch_deadline=clock_timestamp()-interval '1 second' WHERE state='FETCHING'; ALTER TABLE sakhi_applications ENABLE TRIGGER immutable_sakhi_application`); err != nil {
				t.Fatal(err)
			}
			if err := f.service.store.Maintain(ctx); err != nil {
				t.Fatal(err)
			}
			loaded, err := f.service.Get(ctx, f.owner, app.ID)
			if err != nil || loaded.State != "UNKNOWN" || loaded.Result != nil {
				t.Fatal("uncertainty not retained", loaded, err)
			}
		})
	}
}
func TestProductHTTPBrowserOwnershipCSRFAndStrictInput(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	app := f.ready(t)
	handler, err := Handler(f.service, "http://127.0.0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body, cookie, origin, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "http-key")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "sakhi_demo", Value: cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		token, origin, csrf string
		want                int
	}{
		{"", "http://127.0.0.1", f.csrf, 401},
		{f.token, "https://attacker.invalid", f.csrf, 403},
		{f.token, "http://127.0.0.1", "wrong", 403},
	} {
		w := call("POST", "/api/applications/"+app.ID+"/retrieve", "", tc.token, tc.origin, tc.csrf)
		if w.Code != tc.want || f.fetches.Load() != 0 {
			t.Fatal("browser boundary bypassed", w.Code, tc.want)
		}
	}
	token, _, err := f.service.store.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/applications/"+app.ID, "", token, "", ""); w.Code != 404 {
		t.Fatal("cross-browser metadata exposed", w.Code)
	}
	for _, body := range []string{`{"member_reference":"member-12345","include_savings":true,"partial_refusal":true}`, `{"member_reference":"member-12345","include_savings":true,"include_savings":false}`, `{"Member_Reference":"member-12345","include_savings":true}`, `{"member_reference":"member-12345"}`} {
		if w := call("POST", "/api/applications", body, f.token, "http://127.0.0.1", f.csrf); w.Code != 400 {
			t.Fatal("ambiguous input accepted", w.Code)
		}
	}
	w := call("GET", "/api/applications/"+app.ID, "", f.token, "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "member-12345") || strings.Contains(w.Body.String(), "signed-grant") {
		t.Fatal("unsafe product metadata")
	}
	if w := call("POST", "/api/applications/"+app.ID+"/retrieve", "", f.token, "http://127.0.0.1", f.csrf); w.Code != 200 || !strings.Contains(w.Body.String(), "12500.75") {
		t.Fatal("browser result failed", w.Code, w.Body.String())
	}
}

func TestProductRetentionExpiryIsEnforcedBeforeCleanupAndBounded(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	store := f.service.store
	token, _, err := store.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owners := []string{f.owner, ownerOf(token)}
	var first record
	for _, owner := range owners {
		for n := range 20 {
			row, err := store.create(ctx, owner, "retention-"+string(rune('A'+n)), Input{"member-12345", true})
			if err != nil {
				t.Fatal(err)
			}
			if first.ID == "" {
				first = row
			}
		}
	}
	if _, err := store.create(ctx, f.owner, "over-limit", Input{"member-12345", true}); !errors.Is(err, ErrConflict) {
		t.Fatal("browser application limit not enforced", err)
	}
	// Operator-only fault moves the SQL clock boundary for an hour-long TTL test.
	if _, err := f.pool.Exec(ctx, `ALTER TABLE sakhi_applications DISABLE TRIGGER immutable_sakhi_application; UPDATE sakhi_applications SET expires_at=clock_timestamp()-interval '1 second'; ALTER TABLE sakhi_applications ENABLE TRIGGER immutable_sakhi_application`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Get(ctx, f.owner, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired product record exposed", err)
	}
	if _, err := store.create(ctx, f.owner, "retention-A", Input{"member-12345", true}); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired idempotency record reconnected", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE sakhi_sessions SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Session(ctx, f.token); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired browser session accepted", err)
	}
	if err := store.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM sakhi_applications`).Scan(&count); err != nil || count != 8 {
		t.Fatal("cleanup not bounded to 32", count, err)
	}
	if err := store.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM sakhi_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired sessions retained after application cleanup", count, err)
	}
	if f.creates.Load() != 0 || f.fetches.Load() != 0 {
		t.Fatal("maintenance called the platform")
	}
}
func TestProductSessionCookieAndStorageAAD(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	handler, err := Handler(f.service, "https://credit.example", false)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "https://credit.example/api/session", nil))
	cookies := response.Result().Cookies()
	if response.Code != 200 || len(cookies) != 1 || cookies[0].Name != "__Host-sakhi" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].Domain != "" || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe product session cookie")
	}
	row, err := f.service.store.create(ctx, f.owner, "aad-key", Input{"member-12345", true})
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []struct{ id, owner, kind string }{{row.ID + "other", row.owner, "input"}, {row.ID, "other-owner", "input"}, {row.ID, row.owner, "result"}} {
		if _, err := f.service.store.open(binding.id, binding.owner, binding.kind, row.inputCipher); err == nil {
			t.Fatal("encrypted input moved across an authority boundary")
		}
	}
}

func TestProductPolicyDenialNeverFetches(t *testing.T) {
	for _, phase := range []string{"create", "grant denied", "grant expired"} {
		t.Run(phase, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if phase == "create" {
				f.createCode.Store(403)
				app, err := f.service.Create(ctx, f.owner, "create-key", Input{"member-12345", true})
				if err == nil || app.State != "BLOCKED" || f.fetches.Load() != 0 {
					t.Fatal("creation denial did not close product flow", app.State, err)
				}
				if again, err := f.service.Create(ctx, f.owner, "create-key", Input{"member-12345", true}); err != nil || again.State != "BLOCKED" || f.creates.Load() != 1 {
					t.Fatal("denied request was resubmitted")
				}
				return
			}
			app := f.ready(t)
			want := "BLOCKED"
			f.grantCode.Store(403)
			if phase == "grant expired" {
				f.grantCode.Store(410)
				want = "EXPIRED"
			}
			app, err := f.service.Retrieve(ctx, f.owner, app.ID)
			if err == nil || app.State != want || f.fetches.Load() != 0 {
				t.Fatal("denied grant reached source", app.State, err)
			}
			if _, err := f.service.Retrieve(ctx, f.owner, app.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("denied product flow revived", err)
			}
		})
	}
}
func TestProductDatabaseOutageDoesNotReplaceBrowserOwnership(t *testing.T) {
	f := setup(t)
	handler, err := Handler(f.service, "http://127.0.0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	f.pool.Close()
	for _, path := range []string{"/api/session", "/api/applications/unknown"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "sakhi_demo", Value: f.token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 503 || len(w.Result().Cookies()) != 0 {
			t.Fatal("database outage invalidated/replaced browser ownership", w.Code)
		}
	}
}

func TestCancellationOutageReportsItsCommittedFence(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	app := f.ready(t)
	f.statusCode.Store(503)
	pending, err := f.service.Cancel(ctx, f.owner, app.ID)
	if err == nil || pending.State != "CANCELLING" {
		t.Fatal("outage returned stale unfenced application", pending.State, err)
	}
	if _, err := f.service.Retrieve(ctx, f.owner, app.ID); !errors.Is(err, ErrConflict) || f.fetches.Load() != 0 {
		t.Fatal("cancellation outage reopened a fetch", err)
	}
	f.statusCode.Store(0)
	cancelled, err := f.service.Cancel(ctx, f.owner, app.ID)
	if err != nil || cancelled.State != "CANCELLED" {
		t.Fatal("durable cancellation did not recover", cancelled.State, err)
	}
}
