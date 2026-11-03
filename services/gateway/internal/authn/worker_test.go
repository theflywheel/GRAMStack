package authn

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type workerCheckpoint struct {
	Pending, Consent, Digest, NoticeDigest, Withdrawn, Rejected string
	JWS                                                         string
	Version                                                     int64
}

func loadWorkerCheckpoint(t *testing.T) workerCheckpoint {
	t.Helper()
	if os.Getenv("GRAMSTACK_TEST_WORKERS") != "1" {
		t.Skip("requires running gateway with durable workers")
	}
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_TEST_WORKER_RESUME"))
	var value workerCheckpoint
	if err != nil || json.Unmarshal(raw, &value) != nil || value.Pending == "" {
		t.Fatal("missing private worker checkpoint", err)
	}
	return value
}
func awaitRequestState(t *testing.T, base, token, id, want string) int64 {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequest("GET", base+"/v1/access-requests/"+id, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, body := liveResponse(t, client, request)
		var value struct {
			Status  string `json:"status"`
			Version int64  `json:"version"`
		}
		if response.StatusCode != 200 || json.Unmarshal([]byte(body), &value) != nil {
			t.Fatal("consumer status unavailable", response.StatusCode)
		}
		if value.Status == want {
			return value.Version
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("worker did not reconcile request to " + want)
	return 0
}
func TestActualConsentWorkers(t *testing.T) {
	checkpoint := loadWorkerCheckpoint(t)
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	token := liveToken(t)
	pool, _ := database(t)
	ctx := context.Background()
	checkpoint.Version = awaitRequestState(t, base, token, checkpoint.Pending, "CONSENTED")
	awaitRequestState(t, base, token, checkpoint.Withdrawn, "WITHDRAWN")
	awaitRequestState(t, base, token, checkpoint.Rejected, "REJECTED")
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consent_artefacts a JOIN consent_state c USING(consent_id) WHERE c.request_id=ANY($1)`, []string{checkpoint.Withdrawn, checkpoint.Rejected}).Scan(&count); err != nil || count != 0 {
		t.Fatal("delayed work signed a stopped request", err)
	}
	if err := pool.QueryRow(ctx, `SELECT jws FROM consent_artefacts WHERE consent_id=$1`, checkpoint.Consent).Scan(&checkpoint.JWS); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_CONSENT_SIGNING_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("invalid local consent test key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("not a consent signing key")
	}
	verifier, err := cm.NewVerifierWithOptions(base, base, map[string]*ecdsa.PublicKey{"demo-consent-key": &key.PublicKey}, cm.VerifierOptions{AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	artefact, _, err := verifier.Verify(checkpoint.JWS, time.Now())
	if err != nil || artefact.RequestDigest != checkpoint.Digest || artefact.NoticeDigest != checkpoint.NoticeDigest || artefact.Consumer != "consumer-one" {
		t.Fatal("worker signed unbound or invalid artefact", err)
	}
	// A running worker activates the normal HTTP flow, then honors subject withdrawal.
	id, browser, values, _ := reviewRequest(t, base, token)
	values.Set("decision", "approve")
	values.Set("confirmation", "yes")
	if response, _ := liveResponse(t, browser, decisionRequest(base, id, values)); response.StatusCode != 200 {
		t.Fatal("running-worker approval failed")
	}
	awaitRequestState(t, base, token, id, "CONSENTED")
	record := readRecord(t, browser, base, id)
	if record.Consent.Status != "ACTIVE" || record.SignedArtefact == "" {
		t.Fatal("receipt hid activation")
	}
	form := url.Values{"csrf": {values.Get("csrf")}}
	request, _ := http.NewRequest("POST", base+"/consents/"+record.Consent.ConsentID+"/withdrawal", strings.NewReader(form.Encode()))
	request.Header.Set("Origin", base)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if response, _ := liveResponse(t, browser, request); response.StatusCode != 200 {
		t.Fatal("active withdrawal failed")
	}
	awaitRequestState(t, base, token, id, "WITHDRAWN")
	stopped := readRecord(t, browser, base, id)
	if stopped.SignedArtefact != record.SignedArtefact || stopped.Consent.Status != "WITHDRAWN" {
		t.Fatal("withdrawal mutated evidence or stayed active")
	}
	// A short test-injected SQL deadline exercises the actual maintenance loop.
	expireID, expireBrowser, expireValues, _ := reviewRequest(t, base, token)
	if _, err = pool.Exec(ctx, `UPDATE consent_state SET valid_until=clock_timestamp()+interval '0.1 second' WHERE request_id=$1`, expireID); err != nil {
		t.Fatal(err)
	}
	awaitRequestState(t, base, token, expireID, "EXPIRED")
	expired := readRecord(t, expireBrowser, base, expireID)
	if expired.Consent.Status != "EXPIRED" || expired.Decision != nil || expired.SignedArtefact != "" {
		t.Fatal("expiry invented a decision or signed authority")
	}
	expireValues.Set("decision", "approve")
	expireValues.Set("confirmation", "yes")
	if response, _ := liveResponse(t, expireBrowser, decisionRequest(base, expireID, expireValues)); response.StatusCode != 410 {
		t.Fatal("late decision bypassed expiry", response.StatusCode)
	}
	raw, err = json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("GRAMSTACK_TEST_WORKER_RESUME"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: pending approval survived gateway/adapter/source restart, workers activated one verified artefact, stopped notifications stayed terminal, HTTP active withdrawal reconciled")
}
func TestActualConsentWorkersRestart(t *testing.T) {
	checkpoint := loadWorkerCheckpoint(t)
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	token := liveToken(t)
	pool, _ := database(t)
	var stored string
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT jws FROM consent_artefacts WHERE consent_id=$1`, checkpoint.Consent).Scan(&stored); err != nil || stored != checkpoint.JWS {
		t.Fatal("gateway restart changed signed consent", err)
	}
	if got := awaitRequestState(t, base, token, checkpoint.Pending, "CONSENTED"); got != checkpoint.Version {
		t.Fatal("restart duplicated workflow authority")
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM consent_artefacts WHERE consent_id=$1`, checkpoint.Consent).Scan(&count); err != nil || count != 1 {
		t.Fatal("restart duplicated artefact", err)
	}
	t.Log("PASS: restarted gateway workers preserved immutable signed bytes and request version without duplicated activation")
}
