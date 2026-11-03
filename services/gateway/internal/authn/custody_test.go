package authn

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
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
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/custody"
)

type custodyCheckpoint struct {
	RequestID, JWS string
	Ack            custody.Acknowledgement
	Version        int64
}

func custodyKey(t *testing.T, name string) *ecdsa.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GRAMSTACK_TEST_CERTS"), name+".key"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("missing private test key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("invalid test key")
	}
	return key
}
func custodyRequest(origin, compact string) *http.Request {
	raw, _ := json.Marshal(map[string]string{"jws": compact})
	r, _ := http.NewRequest("POST", origin+"/internal/v1/evidence", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Gramstack-Contract-Version", "0.3.0")
	return r
}
func custodyAck(t *testing.T, client *http.Client, origin, compact string) custody.Acknowledgement {
	t.Helper()
	response, body := liveResponse(t, client, custodyRequest(origin, compact))
	var ack custody.Acknowledgement
	if response.StatusCode != 201 || json.Unmarshal([]byte(body), &ack) != nil || ack.ReceiptID == "" || ack.Digest != cm.Digest([]byte(compact)) || ack.AnchorStatus != "pending" {
		t.Fatal("durable custody acknowledgement failed", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Gramstack-Contract-Version") != "0.3.0" {
		t.Fatal("custody response controls missing")
	}
	return ack
}
func liveEvidence(t *testing.T, key *ecdsa.PrivateKey, event custody.Event) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType(jose.ContentType(custody.EvidenceType)).WithHeader("kid", "demo-adapter-key"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cm.Canonical(event)
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
func TestActualEvidenceCustody(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_CUSTODY") != "1" {
		t.Skip("requires actual configured custody endpoint")
	}
	base := os.Getenv("GRAMSTACK_TEST_GATEWAY_URL")
	private := os.Getenv("GRAMSTACK_TEST_AUTHZ_URL")
	token := liveToken(t)
	id, consent, digest := approvedAuthorization(t, base, token)
	grant := liveGrant(t, base, id, token, "custody-"+id)
	parsed, err := jose.ParseSignedCompact(grant.Grant, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal(err)
	}
	grantKey := custodyKey(t, "grant-signing")
	payload, err := parsed.Verify(&grantKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	var claims az.Grant
	if json.Unmarshal(payload, &claims) != nil {
		t.Fatal("invalid actual grant")
	}
	input := az.Redemption{AttemptID: "custody-attempt", Grant: grant.Grant, RequestID: id, Digest: digest, Presenter: az.Presenter{Consumer: claims.Consumer, Thumbprint: claims.Confirmation.Thumbprint}}
	reservation := liveRedemption(t, input)
	if !reservation.First {
		t.Fatal("new evidence fixture was not admitted")
	}
	event := custody.Event{EventID: "custody-" + id, RequestID: id, Digest: digest, ConsentID: consent, GrantJTI: claims.JTI, AdapterID: "lokos-adapter", AttemptID: input.AttemptID, RedemptionID: reservation.ID, Fields: claims.Fields, Outcome: "release_ready", Commitment: cm.Digest([]byte("fictional SDK interoperability fixture commitment")), OccurredAt: time.Now().UTC()}
	key := custodyKey(t, "adapter-evidence")
	compact := liveEvidence(t, key, event)
	if response, _ := liveResponse(t, &http.Client{Timeout: 5 * time.Second}, custodyRequest(base, compact)); response.StatusCode != 404 {
		t.Fatal("custody exposed on public listener", response.StatusCode)
	}
	anon := serviceClient(t, "", "gateway.gramstack.internal")
	if r, err := anon.Do(custodyRequest(private, compact)); err == nil {
		r.Body.Close()
		t.Fatal("custody accepted anonymous TLS")
	}
	for _, identity := range []string{"gateway", "consumer-one", "unauthorised"} {
		r := custodyRequest(private, compact)
		r.Header.Set("X-Forwarded-Client-Cert", "lokos-adapter.gramstack.internal")
		response, body := liveResponse(t, serviceClient(t, identity, "gateway.gramstack.internal"), r)
		if response.StatusCode != 403 || strings.Contains(body, compact) {
			t.Fatal("unregistered producer accepted", identity, response.StatusCode)
		}
	}
	adapter := serviceClient(t, "lokos-adapter", "gateway.gramstack.internal")
	bad := event
	bad.Fields = []string{"savings_summary"}
	if response, _ := liveResponse(t, adapter, custodyRequest(private, liveEvidence(t, key, bad))); response.StatusCode != 403 {
		t.Fatal("wrong evidence scope accepted", response.StatusCode)
	}
	first := custodyAck(t, adapter, private, compact)
	retry := custodyAck(t, adapter, private, compact)
	if first.ReceiptID != retry.ReceiptID || !first.AcceptedAt.Equal(retry.AcceptedAt) {
		t.Fatal("HTTP retry acquired different receipt")
	}
	version := awaitRequestState(t, base, token, id, "EVIDENCE_COMMITTED")
	pool, _ := database(t)
	ctx := context.Background()
	var stored string
	var count int
	if err = pool.QueryRow(ctx, `SELECT jws FROM evidence_receipts WHERE receipt_id=$1`, first.ReceiptID).Scan(&stored); err != nil || stored != compact {
		t.Fatal("HTTP ack preceded exact receipt durability", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_type='evidence' AND aggregate_id=$1 AND event_type='evidence.received'`, first.ReceiptID).Scan(&count); err != nil || count != 1 {
		t.Fatal("HTTP ack missing atomic anchoring work", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM evidence_anchor_state WHERE receipt_id=$1 AND (status='pending' OR ($2 AND status='anchored'))`, first.ReceiptID, os.Getenv("GRAMSTACK_TEST_ANCHORING") == "1").Scan(&count); err != nil || count != 1 {
		t.Fatal("pending anchor custody missing", err)
	}
	raw, err := json.Marshal(custodyCheckpoint{id, compact, first, version})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("GRAMSTACK_TEST_CUSTODY_RESUME"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: actual service mTLS with the registered adapter certificate -> approved scoped ES256 evidence -> immutable receipt + anchoring outbox + request FSM; wrong producer/scope and public/anonymous access denied")
}
func TestActualEvidenceCustodyRestart(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_CUSTODY") != "1" {
		t.Skip("requires restarted custody endpoint")
	}
	raw, err := os.ReadFile(os.Getenv("GRAMSTACK_TEST_CUSTODY_RESUME"))
	var saved custodyCheckpoint
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.RequestID == "" {
		t.Fatal("missing custody checkpoint", err)
	}
	ack := custodyAck(t, serviceClient(t, "lokos-adapter", "gateway.gramstack.internal"), os.Getenv("GRAMSTACK_TEST_AUTHZ_URL"), saved.JWS)
	if ack.ReceiptID != saved.Ack.ReceiptID || ack.Digest != saved.Ack.Digest || !ack.AcceptedAt.Equal(saved.Ack.AcceptedAt) {
		t.Fatal("restart changed durable receipt")
	}
	if got := awaitRequestState(t, os.Getenv("GRAMSTACK_TEST_GATEWAY_URL"), liveToken(t), saved.RequestID, "EVIDENCE_COMMITTED"); got != saved.Version {
		t.Fatal("restart duplicated lifecycle projection")
	}
	t.Log("PASS: restart preserved exact custody acknowledgement and request version without another receipt or work item")
}
