package lokos

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

func contextDatabase(t *testing.T) (*sql.DB, *sdk.SQLContextStore, []byte) {
	t.Helper()
	dsn := os.Getenv("GRAMSTACK_TEST_ADAPTER_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated adapter PostgreSQL not configured")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	store, err := sdk.NewSQLContextStore(db, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db, store, key
}
func contextValue() sdk.StoredContext {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return sdk.StoredContext{PrincipalDigest: "sha256:" + strings.Repeat("b", 64), RequestID: "req_" + rand.Text(), Context: sdk.SubjectContext{ContextID: "ctx_" + rand.Text(), Registry: "lokos", RequestDigest: "sha256:" + strings.Repeat("a", 64), PrincipalContextRef: "principal_" + rand.Text(), SourceMappingRef: "native-private-record-1", VerificationMethod: "demo.registry-map-v1", VerifiedAt: now, ExpiresAt: now.Add(time.Minute)}}
}
func TestDurableContextEncryptionConcurrencyAndRestart(t *testing.T) {
	db, store, key := contextDatabase(t)
	ctx := context.Background()
	input := contextValue()
	var wg sync.WaitGroup
	results := make(chan sdk.StoredContext, 20)
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := input
			v.Context.ContextID = "ctx_" + rand.Text()
			got, err := store.Save(ctx, v)
			if err != nil {
				failures <- err
			} else {
				results <- got
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var first sdk.StoredContext
	for result := range results {
		if first.RequestID == "" {
			first = result
		} else if result != first {
			t.Fatal("concurrent retries created different contexts")
		}
	}
	var raw []byte
	var count int
	if err := db.QueryRowContext(ctx, `SELECT payload FROM adapter_subject_contexts WHERE context_id=$1`, first.Context.ContextID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(input.Context.SourceMappingRef)) {
		t.Fatal("native mapping persisted in plaintext")
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM adapter_subject_contexts WHERE request_id=$1`, input.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate contexts persisted", err, count)
	}
	restarted, err := sdk.NewSQLContextStore(db, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Resolve(ctx, first.Context.ContextID)
	if err != nil || got != first {
		t.Fatal("restart lost durable mapping", err)
	}
	different := input
	different.Context.ContextID = "ctx_" + rand.Text()
	different.Context.SourceMappingRef = "native-private-record-2"
	if _, err = restarted.Save(ctx, different); !errors.Is(err, sdk.ErrVerification) {
		t.Fatal("retry changed verified native mapping", err)
	}
	wrong, _ := sdk.NewSQLContextStore(db, bytes.Repeat([]byte{9}, 32))
	if _, err = wrong.Resolve(ctx, first.Context.ContextID); !errors.Is(err, sdk.ErrVerification) {
		t.Fatal("wrong encryption key read a mapping", err)
	}
}
func TestContextTamperExpiryRenewalAndMigrations(t *testing.T) {
	db, store, _ := contextDatabase(t)
	ctx := context.Background()
	for _, column := range []string{"request_id", "expires_at"} {
		t.Run(column, func(t *testing.T) {
			v := contextValue()
			saved, err := store.Save(ctx, v)
			if err != nil {
				t.Fatal(err)
			}
			if column == "request_id" {
				_, err = db.ExecContext(ctx, `UPDATE adapter_subject_contexts SET request_id='tampered' WHERE context_id=$1`, saved.Context.ContextID)
			} else {
				_, err = db.ExecContext(ctx, `UPDATE adapter_subject_contexts SET expires_at=expires_at+interval '1 minute' WHERE context_id=$1`, saved.Context.ContextID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.Resolve(ctx, saved.Context.ContextID); !errors.Is(err, sdk.ErrVerification) {
				t.Fatal("metadata tampering accepted", err)
			}
		})
	}
	old := contextValue()
	saved, err := store.Save(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE adapter_subject_contexts SET expires_at=clock_timestamp()-interval '1 second' WHERE context_id=$1`, saved.Context.ContextID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Resolve(ctx, saved.Context.ContextID); !errors.Is(err, sdk.ErrContextMissing) {
		t.Fatal("expired context resolved", err)
	}
	fresh := old
	fresh.Context.ContextID = "ctx_" + rand.Text()
	fresh.Context.VerifiedAt = time.Now().UTC().Truncate(time.Microsecond)
	fresh.Context.ExpiresAt = fresh.Context.VerifiedAt.Add(time.Minute)
	renewed, err := store.Save(ctx, fresh)
	if err != nil || renewed.Context.ContextID == saved.Context.ContextID {
		t.Fatal("fresh verification could not replace expired context", err)
	}
	if _, err = store.Resolve(ctx, saved.Context.ContextID); !errors.Is(err, sdk.ErrContextMissing) {
		t.Fatal("old context revived", err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal("repeat migration failed", err)
	}
	if err = store.Ready(ctx); err != nil {
		t.Fatal("migrated store not ready", err)
	}
	var checksum string
	if err = db.QueryRowContext(ctx, `SELECT digest FROM adapter_schema_migrations WHERE version=1`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE adapter_schema_migrations SET digest='tampered' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `UPDATE adapter_schema_migrations SET digest=$1 WHERE version=1`, checksum)
	if err = store.Migrate(ctx); err == nil {
		t.Fatal("migration checksum mismatch accepted")
	}
	if err = store.Ready(ctx); err == nil {
		t.Fatal("readiness accepted changed schema")
	}
}
