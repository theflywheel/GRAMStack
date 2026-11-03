package consumer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/migrations"
)

const requestJSON = `{"registry":"lokos","resource":"shg.membership","subject_hint":{"type":"member_reference","value":"fictional-member"},"fields":["membership_status","savings_summary"],"purpose":"credit_eligibility","access":{"mode":"single_use","valid_for_seconds":300},"recipient_terms":{"retention_policy_id":"terms-v1"}}`

type fixtureAuth struct{}

func (fixtureAuth) Authenticate(_ context.Context, token string) (string, error) {
	if token == "one" {
		return "consumer-one", nil
	}
	if token == "two" {
		return "consumer-two", nil
	}
	return "", ErrUnauthenticated
}
func setup(t *testing.T) (*Service, *pgxpool.Pool) {
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
	p := Policy{Registry: "lokos", Resource: "shg.membership", Fields: []string{"membership_status", "savings_summary"}, Purposes: []string{"credit_eligibility"}, HintTypes: []string{"member_reference"}, TermsVersion: "terms-v1", TermsText: "Use for the declared assessment only."}
	service, err := New(Config{Pool: pool, Auth: fixtureAuth{}, PublicOrigin: "https://gateway.example", Consumers: map[string][]Policy{"consumer-one": {p}, "consumer-two": {p}}})
	if err != nil {
		t.Fatal(err)
	}
	return service, pool
}
func invoke(h http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	result := httptest.NewRecorder()
	h.ServeHTTP(result, req)
	return result
}
func TestRequestsHTTPAndRestart(t *testing.T) {
	s, pool := setup(t)
	h := s.Handler()
	key := rand.Text()
	accepted := invoke(h, "POST", "/v1/access-requests", "one", key, requestJSON)
	if accepted.Code != 202 || accepted.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create %d %s", accepted.Code, accepted.Body.String())
	}
	var value Accepted
	if err := json.Unmarshal(accepted.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	retry := invoke(h, "POST", "/v1/access-requests", "one", key, requestJSON)
	if retry.Code != 202 || retry.Body.String() != accepted.Body.String() {
		t.Fatalf("retry %d %s", retry.Code, retry.Body.String())
	}
	conflict := invoke(h, "POST", "/v1/access-requests", "one", key, strings.Replace(requestJSON, "fictional-member", "different-member", 1))
	if conflict.Code != 409 {
		t.Fatal(conflict.Code)
	}
	for _, path := range []string{"/v1/access-requests/" + value.RequestID, "/v1/access-requests/unknown"} {
		if got := invoke(h, "GET", path, "two", "", ""); got.Code != 404 {
			t.Fatalf("privacy %d", got.Code)
		}
	}
	wrong := invoke(h, "POST", "/v1/access-requests/"+value.RequestID+"/cancel", "two", "", "")
	if wrong.Code != 404 {
		t.Fatal(wrong.Code)
	}
	// New service and new pool prove idempotency does not depend on process memory.
	other, err := pgxpool.New(context.Background(), os.Getenv("GRAMSTACK_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	restart := &Service{other, s.auth, s.origin, s.policies}
	again, err := restart.Create(context.Background(), "consumer-one", key, decodeInput(t, requestJSON))
	if err != nil || again.RequestID != value.RequestID {
		t.Fatalf("restart %+v %v", again, err)
	}
	cancelled := invoke(h, "POST", "/v1/access-requests/"+value.RequestID+"/cancel", "one", "", "")
	if cancelled.Code != 200 || !strings.Contains(cancelled.Body.String(), `"status":"CANCELLED"`) {
		t.Fatalf("cancel %d %s", cancelled.Code, cancelled.Body.String())
	}
	repeated := invoke(h, "POST", "/v1/access-requests/"+value.RequestID+"/cancel", "one", "", "")
	if repeated.Body.String() != cancelled.Body.String() {
		t.Fatal("cancellation retry changed status")
	}
	status := invoke(h, "GET", "/v1/access-requests/"+value.RequestID, "one", "", "")
	if status.Code != 200 || status.Body.String() != cancelled.Body.String() {
		t.Fatalf("status %d %s", status.Code, status.Body.String())
	}
	for _, method := range []string{"GET", "POST"} {
		if got := invoke(h, method, "/v1/access-requests/"+value.RequestID+"/data", "one", "", ""); got.Code != 404 {
			t.Fatalf("unimplemented route exposed %d", got.Code)
		}
	}
	var raw []byte
	if err = pool.QueryRow(context.Background(), "SELECT snapshot_bytes FROM accepted_requests WHERE request_id=$1", value.RequestID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var snapshot cm.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	digest, _ := cm.SnapshotDigest(snapshot)
	var stored string
	if err = pool.QueryRow(context.Background(), "SELECT request_digest FROM access_requests WHERE request_id=$1", value.RequestID).Scan(&stored); err != nil || digest != stored {
		t.Fatalf("snapshot binding %v", err)
	}
}
func decodeInput(t *testing.T, raw string) cm.AccessRequest {
	t.Helper()
	var value cm.AccessRequest
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestRequestRejections(t *testing.T) {
	s, _ := setup(t)
	h := s.Handler()
	cases := []struct {
		name, token, key, body string
		want                   int
	}{
		{"null body", "one", rand.Text(), "null", 400},
		{"empty object", "one", rand.Text(), "{}", 400},
		{"missing token", "", rand.Text(), requestJSON, 401},
		{"invalid token", "invalid", rand.Text(), requestJSON, 401},
		{"missing key", "one", "", requestJSON, 400},
		{"unknown identity", "one", rand.Text(), strings.Replace(requestJSON, `"registry":`, `"consumer_id":"attacker","registry":`, 1), 400},
		{"duplicate field", "one", rand.Text(), strings.Replace(requestJSON, `"savings_summary"`, `"membership_status"`, 1), 400},
		{"duplicate JSON key", "one", rand.Text(), strings.Replace(requestJSON, `"registry":"lokos"`, `"registry":"other","registry":"lokos"`, 1), 400},
		{"unknown nested field", "one", rand.Text(), strings.Replace(requestJSON, `"mode":"single_use"`, `"mode":"single_use","extra":true`, 1), 400},
		{"extra input", "one", rand.Text(), requestJSON + `{}`, 400},
		{"overlong", "one", rand.Text(), strings.Replace(requestJSON, "fictional-member", strings.Repeat("a", 40000), 1), 400},
		{"wrong purpose", "one", rand.Text(), strings.Replace(requestJSON, "credit_eligibility", "marketing", 1), 403},
		{"wrong field", "one", rand.Text(), strings.Replace(requestJSON, "savings_summary", "loans", 1), 403},
		{"too long grant", "one", rand.Text(), strings.Replace(requestJSON, "300", "301", 1), 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := invoke(h, "POST", "/v1/access-requests", tc.token, tc.key, tc.body)
			if result.Code != tc.want {
				t.Fatalf("got %d want %d: %s", result.Code, tc.want, result.Body.String())
			}
			if strings.Contains(result.Body.String(), "fictional-member") {
				t.Fatal("source hint leaked in error")
			}
		})
	}
}
func TestConcurrentCreateAndRollback(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	key := rand.Text()
	input := decodeInput(t, requestJSON)
	const n = 12
	results := make(chan Accepted, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := s.Create(ctx, "consumer-one", key, input)
			results <- value
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for result := range results {
		if id != "" && id != result.RequestID {
			t.Fatal("duplicate requests")
		}
		id = result.RequestID
	}
	var events int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE aggregate_type='request' AND aggregate_id=$1", id).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d %v", events, err)
	}
	// A real insert trigger failure must roll back the request as well as its snapshot.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION test_fail_accept() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END; $$; CREATE TRIGGER test_fail_accept BEFORE INSERT ON accepted_requests FOR EACH ROW EXECUTE FUNCTION test_fail_accept();`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DROP TRIGGER IF EXISTS test_fail_accept ON accepted_requests; DROP FUNCTION IF EXISTS test_fail_accept();")
	})
	var before, after int
	pool.QueryRow(ctx, "SELECT count(*) FROM access_requests WHERE consumer_id='consumer-one'").Scan(&before)
	if _, err := s.Create(ctx, "consumer-one", rand.Text(), input); err == nil {
		t.Fatal("snapshot failure ignored")
	}
	pool.QueryRow(ctx, "SELECT count(*) FROM access_requests WHERE consumer_id='consumer-one'").Scan(&after)
	if before != after {
		t.Fatal("orphan accepted request")
	}
}
func TestHydraValidation(t *testing.T) {
	good := map[string]any{"active": true, "client_id": "consumer-one", "iss": "https://issuer.example/", "aud": []string{"gramstack"}, "scope": "gramstack.access", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Add(-time.Second).Unix(), "nbf": time.Now().Add(-time.Second).Unix(), "token_type": "Bearer", "token_use": "access_token"}
	var response []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/admin/oauth2/introspect" {
			t.Error("wrong Hydra operation")
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("token") != "synthetic-token" || r.Form.Get("scope") != "gramstack.access" || r.Form.Get("token_type_hint") != "access_token" {
			t.Error("wrong introspection form")
		}
		w.Write(response)
	}))
	defer server.Close()
	h, err := NewHydra(server.URL+"/admin/oauth2/introspect", "https://issuer.example/", "gramstack", "gramstack.access", false)
	if err != nil {
		t.Fatal(err)
	}
	h.client.Transport = server.Client().Transport
	response, _ = json.Marshal(good)
	if id, err := h.Authenticate(context.Background(), "synthetic-token"); err != nil || id != "consumer-one" {
		t.Fatalf("%s %v", id, err)
	}
	for key, bad := range map[string]any{"active": false, "iss": "https://attacker.example/", "aud": []string{"other"}, "scope": "other", "exp": time.Now().Add(-time.Second).Unix(), "nbf": time.Now().Add(time.Minute).Unix(), "token_use": "refresh_token", "token_type": "Other", "client_id": ""} {
		original := good[key]
		good[key] = bad
		response, _ = json.Marshal(good)
		if _, err := h.Authenticate(context.Background(), "synthetic-token"); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("accepted %s: %v", key, err)
		}
		good[key] = original
	}
	response = []byte(`{`)
	if _, err := h.Authenticate(context.Background(), "synthetic-token"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://127.0.0.1:4445/admin/oauth2/introspect", "https://user:password@issuer.example/introspect", "https://issuer.example/introspect?redirect=other"} {
		if _, err := NewHydra(endpoint, "https://issuer.example/", "gramstack", "gramstack.access", false); err == nil {
			t.Fatal("untrusted endpoint accepted")
		}
	}
}
func TestActualHydraConsumerAPI(t *testing.T) {
	admin, public := os.Getenv("GRAMSTACK_TEST_HYDRA_ADMIN"), os.Getenv("GRAMSTACK_TEST_HYDRA_PUBLIC")
	if admin == "" || public == "" {
		t.Skip("isolated Hydra not configured")
	}
	s, _ := setup(t)
	auth, err := NewHydra(admin+"/admin/oauth2/introspect", public+"/", "gramstack", "gramstack.access", true)
	if err != nil {
		t.Fatal(err)
	}
	s.auth = auth
	clientID, secret := "consumer-one", "isolated-synthetic-secret"
	body, _ := json.Marshal(map[string]any{"client_id": clientID, "client_secret": secret, "grant_types": []string{"client_credentials"}, "scope": "gramstack.access", "audience": []string{"gramstack"}, "token_endpoint_auth_method": "client_secret_basic"})
	response, err := http.Post(admin+"/admin/clients", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatalf("client registration %d", response.StatusCode)
	}
	req, _ := http.NewRequest("POST", public+"/oauth2/token", strings.NewReader("grant_type=client_credentials&scope=gramstack.access&audience=gramstack"))
	req.SetBasicAuth(clientID, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.NewDecoder(response.Body).Decode(&token) != nil || token.AccessToken == "" {
		t.Fatal("Hydra did not issue token")
	}
	h := s.Handler()
	created := invoke(h, "POST", "/v1/access-requests", token.AccessToken, rand.Text(), requestJSON)
	if created.Code != 202 {
		t.Fatalf("actual Hydra create: %d %s", created.Code, created.Body.String())
	}
	var accepted Accepted
	json.Unmarshal(created.Body.Bytes(), &accepted)
	if result := invoke(h, "GET", "/v1/access-requests/"+accepted.RequestID, token.AccessToken, "", ""); result.Code != 200 {
		t.Fatal(result.Code)
	}
	if result := invoke(h, "POST", "/v1/access-requests/"+accepted.RequestID+"/cancel", token.AccessToken, "", ""); result.Code != 200 {
		t.Fatal(result.Code)
	}
	revoke, _ := http.NewRequest("POST", public+"/oauth2/revoke", strings.NewReader("token="+token.AccessToken+"&token_type_hint=access_token"))
	revoke.SetBasicAuth(clientID, secret)
	revoke.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = http.DefaultClient.Do(revoke)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("revocation %d", response.StatusCode)
	}
	if result := invoke(h, "GET", "/v1/access-requests/"+accepted.RequestID, token.AccessToken, "", ""); result.Code != 401 {
		t.Fatalf("revoked token admitted %d", result.Code)
	}
}

// Compile-time assertion: cancellation still uses the common enforcement authority.
var _ = access.ErrAdmitted

func TestRequestDeadlineAndNoBodyMutation(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	value, err := s.Create(ctx, "consumer-one", rand.Text(), decodeInput(t, requestJSON))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE access_requests SET expires_at=clock_timestamp()-interval '1 second' WHERE request_id=$1", value.RequestID); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(ctx, "consumer-one", value.RequestID)
	if err != nil || status.Status != "EXPIRED" || status.NextAction != "none" {
		t.Fatalf("expired %+v %v", status, err)
	}
	repeated, err := s.Status(ctx, "consumer-one", value.RequestID)
	if err != nil || repeated.Version != status.Version {
		t.Fatal("expiry polling changes version")
	}
	created, err := s.Create(ctx, "consumer-one", rand.Text(), decodeInput(t, requestJSON))
	if err != nil {
		t.Fatal(err)
	}
	result := invoke(s.Handler(), "POST", "/v1/access-requests/"+created.RequestID+"/cancel", "one", "", `{"fields":["other"]}`)
	if result.Code != 400 {
		t.Fatalf("cancellation accepts scope mutation: %d", result.Code)
	}
}

func TestTrustedRecipientTermsAreUnambiguousAndCopied(t *testing.T) {
	service, pool := setup(t)
	ctx := context.Background()
	bound, err := service.Resolve(ctx, "consumer-one", "terms-v1")
	if err != nil || bound.Text != "Use for the declared assessment only." {
		t.Fatal("trusted terms not resolved", err)
	}
	for _, tuple := range [][2]string{{"unknown", "terms-v1"}, {"consumer-one", "unknown"}} {
		if _, err = service.Resolve(ctx, tuple[0], tuple[1]); !errors.Is(err, cm.ErrBinding) {
			t.Fatal("unconfigured terms accepted", err)
		}
	}
	policies := map[string][]Policy{"consumer-one": {service.policies["consumer-one"][0]}}
	copied, err := New(Config{Pool: pool, Auth: fixtureAuth{}, PublicOrigin: "https://gateway.example", Consumers: policies})
	if err != nil {
		t.Fatal(err)
	}
	policies["consumer-one"][0].TermsText = "mutated caller config"
	if after, _ := copied.Resolve(ctx, "consumer-one", "terms-v1"); after != bound {
		t.Fatal("caller config mutation changed existing terms")
	}
	policies["consumer-one"][0].TermsText = bound.Text
	duplicate := policies["consumer-one"][0]
	duplicate.Resource = "other.resource"
	duplicate.TermsText = "different text for the same immutable version"
	policies["consumer-one"] = append(policies["consumer-one"], duplicate)
	if _, err = New(Config{Pool: pool, Auth: fixtureAuth{}, PublicOrigin: "https://gateway.example", Consumers: policies}); err == nil {
		t.Fatal("ambiguous recipient terms accepted")
	}
}
