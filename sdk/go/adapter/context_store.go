package adapter

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// SQLContextStore is an optional PostgreSQL-backed SDK store using database/sql.
// Registry source mappings are encrypted; the driver is supplied by the host.
type SQLContextStore struct {
	db   *sql.DB
	aead cipher.AEAD
}

func NewSQLContextStore(db *sql.DB, key []byte) (*SQLContextStore, error) {
	if db == nil || len(key) != 32 {
		return nil, errors.New("database and 32-byte context encryption key required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SQLContextStore{db, aead}, nil
}

const contextSchema = `CREATE TABLE adapter_subject_contexts(context_id text PRIMARY KEY,request_id text NOT NULL,request_digest text NOT NULL,principal_ref text NOT NULL,payload bytea NOT NULL,expires_at timestamptz NOT NULL,UNIQUE(request_id,request_digest,principal_ref)); CREATE INDEX adapter_context_expiry ON adapter_subject_contexts(expires_at)`

// Migrate is an explicit, checksummed and serialized operator operation.
func (s *SQLContextStore) Migrate(ctx context.Context) error {
	return migrateSchema(ctx, s.db, 1, contextSchema)
}

// Ready checks the installed schema without applying migrations.
func (s *SQLContextStore) Ready(ctx context.Context) error {
	return checkSchema(ctx, s.db, 1, contextSchema)
}
func checkSchema(ctx context.Context, db *sql.DB, version int, schema string) error {
	var digest string
	if err := db.QueryRowContext(ctx, `SELECT digest FROM adapter_schema_migrations WHERE version=$1`, version).Scan(&digest); err != nil {
		return err
	}
	if digest != Digest([]byte(schema)) {
		return errors.New("adapter schema checksum mismatch")
	}
	return nil
}
func migrateSchema(ctx context.Context, db *sql.DB, version int, schema string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(6684936279132501)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS adapter_schema_migrations(version integer PRIMARY KEY,digest text NOT NULL)`); err != nil {
		return err
	}
	var digest string
	err = tx.QueryRowContext(ctx, `SELECT digest FROM adapter_schema_migrations WHERE version=$1`, version).Scan(&digest)
	switch {
	case err == nil:
		if digest != Digest([]byte(schema)) {
			return errors.New("adapter schema checksum mismatch")
		}
	case errors.Is(err, sql.ErrNoRows):
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO adapter_schema_migrations(version,digest) VALUES($1,$2)`, version, Digest([]byte(schema))); err != nil {
			return err
		}
	default:
		return err
	}
	return tx.Commit()
}
func contextAAD(requestID string, c SubjectContext) []byte {
	return []byte(requestID + "\x00" + c.ContextID + "\x00" + c.RequestDigest + "\x00" + c.PrincipalContextRef + "\x00" + strconv.FormatInt(c.ExpiresAt.UnixMicro(), 10))
}

type contextQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *SQLContextStore) read(ctx context.Context, q contextQuerier, query string, args ...any) (StoredContext, error) {
	var result StoredContext
	var raw []byte
	var c SubjectContext
	err := q.QueryRowContext(ctx, query, args...).Scan(&result.RequestID, &c.ContextID, &c.RequestDigest, &c.PrincipalContextRef, &c.ExpiresAt, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrContextMissing
	}
	if err != nil {
		return result, err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return result, ErrVerification
	}
	clear, err := s.aead.Open(nil, raw[:n], raw[n:], contextAAD(result.RequestID, c))
	var payload struct {
		Context         SubjectContext `json:"context"`
		PrincipalDigest string         `json:"principal_digest"`
	}
	if err != nil || json.Unmarshal(clear, &payload) != nil || !digestPattern.MatchString(payload.PrincipalDigest) {
		return StoredContext{}, ErrVerification
	}
	result.Context = payload.Context
	result.PrincipalDigest = payload.PrincipalDigest
	if result.Context.ContextID != c.ContextID || result.Context.RequestDigest != c.RequestDigest || result.Context.PrincipalContextRef != c.PrincipalContextRef || !result.Context.ExpiresAt.Equal(c.ExpiresAt) {
		return StoredContext{}, ErrVerification
	}
	return result, nil
}

const contextSelect = `SELECT request_id,context_id,request_digest,principal_ref,expires_at,payload FROM adapter_subject_contexts WHERE expires_at>clock_timestamp() AND `

func (s *SQLContextStore) Find(ctx context.Context, request, digest, principal string) (StoredContext, error) {
	return s.read(ctx, s.db, contextSelect+`request_id=$1 AND request_digest=$2 AND principal_ref=$3`, request, digest, principal)
}
func (s *SQLContextStore) Resolve(ctx context.Context, id string) (StoredContext, error) {
	return s.read(ctx, s.db, contextSelect+`context_id=$1`, id)
}
func (s *SQLContextStore) Save(ctx context.Context, value StoredContext) (StoredContext, error) {
	c := value.Context
	c.VerifiedAt = c.VerifiedAt.UTC().Truncate(time.Microsecond)
	c.ExpiresAt = c.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !digestPattern.MatchString(value.PrincipalDigest) || !identifier.MatchString(value.RequestID) || !identifier.MatchString(c.ContextID) || !digestPattern.MatchString(c.RequestDigest) || !identifier.MatchString(c.PrincipalContextRef) || !identifier.MatchString(c.SourceMappingRef) || !identifier.MatchString(c.Registry) || !identifier.MatchString(c.VerificationMethod) || c.VerifiedAt.IsZero() || !c.ExpiresAt.After(c.VerifiedAt) {
		return StoredContext{}, ErrVerification
	}
	raw, err := json.Marshal(struct {
		Context         SubjectContext `json:"context"`
		PrincipalDigest string         `json:"principal_digest"`
	}{c, value.PrincipalDigest})
	if err != nil {
		return StoredContext{}, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return StoredContext{}, err
	}
	sealed := s.aead.Seal(nonce, nonce, raw, contextAAD(value.RequestID, c))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredContext{}, err
	}
	defer tx.Rollback()
	// Expired contexts cannot be reused. Remove only the expired tuple so a fresh
	// authentication can create a new context without mutating old live authority.
	if _, err = tx.ExecContext(ctx, `DELETE FROM adapter_subject_contexts WHERE request_id=$1 AND request_digest=$2 AND principal_ref=$3 AND expires_at<=clock_timestamp()`, value.RequestID, c.RequestDigest, c.PrincipalContextRef); err != nil {
		return StoredContext{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO adapter_subject_contexts(context_id,request_id,request_digest,principal_ref,payload,expires_at) SELECT $1,$2,$3,$4,$5,$6 WHERE $6>clock_timestamp() ON CONFLICT(request_id,request_digest,principal_ref) DO NOTHING`, c.ContextID, value.RequestID, c.RequestDigest, c.PrincipalContextRef, sealed, c.ExpiresAt); err != nil {
		return StoredContext{}, err
	}
	result, err := s.read(ctx, tx, contextSelect+`request_id=$1 AND request_digest=$2 AND principal_ref=$3`, value.RequestID, c.RequestDigest, c.PrincipalContextRef)
	if err != nil {
		return StoredContext{}, err
	}
	if result.PrincipalDigest != value.PrincipalDigest || result.Context.SourceMappingRef != c.SourceMappingRef || result.Context.Registry != c.Registry || result.Context.VerificationMethod != c.VerificationMethod {
		return StoredContext{}, ErrVerification
	}
	if err = tx.Commit(); err != nil {
		return StoredContext{}, err
	}
	return result, nil
}
