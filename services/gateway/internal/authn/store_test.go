package authn

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/migrations"
)

func database(t *testing.T) (*pgxpool.Pool, []byte) {
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
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return pool, key
}
func TestEncryptedOneTimeStateAndRestart(t *testing.T) {
	pool, key := database(t)
	store, err := NewStore(pool, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	state, binding := rand.Text(), rand.Text()
	payload := map[string]string{"subject": "private-upstream-subject", "nonce": "private-nonce"}
	until := time.Now().Add(time.Minute)
	if err = store.Put(ctx, "test", state, binding, payload, until); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, "SELECT payload FROM browser_state WHERE kind='test' AND key_digest=$1", cm.Digest([]byte(state))).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-upstream-subject") || strings.Contains(string(raw), "private-nonce") {
		t.Fatal("authentication payload not encrypted")
	}
	var found map[string]string
	if err = store.Take(ctx, "test", state, "wrong-browser", &found); !errors.Is(err, ErrAuthentication) {
		t.Fatal("wrong browser consumed transaction")
	}
	other, err := pgxpool.New(ctx, os.Getenv("GRAMSTACK_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	restarted, err := NewStore(other, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Peek(ctx, "test", state, binding, &found); err != nil || found["subject"] != payload["subject"] {
		t.Fatalf("restart state %v", err)
	}
	wrongKey := make([]byte, 32)
	wrong, _ := NewStore(other, wrongKey)
	if err = wrong.Peek(ctx, "test", state, binding, &found); !errors.Is(err, ErrAuthentication) {
		t.Fatal("wrong key decrypted payload")
	}
	const n = 12
	results := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var value map[string]string
			results <- restarted.Take(ctx, "test", state, binding, &value)
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrAuthentication) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("transaction consumed %d times", success)
	}
	if err = restarted.Replace(ctx, "test", state, binding, payload, until); !errors.Is(err, ErrAuthentication) {
		t.Fatal("consumed transaction resurrected")
	}
}
func TestStateExpiryTamperAndSessionBinding(t *testing.T) {
	pool, key := database(t)
	store, _ := NewStore(pool, key)
	ctx := context.Background()
	state, binding := rand.Text(), rand.Text()
	until := time.Now().Add(time.Minute)
	if err := store.Put(ctx, "expiry", state, binding, map[string]string{"value": "secret"}, until); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE browser_state SET expires_at=clock_timestamp()-interval '1 second' WHERE kind='expiry' AND key_digest=$1", cm.Digest([]byte(state))); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := store.Take(ctx, "expiry", state, binding, &value); !errors.Is(err, ErrAuthentication) {
		t.Fatal("expired state accepted")
	}
	if err := store.Put(ctx, "tamper", state, binding, map[string]string{"value": "secret"}, until); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE browser_state SET expires_at=expires_at+interval '1 minute' WHERE kind='tamper' AND key_digest=$1", cm.Digest([]byte(state))); err != nil {
		t.Fatal(err)
	}
	if err := store.Peek(ctx, "tamper", state, binding, &value); !errors.Is(err, ErrAuthentication) {
		t.Fatal("modified expiry accepted")
	}
	s := &Service{store: store, origin: "https://gateway.example", secure: true}
	sessionID := rand.Text()
	session := Session{RequestID: "request-one", PrincipalRef: "principal-one", CSRF: rand.Text(), ExpiresAt: until}
	if err := store.Put(ctx, "auth.session", sessionID, sessionID, session, until); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "https://gateway.example/interactions/request-one", nil)
	req.AddCookie(&http.Cookie{Name: s.cookieName("session"), Value: sessionID})
	if got, err := s.Session(req, "request-one"); err != nil || got.PrincipalRef != "principal-one" {
		t.Fatal(err)
	}
	if _, err := s.Session(req, "request-two"); !errors.Is(err, ErrAuthentication) {
		t.Fatal("session crossed request boundary")
	}
	req.Header.Set("Origin", s.origin)
	if err := s.VerifyCSRF(req, session, session.CSRF); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://attacker.example")
	if s.VerifyCSRF(req, session, session.CSRF) == nil {
		t.Fatal("cross-origin mutation accepted")
	}
	req.Header.Set("Origin", s.origin)
	if s.VerifyCSRF(req, session, "wrong-token") == nil {
		t.Fatal("wrong CSRF accepted")
	}
	recorder := httptest.NewRecorder()
	s.setCookie(recorder, "session", sessionID, until)
	cookie := recorder.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || !strings.HasPrefix(cookie.Name, "__Host-") {
		t.Fatal("unsafe session cookie")
	}
	var parsed Session
	raw, _ := json.Marshal(session)
	if json.Unmarshal(raw, &parsed) != nil {
		t.Fatal("invalid session encoding")
	}
}
func TestTrustedRedirects(t *testing.T) {
	for _, raw := range []string{"https://attacker.example/", "https://user:pass@hydra.example/", "javascript:alert(1)", "https://hydra.example/#token"} {
		if sameOrigin(raw, "https://hydra.example/") {
			t.Fatalf("untrusted redirect %s", raw)
		}
	}
	if !sameOrigin("https://hydra.example/oauth2/auth?login_verifier=opaque", "https://hydra.example/") {
		t.Fatal("valid Hydra redirect rejected")
	}
	if trustedURL("http://127.0.0.1:4444", false) || trustedURL("http://localhost:4444", true) || trustedURL("http://10.0.0.1", true) {
		t.Fatal("insecure endpoint accepted")
	}
}

func TestCallbackShape(t *testing.T) {
	state := rand.Text()
	valid := httptest.NewRequest("GET", "https://gateway.example/auth/callback?state="+state+"&code=opaque-code", nil)
	if _, _, err := callbackParameters(valid); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"state=" + state + "&state=other&code=code", "state=" + state + "&code=one&code=two", "state=" + state + "&code=", "state=" + state + "&code=one&error=access_denied", "state=short&code=one"} {
		if _, _, err := callbackParameters(httptest.NewRequest("GET", "https://gateway.example/auth/callback?"+query, nil)); err == nil {
			t.Fatal("ambiguous callback admitted")
		}
	}
}
