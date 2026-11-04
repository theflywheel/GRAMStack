package migrations

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReadinessRequiresExactExplicitMigrations(t *testing.T) {
	dsn := os.Getenv("GRAMSTACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "readiness_" + rand.Text()
	// rand.Text only uses safe base32 characters; this test owns its schema.
	if _, err = admin.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = `"` + schema + `"`
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = Check(ctx, pool); err == nil {
		t.Fatal("unmigrated database ready")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1`, schema).Scan(&count); err != nil || count != 0 {
		t.Fatal("readiness mutated empty schema", count, err)
	}
	if err = Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = Check(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE schema_migrations SET checksum='changed' WHERE name='0001_core.sql'`); err != nil {
		t.Fatal(err)
	}
	if err = Check(ctx, pool); err == nil {
		t.Fatal("changed migration ready")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE name='0001_core.sql'`); err != nil {
		t.Fatal(err)
	}
	if err = Check(ctx, pool); err == nil {
		t.Fatal("missing migration ready")
	}
	pool.Close()
	if err = Check(ctx, pool); err == nil {
		t.Fatal("unavailable database ready")
	}
}
