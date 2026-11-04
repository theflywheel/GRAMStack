// Package migrations applies embedded, checksummed schema migrations while
// holding a database advisory lock across concurrent operator calls.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	const lockID int64 = 0x4752414d535441
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.Exec(release, "SELECT pg_advisory_unlock($1)", lockID)
	}()
	if _, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT clock_timestamp())`); err != nil {
		return err
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		raw, err := files.ReadFile(name)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		checksum := hex.EncodeToString(hash[:])
		var existing string
		err = conn.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", name).Scan(&existing)
		if err == nil {
			if existing != checksum {
				return fmt.Errorf("migration checksum changed: %s", name)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(raw)); err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", name, checksum)
		}
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s failed: %w", name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Check verifies every embedded migration without executing DDL. Readiness must
// never make an unmigrated database appear usable by applying migrations itself.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	rows, err := pool.Query(ctx, "SELECT name, checksum FROM schema_migrations")
	if err != nil {
		return err
	}
	defer rows.Close()
	applied := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return err
		}
		applied[name] = checksum
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		raw, err := files.ReadFile(name)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		if applied[name] != hex.EncodeToString(hash[:]) {
			return fmt.Errorf("migration missing or changed: %s", name)
		}
	}
	return nil
}
