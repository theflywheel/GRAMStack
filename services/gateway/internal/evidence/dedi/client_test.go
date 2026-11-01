package dedi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

func TestSignedPublishAndVerifiedProof(t *testing.T) {
	_, publisher, _ := ed25519.GenerateKey(rand.Reader)
	skey, vkey, err := note.GenerateKey(rand.Reader, "test-node")
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := note.NewSigner(skey)
	digest := "sha256:" + strings.Repeat("a", 64)
	id := sha256.Sum256([]byte("event-1"))
	record := hex.EncodeToString(id[:])
	payload := []byte(`{"schema":"gramstack.evidence.v1","evidence_digest":"` + digest + `"}`)
	payloadHash := sha256.Sum256(payload)
	created := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	leafBytes, _ := json.Marshal([]any{"dedi/v1/leaf", "record", "gramstack", "evidence", record, 1, hex.EncodeToString(payloadHash[:]), "publisher:publisher-1", created})
	root := tlog.RecordHash(leafBytes)
	signed, _ := note.Sign(&note.Note{Text: fmt.Sprintf("test/log\n1\n%s\n", base64.StdEncoding.EncodeToString(root[:]))}, signer)
	publications := 0
	tamper := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			publications++
			body, _ := io.ReadAll(r.Body)
			hash := sha256.Sum256(body)
			preimage := strings.Join([]string{"dedi/v1/publish", "POST", r.URL.RequestURI(), base64.StdEncoding.EncodeToString(hash[:]), "", "*", r.Header.Get("DeDi-Timestamp")}, "\n")
			signature, _ := base64.StdEncoding.DecodeString(r.Header.Get("DeDi-Signature"))
			user, password, ok := r.BasicAuth()
			if !ok || user != "admin" || password != "test-password" || r.Header.Get("If-None-Match") != "*" || r.Header.Get("DeDi-Key-Id") != "publisher-1" || !ed25519.Verify(publisher.Public().(ed25519.PublicKey), []byte(preimage), signature) {
				t.Error("publisher authentication invalid")
				w.WriteHeader(401)
				return
			}
			if publications > 1 {
				w.WriteHeader(412)
			} else {
				w.WriteHeader(201)
			}
			return
		}
		if r.URL.Path != "/dedi/lookup/gramstack/evidence/"+record || r.URL.Query().Get("proof") != "inclusion" {
			t.Error("wrong proof target")
		}
		response := map[string]any{"data": map[string]any{"details": json.RawMessage(payload), "state": "live"}, "proof": map[string]any{"leaf_index": 0, "tree_size": 1, "checkpoint": string(signed), "path": []string{}, "leaf": map[string]any{"entry_type": "record", "namespace": "gramstack", "registry": "evidence", "record_name": record, "version_num": 1, "digest": hex.EncodeToString(payloadHash[:]), "created_by": "publisher:publisher-1", "created_at": created}}}
		if tamper {
			response["proof"].(map[string]any)["leaf"].(map[string]any)["created_at"] = "2026-11-03T00:00:00Z"
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	config := Config{BaseURL: server.URL, Namespace: "gramstack", Registry: "evidence", PublisherKeyID: "publisher-1", PublisherKey: publisher, AdminUser: "admin", AdminPassword: "test-password", CheckpointOrigin: "test/log", CheckpointVerifier: vkey, AllowInsecureLoopback: true}
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		anchor, err := client.Publish(context.Background(), "event-1", digest)
		if err != nil || anchor.TreeSize != 1 {
			t.Fatalf("%+v %v", anchor, err)
		}
	}
	if _, err := client.Publish(context.Background(), "event-1", "sha256:"+strings.Repeat("b", 64)); err == nil {
		t.Fatal("conflicting commitment accepted")
	}
	tamper = true
	if _, err := client.Publish(context.Background(), "event-1", digest); err == nil {
		t.Fatal("tampered leaf accepted")
	}
	tamper = false
	_, wrongVerifier, _ := note.GenerateKey(rand.Reader, "other-node")
	config.CheckpointVerifier = wrongVerifier
	wrong, _ := New(config)
	if _, err := wrong.Publish(context.Background(), "event-1", digest); err == nil {
		t.Fatal("wrong checkpoint key accepted")
	}
	config.BaseURL = "http://192.0.2.1"
	if _, err := New(config); err == nil {
		t.Fatal("remote plaintext endpoint accepted")
	}
}
