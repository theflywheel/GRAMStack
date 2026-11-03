package lokos

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

func TestDurableAttemptClaimFencingEncryptionAndRestart(t *testing.T) {
	db, _, key := contextDatabase(t)
	ctx := context.Background()
	store, err := sdk.NewSQLAttemptStore(db, "lokos-adapter", key)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	input := sdk.Attempt{ID: rand.Text(), RequestID: rand.Text(), Digest: sdk.Digest([]byte("request")), GrantJTI: rand.Text(), GrantDigest: sdk.Digest([]byte("signed grant")), Consumer: "consumer-one", Thumbprint: strings.Repeat("a", 43)}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Claim(ctx, input); results <- err }()
	}
	wg.Wait()
	close(results)
	claimed := 0
	for err := range results {
		if err == nil {
			claimed++
		} else if !errors.Is(err, sdk.ErrAttemptExists) {
			t.Fatal(err)
		}
	}
	if claimed != 1 {
		t.Fatal("duplicate durable claims", claimed)
	}
	for _, edit := range []func(*sdk.Attempt){func(a *sdk.Attempt) { a.ID = rand.Text() }, func(a *sdk.Attempt) { a.ID = rand.Text(); a.RequestID = rand.Text() }} {
		v := input
		edit(&v)
		if _, err = store.Claim(ctx, v); !errors.Is(err, sdk.ErrAttemptExists) {
			t.Fatal("request or grant slot reused", err)
		}
	}
	restarted, err := sdk.NewSQLAttemptStore(db, "lokos-adapter", key)
	if err != nil {
		t.Fatal(err)
	}
	a, err := restarted.Get(ctx, input.ID)
	if err != nil || a.State != "CLAIMED" || a.Version != 1 {
		t.Fatal("restart lost claim", err)
	}
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := restarted.Advance(ctx, input.ID, 1, "admitted", sdk.AttemptUpdate{RedemptionID: "reservation-1"})
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	won := 0
	for err := range outcomes {
		if err == nil {
			won++
		} else if !errors.Is(err, sdk.ErrAttemptConflict) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal("stale version mutated authority")
	}
	a, err = restarted.Advance(ctx, input.ID, 2, "source_started", sdk.AttemptUpdate{})
	if err != nil {
		t.Fatal(err)
	}
	signerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := sdk.NewEvidenceSigner("adapter-key", signerKey)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(ctx, sdk.EvidenceEvent{EventID: rand.Text(), RequestID: input.RequestID, Digest: input.Digest, ConsentID: "consent-1", GrantJTI: input.GrantJTI, AdapterID: "lokos-adapter", AttemptID: input.ID, RedemptionID: a.RedemptionID, Fields: []string{"membership_status"}, Outcome: "release_ready", ResponseCommitment: sdk.Digest([]byte("protected commitment")), OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	protected := sdk.ProtectedEvidence{JWS: signed, Salt: bytes.Repeat([]byte("s"), 32)}
	// A failed checkpoint must not partially persist its recovery material.
	if _, err = db.ExecContext(ctx, `CREATE FUNCTION fail_attempt_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test checkpoint failure'; END $$; CREATE TRIGGER fail_attempt_checkpoint BEFORE UPDATE ON adapter_attempts FOR EACH ROW EXECUTE FUNCTION fail_attempt_checkpoint()`); err != nil {
		t.Fatal(err)
	}
	_, failure := restarted.Advance(ctx, input.ID, a.Version, "evidence_prepared", sdk.AttemptUpdate{Evidence: &protected})
	if _, err = db.ExecContext(ctx, `DROP TRIGGER fail_attempt_checkpoint ON adapter_attempts; DROP FUNCTION fail_attempt_checkpoint()`); err != nil {
		t.Fatal(err)
	}
	if failure == nil {
		t.Fatal("checkpoint fault ignored")
	}
	var raw []byte
	var digest string
	if err = db.QueryRowContext(ctx, `SELECT protected_evidence,evidence_digest FROM adapter_attempts WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID).Scan(&raw, &digest); err != nil || len(raw) != 0 || digest != "" {
		t.Fatal("failed checkpoint partially persisted", err)
	}
	a, err = restarted.Advance(ctx, input.ID, a.Version, "evidence_prepared", sdk.AttemptUpdate{Evidence: &protected})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT protected_evidence FROM adapter_attempts WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID).Scan(&raw); err != nil || bytes.Contains(raw, []byte(signed)) || bytes.Contains(raw, protected.Salt) {
		t.Fatal("protected recovery material stored in clear", err)
	}
	recovered, err := restarted.Evidence(ctx, input.ID)
	if err != nil || recovered.JWS != signed || !bytes.Equal(recovered.Salt, protected.Salt) {
		t.Fatal("restart could not recover evidence", err)
	}
	wrong, err := sdk.NewSQLAttemptStore(db, "lokos-adapter", bytes.Repeat([]byte("x"), 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Evidence(ctx, input.ID); !errors.Is(err, sdk.ErrAttemptConflict) {
		t.Fatal("wrong key decrypted evidence", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE adapter_attempts SET request_digest=$2 WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID, sdk.Digest([]byte("changed"))); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Evidence(ctx, input.ID); !errors.Is(err, sdk.ErrAttemptConflict) {
		t.Fatal("metadata substitution decrypted evidence", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE adapter_attempts SET request_digest=$2 WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID, input.Digest); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"grant_jti", "consumer_id", "presenter_thumbprint"} {
		// Column names are fixed test constants, never API input.
		var original string
		if err = db.QueryRowContext(ctx, `SELECT `+column+` FROM adapter_attempts WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID).Scan(&original); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE adapter_attempts SET `+column+`=$2 WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID, "substituted"); err != nil {
			t.Fatal(err)
		}
		if _, err = restarted.Evidence(ctx, input.ID); !errors.Is(err, sdk.ErrAttemptConflict) {
			t.Fatal("authority metadata substitution decrypted evidence", column, err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE adapter_attempts SET `+column+`=$2 WHERE adapter_id='lokos-adapter' AND attempt_id=$1`, input.ID, original); err != nil {
			t.Fatal(err)
		}
	}
	bad := sdk.EvidenceAcknowledgement{ReceiptID: "receipt-1", Digest: sdk.Digest([]byte("other")), AcceptedAt: time.Now(), AnchorStatus: "pending"}
	if _, err = restarted.Advance(ctx, input.ID, a.Version, "evidence_committed", sdk.AttemptUpdate{Ack: &bad}); !errors.Is(err, sdk.ErrAttemptConflict) {
		t.Fatal("receipt substitution accepted", err)
	}
	bad.Digest = sdk.Digest([]byte(signed))
	a, err = restarted.Advance(ctx, input.ID, a.Version, "evidence_committed", sdk.AttemptUpdate{Ack: &bad})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"release_started", "response_written"} {
		a, err = restarted.Advance(ctx, input.ID, a.Version, event, sdk.AttemptUpdate{})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = restarted.Advance(ctx, input.ID, a.Version, "source_started", sdk.AttemptUpdate{}); !errors.Is(err, sdk.ErrAttemptConflict) {
		t.Fatal("terminal attempt revived", err)
	}
	other, _ := sdk.NewSQLAttemptStore(db, "other-adapter", key)
	if _, err = other.Get(ctx, input.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("attempt crossed adapter identity", err)
	}
	if err = restarted.Migrate(ctx); err != nil {
		t.Fatal("migration retry changed schema", err)
	}
}
