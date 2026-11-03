package authn

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/dedi"
)

// Run after protected retrieval and again after gateway/adapter/source restart.
func TestActualEvidenceAnchoringProcess(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_ANCHORING") != "1" {
		t.Skip("configured gateway and actual DeDi node required")
	}
	rawKey, err := base64.StdEncoding.DecodeString(os.Getenv("GRAMSTACK_TEST_DEDI_PUBLISHER_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := dedi.New(dedi.Config{BaseURL: os.Getenv("DEDI_NODE_URL"), Namespace: os.Getenv("DEDI_NAMESPACE"), Registry: os.Getenv("DEDI_REGISTRY"), PublisherKeyID: os.Getenv("DEDI_PUBLISHER_KEY_ID"), PublisherKey: ed25519.PrivateKey(rawKey), AdminUser: os.Getenv("DEDI_ADMIN_USER"), AdminPassword: os.Getenv("DEDI_ADMIN_PASSWORD"), CheckpointOrigin: os.Getenv("DEDI_CHECKPOINT_ORIGIN"), CheckpointVerifier: os.Getenv("DEDI_CHECKPOINT_VERIFIER"), AllowInsecureLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		var total, complete int
		err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE a.status='anchored' AND d.acknowledged_at IS NOT NULL) FROM evidence_receipts r JOIN evidence_anchor_state a USING(receipt_id) JOIN outbox e ON e.aggregate_type='evidence' AND e.aggregate_id=r.receipt_id LEFT JOIN outbox_deliveries d ON d.event_id=e.event_id AND d.subscriber='evidence.anchoring.v1'`).Scan(&total, &complete)
		if err != nil {
			t.Fatal(err)
		}
		if total > 0 && complete == total {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("configured worker failed to drain evidence", total, complete)
		case <-time.After(50 * time.Millisecond):
		}
	}
	rows, err := pool.Query(ctx, `SELECT r.receipt_id,r.evidence_digest,a.anchor_reference,(SELECT count(*) FROM outbox e WHERE e.aggregate_type='anchor' AND e.aggregate_id=r.receipt_id AND e.event_type='evidence.anchored') FROM evidence_receipts r JOIN evidence_anchor_state a USING(receipt_id)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, digest string
		var raw []byte
		var events int
		if err := rows.Scan(&id, &digest, &raw, &events); err != nil {
			t.Fatal(err)
		}
		var proof dedi.Anchor
		if json.Unmarshal(raw, &proof) != nil || client.VerifyAnchor(id, digest, proof) != nil || events != 1 {
			t.Fatal("invalid or duplicate verified projection")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("PASS: configured gateway worker anchored %d real custody receipts with offline-verifiable inclusion proofs and one projection each", count)
}
