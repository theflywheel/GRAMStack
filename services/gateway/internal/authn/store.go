package authn

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

var ErrAuthentication = errors.New("authentication transaction unavailable or invalid")

// Store is shared TTL state, not a citizen account database. Payloads are AEAD
// encrypted; lookup/binding values are digested. Every read enforces DB expiry.
// The cleanup worker also physically removes expired rows (D06).
type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

func NewStore(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, errors.New("database and 32-byte authentication encryption key required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{pool, aead}, nil
}
func aad(kind, key, binding string, until time.Time) []byte {
	return []byte(kind + "\x00" + key + "\x00" + binding + "\x00" + strconv.FormatInt(until.UnixMicro(), 10))
}
func (s *Store) seal(kind, key, binding string, value any, until time.Time) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, raw, aad(kind, key, binding, until)), nil
}
func (s *Store) Put(ctx context.Context, kind, key, binding string, value any, until time.Time) error {
	until = until.UTC().Truncate(time.Microsecond)
	kd, bd := cm.Digest([]byte(key)), cm.Digest([]byte(binding))
	raw, err := s.seal(kind, kd, bd, value, until)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `INSERT INTO browser_state(kind,key_digest,binding_digest,payload,expires_at) SELECT $1,$2,$3,$4,$5 WHERE $5>clock_timestamp() ON CONFLICT DO NOTHING`, kind, kd, bd, raw, until)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAuthentication
	}
	return nil
}
func (s *Store) Replace(ctx context.Context, kind, key, binding string, value any, until time.Time) error {
	until = until.UTC().Truncate(time.Microsecond)
	kd, bd := cm.Digest([]byte(key)), cm.Digest([]byte(binding))
	raw, err := s.seal(kind, kd, bd, value, until)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE browser_state SET payload=$4,expires_at=$5 WHERE kind=$1 AND key_digest=$2 AND binding_digest=$3 AND expires_at>clock_timestamp() AND $5>clock_timestamp()`, kind, kd, bd, raw, until)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAuthentication
	}
	return nil
}
func (s *Store) read(ctx context.Context, consume bool, kind, key, binding string, dest any) error {
	kd, bd := cm.Digest([]byte(key)), cm.Digest([]byte(binding))
	var raw []byte
	var until time.Time
	query := `SELECT payload,expires_at FROM browser_state WHERE kind=$1 AND key_digest=$2 AND binding_digest=$3 AND expires_at>clock_timestamp()`
	if consume {
		query = `DELETE FROM browser_state WHERE kind=$1 AND key_digest=$2 AND binding_digest=$3 AND expires_at>clock_timestamp() RETURNING payload,expires_at`
	}
	err := s.pool.QueryRow(ctx, query, kind, kd, bd).Scan(&raw, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthentication
	}
	if err != nil {
		return err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return ErrAuthentication
	}
	clear, err := s.aead.Open(nil, raw[:n], raw[n:], aad(kind, kd, bd, until))
	if err != nil {
		return ErrAuthentication
	}
	if json.Unmarshal(clear, dest) != nil {
		return ErrAuthentication
	}
	return nil
}
func (s *Store) Peek(ctx context.Context, kind, key, binding string, dest any) error {
	return s.read(ctx, false, kind, key, binding, dest)
}
func (s *Store) Take(ctx context.Context, kind, key, binding string, dest any) error {
	return s.read(ctx, true, kind, key, binding, dest)
}
