package consentmanager_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/anchoring"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/custody"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/dedi"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

func TestActualEvidenceAnchoring(t *testing.T) {
	endpoint := os.Getenv("GRAMSTACK_TEST_DEDI_URL")
	if endpoint == "" {
		t.Skip("isolated actual DeDi node not configured")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("GRAMSTACK_TEST_DEDI_PUBLISHER_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"lost acknowledgement", "outage", "publication before SQL rollback", "concurrent publication"} {
		t.Run(scenario, func(t *testing.T) {
			f := setupCustody(t)
			ctx := context.Background()
			signed := signEvidence(t, f.key, f.event, custody.EvidenceType, nil)
			ack, err := f.custodian.Record(ctx, "lokos-adapter", signed)
			if err != nil {
				t.Fatal(err)
			}
			if ack.AnchorStatus != "pending" {
				t.Fatal("batch acknowledgement claimed anchoring")
			}
			var offline atomic.Bool
			var publications atomic.Int64
			var invalidPayload atomic.Bool
			proxy := httputil.NewSingleHostReverseProxy(target)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if offline.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if r.Method == http.MethodPost {
					publications.Add(1)
					raw, err := io.ReadAll(r.Body)
					r.Body.Close()
					var body struct {
						Payload map[string]string `json:"payload"`
					}
					if err != nil || json.Unmarshal(raw, &body) != nil || len(body.Payload) != 2 || body.Payload["schema"] != "gramstack.evidence.v1" || body.Payload["evidence_digest"] != ack.Digest {
						invalidPayload.Store(true)
					}
					r.Body = io.NopCloser(bytes.NewReader(raw))
				}
				proxy.ServeHTTP(w, r)
			}))
			defer server.Close()
			client, err := dedi.New(dedi.Config{BaseURL: server.URL, Namespace: "gramstack-test", Registry: "commitments", PublisherKeyID: "gramstack-test", PublisherKey: ed25519.PrivateKey(key), AdminUser: "admin", AdminPassword: os.Getenv("GRAMSTACK_TEST_DEDI_PASSWORD"), CheckpointOrigin: "gramstack-test/log", CheckpointVerifier: os.Getenv("GRAMSTACK_TEST_DEDI_VERIFIER"), AllowInsecureLoopback: true})
			if err != nil {
				t.Fatal(err)
			}
			if err = client.Provision(ctx); err != nil {
				t.Fatal(err)
			}
			worker, err := anchoring.New(f.pool, f.custodian, client)
			if err != nil {
				t.Fatal(err)
			}
			queue, err := outbox.NewQueue(f.pool, anchoring.Subscriber, []string{"evidence.received"}, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			foreign, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			badConfig := f.custodyConfig
			badConfig.Adapters = append([]custody.Adapter(nil), f.custodyConfig.Adapters...)
			badConfig.Adapters[0].Keys = map[string]*ecdsa.PublicKey{"adapter-key": &foreign.PublicKey}
			invalidCustody, err := custody.New(badConfig)
			if err != nil {
				t.Fatal(err)
			}
			invalidWorker, err := anchoring.New(f.pool, invalidCustody, client)
			if err != nil {
				t.Fatal(err)
			}
			if err := invalidWorker.Handle(ctx, outbox.Delivery{AggregateID: ack.ReceiptID, EventType: "evidence.received", AggregateVersion: 1}); err == nil || publications.Load() != 0 {
				t.Fatal("untrusted retained evidence published")
			}
			if err := worker.Handle(ctx, outbox.Delivery{AggregateID: ack.ReceiptID, EventType: "evidence.received", AggregateVersion: 2}); err == nil || publications.Load() != 0 {
				t.Fatal("unknown work version published")
			}
			delivery, ok, err := queue.Claim(ctx)
			if err != nil || !ok {
				t.Fatal("missing durable anchoring work", err)
			}
			assertState := func(want string, events int) {
				t.Helper()
				var status string
				var count int
				if err := f.pool.QueryRow(ctx, `SELECT status FROM evidence_anchor_state WHERE receipt_id=$1`, ack.ReceiptID).Scan(&status); err != nil {
					t.Fatal(err)
				}
				if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_type='anchor' AND aggregate_id=$1`, ack.ReceiptID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if status != want || count != events {
					t.Fatal("incorrect atomic projection", status, count, want, events)
				}
			}
			switch scenario {
			case "outage":
				// Persist a retry using the actual worker, not an in-memory loop.
				if err := queue.Retry(ctx, delivery, 0); err != nil {
					t.Fatal(err)
				}
				offline.Store(true)
				if worked, err := worker.Deliver(ctx); !worked || err == nil {
					t.Fatal("outage was acknowledged", worked, err)
				}
				assertState("pending", 0)
				var attempts int
				var backedOff bool
				if err := f.pool.QueryRow(ctx, `SELECT attempts,available_at>clock_timestamp() AND lease_token IS NULL AND acknowledged_at IS NULL FROM outbox_deliveries WHERE subscriber=$1 AND event_id=$2`, anchoring.Subscriber, delivery.EventID).Scan(&attempts, &backedOff); err != nil || attempts != 2 || !backedOff {
					t.Fatal("retry not retained with backoff", err)
				}
				offline.Store(false)
				if _, err := f.pool.Exec(ctx, `UPDATE outbox_deliveries SET available_at=clock_timestamp() WHERE subscriber=$1`, anchoring.Subscriber); err != nil {
					t.Fatal(err)
				}
			case "publication before SQL rollback":
				if _, err := f.pool.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES('anchor-fault','anchor',$1,1,'fault','{}')`, ack.ReceiptID); err != nil {
					t.Fatal(err)
				}
				if err := worker.Handle(ctx, delivery); err == nil {
					t.Fatal("partial SQL transaction acknowledged")
				}
				assertState("pending", 1)
				if _, err := f.pool.Exec(ctx, `DELETE FROM outbox WHERE event_id='anchor-fault'`); err != nil {
					t.Fatal(err)
				}
				if err := queue.Retry(ctx, delivery, 0); err != nil {
					t.Fatal(err)
				}
			case "lost acknowledgement":
				if err := worker.Handle(ctx, delivery); err != nil {
					t.Fatal(err)
				}
				assertState("anchored", 1)
				offline.Store(true) // Recovery must verify retained proof without the node.
				if _, err := f.pool.Exec(ctx, `UPDATE outbox_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE subscriber=$1`, anchoring.Subscriber); err != nil {
					t.Fatal(err)
				}
				if err := queue.Ack(ctx, delivery); !errors.Is(err, outbox.ErrLease) {
					t.Fatal("stale acknowledgement accepted", err)
				}
			case "concurrent publication":
				var group sync.WaitGroup
				failures := make(chan error, 4)
				for range 4 {
					group.Go(func() { failures <- worker.Handle(ctx, delivery) })
				}
				group.Wait()
				close(failures)
				for err := range failures {
					if err != nil {
						t.Fatal(err)
					}
				}
				assertState("anchored", 1)
				if err := queue.Retry(ctx, delivery, 0); err != nil {
					t.Fatal(err)
				}
			}
			// New instance proves pending work and verified state survive restart.
			restarted, err := anchoring.New(f.pool, f.custodian, client)
			if err != nil {
				t.Fatal(err)
			}
			if worked, err := restarted.Deliver(ctx); !worked || err != nil {
				t.Fatal("restart recovery failed", worked, err)
			}
			assertState("anchored", 1)
			if worked, err := restarted.Deliver(ctx); worked || err != nil {
				t.Fatal("acknowledged work redelivered", worked, err)
			}
			if invalidPayload.Load() {
				t.Fatal("DeDi received more than the protected commitment")
			}
			var raw []byte
			if err := f.pool.QueryRow(ctx, `SELECT anchor_reference FROM evidence_anchor_state WHERE receipt_id=$1`, ack.ReceiptID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var anchor dedi.Anchor
			if json.Unmarshal(raw, &anchor) != nil || client.VerifyAnchor(ack.ReceiptID, ack.Digest, anchor) != nil {
				t.Fatal("retained proof cannot be verified offline")
			}
			if publications.Load() < 1 {
				t.Fatal("actual publication not exercised")
			}
			if scenario == "lost acknowledgement" && publications.Load() != 1 {
				t.Fatal("lost acknowledgement republished")
			}
			if scenario == "publication before SQL rollback" && publications.Load() != 2 {
				t.Fatal("create-only reconciliation not exercised")
			}
			if _, err := f.pool.Exec(ctx, `UPDATE evidence_anchor_state SET status='pending' WHERE receipt_id=$1`, ack.ReceiptID); err == nil {
				t.Fatal("verified projection regressed")
			}
			if _, err := f.pool.Exec(ctx, `UPDATE evidence_anchor_state SET anchor_reference='{}' WHERE receipt_id=$1`, ack.ReceiptID); err == nil {
				t.Fatal("verified proof overwritten")
			}
			original, err := f.custodian.Record(ctx, "lokos-adapter", signed)
			if err != nil || original.AnchorStatus != "pending" || original.ReceiptID != ack.ReceiptID {
				t.Fatal("historical acknowledgement changed", err)
			}
			requestState(t, f.fixture, "EVIDENCE_COMMITTED")
			t.Log("Verified actual-node commitment, retained proof and durable recovery:", scenario)
		})
	}
}
