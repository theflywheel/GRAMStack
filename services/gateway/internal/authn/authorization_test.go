package authn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
)

type authorizationCheckpoint struct {
	Request, Consent, Digest string
	Grant                    az.GrantResponse
	Redemption               az.Redemption
	Reservation              az.Reservation
}

func requireAuthorization(t *testing.T) {
	t.Helper()
	if os.Getenv("GRAMSTACK_TEST_AUTHZ") != "1" {
		t.Skip("requires actual configured authorization listener")
	}
}
func grantRequest(base, id, token, key, body string) *http.Request {
	r, _ := http.NewRequest("POST", base+"/v1/access-requests/"+id+"/grant", strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Idempotency-Key", key)
	return r
}
func liveGrant(t *testing.T, base, id, token, key string) az.GrantResponse {
	t.Helper()
	res, body := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, grantRequest(base, id, token, key, ""))
	var g az.GrantResponse
	if res.StatusCode != 200 || json.Unmarshal([]byte(body), &g) != nil || g.Grant == "" || g.TokenType != "Bearer" {
		t.Fatal("grant endpoint failed", res.StatusCode)
	}
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Gramstack-Contract-Version") != "0.3.0" {
		t.Fatal("grant headers missing")
	}
	return g
}
func redeemRequest(origin string, v az.Redemption) *http.Request {
	body, _ := json.Marshal(v)
	r, _ := http.NewRequest("POST", origin+"/internal/v1/grants/redeem", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
func liveRedemption(t *testing.T, v az.Redemption) az.Reservation {
	t.Helper()
	res, body := liveResponse(t, serviceClient(t, "lokos-adapter", "gateway.gramstack.internal"), redeemRequest(os.Getenv("GRAMSTACK_TEST_AUTHZ_URL"), v))
	var out az.Reservation
	if res.StatusCode != 200 || json.Unmarshal([]byte(body), &out) != nil || out.ID == "" {
		t.Fatal("private redemption failed", res.StatusCode)
	}
	return out
}
func approvedAuthorization(t *testing.T, base, token string) (string, string, string) {
	t.Helper()
	id, browser, values, _ := reviewRequest(t, base, token)
	values.Set("decision", "approve")
	values.Set("confirmation", "yes")
	if res, _ := liveResponse(t, browser, decisionRequest(base, id, values)); res.StatusCode != 200 {
		t.Fatal("approval failed")
	}
	awaitRequestState(t, base, token, id, "CONSENTED")
	record := readRecord(t, browser, base, id)
	return id, record.Consent.ConsentID, record.Notice.RequestDigest
}
func TestActualAuthorization(t *testing.T) {
	requireAuthorization(t)
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	private := os.Getenv("GRAMSTACK_TEST_AUTHZ_URL")
	token := liveToken(t)
	pending := liveRequestMember(t, base, token, "member-12345")
	for _, tc := range []struct {
		token, body string
		status      int
	}{{"", "", 401}, {"invalid", "", 401}, {token, "{}", 400}, {token, "", 409}} {
		res, body := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, grantRequest(base, pending, tc.token, "key", tc.body))
		if res.StatusCode != tc.status || strings.Contains(body, "member-12345") {
			t.Fatal("unsafe grant request", res.StatusCode, tc.status)
		}
	}
	id, consent, digest := approvedAuthorization(t, base, token)
	g := liveGrant(t, base, id, token, "first")
	if liveGrant(t, base, id, token, "second").Grant != g.Grant {
		t.Fatal("issuance retry changed signed grant")
	}
	req, _ := http.NewRequest("GET", base+"/.well-known/gramstack-jwks.json", nil)
	res, body := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, req)
	var keys jose.JSONWebKeySet
	if res.StatusCode != 200 || json.Unmarshal([]byte(body), &keys) != nil || len(keys.Keys) != 1 || !keys.Keys[0].IsPublic() {
		t.Fatal("public grant key discovery invalid")
	}
	parsed, err := jose.ParseSigned(g.Grant, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal("invalid compact grant")
	}
	payload, err := parsed.Verify(keys.Keys[0].Key)
	if err != nil {
		t.Fatal("grant signature did not match discovered pinned key")
	}
	var claims az.Grant
	if json.Unmarshal(payload, &claims) != nil || claims.RequestID != id || claims.Digest != digest || claims.Audience != "lokos-adapter" || claims.Consumer != "consumer-one" || (len(claims.Fields) != 1 || claims.Fields[0] != "membership_status") || claims.ExpiresAt-claims.IssuedAt > 60 || claims.Subject == "demo-sunita" || strings.Contains(string(payload), "member-12345") {
		t.Fatal("grant scope leaked or changed")
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GRAMSTACK_TEST_CERTS"), "consumer-one.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("invalid test presenter certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(cert.Raw)
	thumb := base64.RawURLEncoding.EncodeToString(sum[:])
	if claims.Confirmation.Thumbprint != thumb {
		t.Fatal("grant not bound to registered presenter certificate")
	}
	input := az.Redemption{AttemptID: "live-attempt", Grant: g.Grant, RequestID: id, Digest: digest, Presenter: az.Presenter{Consumer: "consumer-one", Thumbprint: thumb}}
	// No public route, anonymous TLS, or a different CA-approved identity may redeem.
	if res, _ := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, redeemRequest(base, input)); res.StatusCode != 404 {
		t.Fatal("private redemption exposed publicly", res.StatusCode)
	}
	anon := serviceClient(t, "", "gateway.gramstack.internal")
	if r, err := anon.Do(redeemRequest(private, input)); err == nil {
		r.Body.Close()
		t.Fatal("redemption accepted without client certificate")
	}
	for _, identity := range []string{"gateway", "consumer-one", "unauthorised"} {
		req := redeemRequest(private, input)
		req.Header.Set("X-Forwarded-Client-Cert", "lokos-adapter.gramstack.internal")
		req.Header.Set("X-Gramstack-Adapter", "lokos-adapter")
		if res, _ := liveResponse(t, serviceClient(t, identity, "gateway.gramstack.internal"), req); res.StatusCode != 403 {
			t.Fatal("unregistered TLS peer redeemed", identity, res.StatusCode)
		}
	}
	adapter := serviceClient(t, "lokos-adapter", "gateway.gramstack.internal")
	bad := input
	bad.Presenter.Thumbprint = strings.Repeat("b", 43)
	if res, _ := liveResponse(t, adapter, redeemRequest(private, bad)); res.StatusCode != 403 {
		t.Fatal("sender substitution accepted", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", private+"/internal/v1/grants/redeem", strings.NewReader(`{"attempt_id":"one","attempt_id":"two"}`))
	req.Header.Set("Content-Type", "application/json")
	if res, _ := liveResponse(t, adapter, req); res.StatusCode != 400 {
		t.Fatal("duplicate JSON accepted", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", private+"/internal/consents/"+consent+"/status", nil)
	if res, body := liveResponse(t, adapter, req); res.StatusCode != 200 || !strings.Contains(body, `"ACTIVE"`) {
		t.Fatal("private current consent status unavailable", res.StatusCode)
	}
	first := liveRedemption(t, input)
	if !first.First {
		t.Fatal("new attempt not admitted")
	}
	retry := liveRedemption(t, input)
	if retry.First || retry.ID != first.ID {
		t.Fatal("retry acquired new authority")
	}
	bad = input
	bad.AttemptID = "new-attempt"
	if res, _ := liveResponse(t, adapter, redeemRequest(private, bad)); res.StatusCode != 409 {
		t.Fatal("consumed grant replay admitted", res.StatusCode)
	}
	awaitRequestState(t, base, token, id, "REDEEMING")
	// Keep a second unconsumed grant to prove stable issuance across process restart.
	next, nextConsent, nextDigest := approvedAuthorization(t, base, token)
	if res, _ := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, grantRequest(base, next, token, "first", "")); res.StatusCode != 409 {
		t.Fatal("idempotency key crossed requests", res.StatusCode)
	}
	saved := authorizationCheckpoint{Request: next, Consent: nextConsent, Digest: nextDigest, Grant: liveGrant(t, base, next, token, "restart-key"), Redemption: input, Reservation: first}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("GRAMSTACK_TEST_AUTHZ_RESUME"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: real Hydra consumer -> verified subject/active consent -> signed certificate-bound grant -> actual mTLS adapter admission; anonymous/wrong peers, sender substitution, malformed JSON and replay denied")
}
func TestActualAuthorizationRestart(t *testing.T) {
	requireAuthorization(t)
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_TEST_AUTHZ_RESUME"))
	var saved authorizationCheckpoint
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.Request == "" {
		t.Fatal("missing private authorization checkpoint", err)
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	token := liveToken(t)
	if got := liveGrant(t, base, saved.Request, token, "restart-retry"); got.Grant != saved.Grant.Grant {
		t.Fatal("restart changed durable grant")
	}
	retry := liveRedemption(t, saved.Redemption)
	if retry.First || retry.ID != saved.Reservation.ID {
		t.Fatal("restart duplicated admission")
	}
	pool, _ := database(t)
	var count int
	if err = pool.QueryRow(context.Background(), `SELECT count(*) FROM disclosure_reservations WHERE consent_id=$1`, saved.Consent).Scan(&count); err != nil || count != 0 {
		t.Fatal("issuance or retry reserved another disclosure", err)
	}
	t.Log("PASS: gateway restart preserved exact grant bytes and metadata-only redemption acknowledgement without another reservation")
}
