package dedi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// Set these variables only for an isolated operator-provisioned test node.
// The test creates an evidence namespace/registry and publishes synthetic digests.
func TestLiveNode(t *testing.T) {
	endpoint := os.Getenv("GRAMSTACK_TEST_DEDI_URL")
	if endpoint == "" {
		t.Skip("isolated DeDi integration node not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("GRAMSTACK_TEST_DEDI_PUBLISHER_KEY"))
	if err != nil {
		t.Fatal("invalid test publisher key")
	}
	client, err := New(Config{BaseURL: endpoint, Namespace: "gramstack-test", Registry: "commitments", PublisherKeyID: "gramstack-test", PublisherKey: ed25519.PrivateKey(raw), AdminUser: "admin", AdminPassword: os.Getenv("GRAMSTACK_TEST_DEDI_PASSWORD"), CheckpointOrigin: "gramstack-test/log", CheckpointVerifier: os.Getenv("GRAMSTACK_TEST_DEDI_VERIFIER"), AllowInsecureLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	first, err := client.Publish(context.Background(), "integration-event-1", digest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Publish(context.Background(), "integration-event-1", digest)
	if err != nil {
		t.Fatal(err)
	}
	if first.RecordID != second.RecordID || first.LeafIndex != second.LeafIndex {
		t.Fatal("duplicate publication changed the anchored record")
	}
	if _, err := client.Publish(context.Background(), "integration-event-1", "sha256:"+strings.Repeat("b", 64)); err == nil {
		t.Fatal("conflicting event accepted")
	}
	t.Logf("Verified actual DeDi signed publication, retry and Merkle proof at tree size %d", second.TreeSize)
}
