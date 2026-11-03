package application

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/looplab/fsm"
)

//go:embed schema.sql
var schema string

type Store struct {
	pool *pgxpool.Pool
	box  cipher.AEAD
	key  []byte
}
type Application struct {
	ID             string    `json:"id"`
	State          string    `json:"state"`
	PlatformStatus string    `json:"platform_status"`
	InteractionURL string    `json:"interaction_url,omitempty"`
	RequestID      string    `json:"request_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Result         *Result   `json:"result,omitempty"`
}
type record struct {
	Application
	owner, digest            string
	inputCipher              []byte
	version, platformVersion int64
	fetchDeadline            *time.Time
	resultCipher             []byte
}

func NewStore(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, errors.New("product database and 32-byte storage key required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{pool, box, slices.Clone(key)}, nil
}

// Migrate is an explicit operator operation, never implicit server startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x53414b4849)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS sakhi_schema(version integer PRIMARY KEY,checksum text NOT NULL)`); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(schema))
		checksum := hex.EncodeToString(digest[:])
		var current string
		err := tx.QueryRow(ctx, `SELECT checksum FROM sakhi_schema WHERE version=1`).Scan(&current)
		if err == nil {
			if current != checksum {
				return errors.New("product migration checksum changed")
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, schema); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO sakhi_schema VALUES(1,$1)`, checksum)
		return err
	})
}
func (s *Store) Ready(ctx context.Context) error {
	var checksum string
	if err := s.pool.QueryRow(ctx, `SELECT checksum FROM sakhi_schema WHERE version=1`).Scan(&checksum); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(schema))
	if checksum != hex.EncodeToString(digest[:]) {
		return errors.New("product migration mismatch")
	}
	return nil
}
func (s *Store) mac(label, value string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(label + ":" + value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func ownerOf(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
func (s *Store) NewSession(ctx context.Context) (string, string, error) {
	token := rand.Text()
	_, err := s.pool.Exec(ctx, `INSERT INTO sakhi_sessions(owner_hash) VALUES($1)`, ownerOf(token))
	return token, s.mac("csrf", token), err
}
func (s *Store) Session(ctx context.Context, token string) (string, string, error) {
	if len(token) != 26 {
		return "", "", ErrNotFound
	}
	owner := ownerOf(token)
	var found bool
	err := s.pool.QueryRow(ctx, `SELECT true FROM sakhi_sessions WHERE owner_hash=$1 AND expires_at>clock_timestamp()`, owner).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", ErrUnavailable
	}
	return owner, s.mac("csrf", token), nil
}
func (s *Store) seal(id, owner, kind string, raw []byte) []byte {
	nonce := make([]byte, s.box.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return s.box.Seal(nonce, nonce, raw, []byte("sakhi:"+kind+":"+id+":"+owner))
}
func (s *Store) open(id, owner, kind string, raw []byte) ([]byte, error) {
	n := s.box.NonceSize()
	if len(raw) < n {
		return nil, ErrUnavailable
	}
	return s.box.Open(nil, raw[:n], raw[n:], []byte("sakhi:"+kind+":"+id+":"+owner))
}

var columns = `application_id,owner_hash,input_digest,input_cipher,state,version,COALESCE(request_id,''),COALESCE(interaction_url,''),platform_status,platform_version,created_at,expires_at,fetch_deadline,result_cipher`

func scan(row pgx.Row) (record, error) {
	var v record
	err := row.Scan(&v.ID, &v.owner, &v.digest, &v.inputCipher, &v.State, &v.version, &v.RequestID, &v.InteractionURL, &v.PlatformStatus, &v.platformVersion, &v.CreatedAt, &v.ExpiresAt, &v.fetchDeadline, &v.resultCipher)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (s *Store) get(ctx context.Context, owner, id string) (record, error) {
	if !idPattern.MatchString(id) {
		return record{}, ErrNotFound
	}
	return scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM sakhi_applications WHERE application_id=$1 AND owner_hash=$2 AND expires_at>clock_timestamp()`, id, owner))
}
func (s *Store) view(r record) (Application, error) {
	if r.State == "COMPLETE" {
		raw, err := s.open(r.ID, r.owner, "result", r.resultCipher)
		if err != nil {
			return Application{}, ErrUnavailable
		}
		var value Result
		if decode(raw, &value) != nil {
			return Application{}, ErrUnavailable
		}
		r.Result = &value
	}
	return r.Application, nil
}
func (s *Store) input(r record) (Input, error) {
	raw, err := s.open(r.ID, r.owner, "input", r.inputCipher)
	if err != nil {
		return Input{}, ErrUnavailable
	}
	var value Input
	if decode(raw, &value) != nil || value.validate() != nil {
		return Input{}, ErrUnavailable
	}
	return value, nil
}
func (s *Store) create(ctx context.Context, owner, key string, in Input) (record, error) {
	if !idPattern.MatchString(key) || in.validate() != nil {
		return record{}, ErrInput
	}
	raw, _ := json.Marshal(in)
	digest := s.mac("input", owner+":"+string(raw))
	var value record
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var valid bool
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT true,clock_timestamp() FROM sakhi_sessions WHERE owner_hash=$1 AND expires_at>clock_timestamp() FOR UPDATE`, owner).Scan(&valid, &now); err != nil {
			return ErrNotFound
		}
		existing, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM sakhi_applications WHERE owner_hash=$1 AND create_key=$2`, owner, key))
		if err == nil {
			if !now.Before(existing.ExpiresAt) {
				return ErrNotFound
			}
			if existing.digest != digest {
				return ErrConflict
			}
			value = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM sakhi_applications WHERE owner_hash=$1`, owner).Scan(&count); err != nil {
			return err
		}
		if count >= 20 {
			return ErrConflict
		}
		id := rand.Text()
		encrypted := s.seal(id, owner, "input", raw)
		if _, err := tx.Exec(ctx, `INSERT INTO sakhi_applications(application_id,owner_hash,create_key,input_digest,input_cipher,state) VALUES($1,$2,$3,$4,$5,'CONNECTING')`, id, owner, key, digest, encrypted); err != nil {
			return err
		}
		value, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM sakhi_applications WHERE application_id=$1`, id))
		return err
	})
	return value, err
}

var transitions = fsm.Events{
	{Name: "WAITING", Src: []string{"CONNECTING", "READY"}, Dst: "WAITING"},
	{Name: "READY", Src: []string{"WAITING"}, Dst: "READY"},
	{Name: "FETCHING", Src: []string{"READY"}, Dst: "FETCHING"},
	{Name: "COMPLETE", Src: []string{"FETCHING"}, Dst: "COMPLETE"},
	{Name: "UNKNOWN", Src: []string{"WAITING", "READY", "FETCHING", "CANCELLING"}, Dst: "UNKNOWN"},
	{Name: "BLOCKED", Src: []string{"CONNECTING", "WAITING", "READY", "CANCELLING"}, Dst: "BLOCKED"},
	{Name: "EXPIRED", Src: []string{"WAITING", "READY", "CANCELLING"}, Dst: "EXPIRED"},
	{Name: "CANCELLING", Src: []string{"WAITING", "READY"}, Dst: "CANCELLING"},
	{Name: "CANCELLED", Src: []string{"WAITING", "READY", "CANCELLING"}, Dst: "CANCELLED"},
}

func move(ctx context.Context, current, target string) error {
	if current == target {
		return nil
	}
	if fsm.NewFSM(current, transitions, nil).Event(ctx, target) != nil {
		return ErrConflict
	}
	return nil
}
func (s *Store) change(ctx context.Context, owner, id string, fn func(pgx.Tx, record) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM sakhi_applications WHERE application_id=$1 AND owner_hash=$2 AND expires_at>clock_timestamp() FOR UPDATE`, id, owner))
		if err != nil {
			return err
		}
		return fn(tx, r)
	})
}
func (s *Store) attach(ctx context.Context, owner, id string, a Accepted) error {
	return s.change(ctx, owner, id, func(tx pgx.Tx, r record) error {
		if r.RequestID != "" {
			if r.RequestID == a.RequestID {
				return nil
			}
			return ErrConflict
		}
		if err := move(ctx, r.State, "WAITING"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sakhi_applications SET state='WAITING',version=version+1,request_id=$2,interaction_url=$3,platform_status=$4,request_expires_at=$5 WHERE application_id=$1 AND version=$6`, id, a.RequestID, a.InteractionURL, a.Status, a.ExpiresAt, r.version)
		return err
	})
}
func productState(platform string) string {
	switch platform {
	case "CONSENTED", "AUTHORIZED":
		return "READY"
	case "AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING":
		return "WAITING"
	case "EXPIRED":
		return "EXPIRED"
	case "CANCELLED":
		return "CANCELLED"
	case "DENIED", "REJECTED", "WITHDRAWN", "FAILED":
		return "BLOCKED"
	default:
		return "UNKNOWN"
	}
}
func (s *Store) observe(ctx context.Context, owner, id string, status PlatformStatus) error {
	return s.change(ctx, owner, id, func(tx pgx.Tx, r record) error {
		if !slices.Contains([]string{"WAITING", "READY", "CANCELLING"}, r.State) || status.Version < r.platformVersion {
			return nil
		}
		target := productState(status.Status)
		if r.State == "CANCELLING" && (target == "WAITING" || target == "READY") {
			return nil
		}
		if err := move(ctx, r.State, target); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sakhi_applications SET state=$2,platform_status=$3,platform_version=$4,version=version+1 WHERE application_id=$1 AND version=$5`, id, target, status.Status, status.Version, r.version)
		return err
	})
}
func (s *Store) claim(ctx context.Context, owner, id, target string) error {
	return s.change(ctx, owner, id, func(tx pgx.Tx, r record) error {
		if r.State == target {
			return ErrConflict
		}
		if err := move(ctx, r.State, target); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sakhi_applications SET state=$2,fetch_deadline=CASE WHEN $2='FETCHING' THEN clock_timestamp()+interval '45 seconds' ELSE fetch_deadline END,version=version+1 WHERE application_id=$1 AND version=$3`, id, target, r.version)
		return err
	})
}
func (s *Store) finish(ctx context.Context, owner, id string, result *Result) error {
	return s.change(ctx, owner, id, func(tx pgx.Tx, r record) error {
		target := "UNKNOWN"
		var raw []byte
		if result != nil {
			target = "COMPLETE"
			plain, _ := json.Marshal(result)
			raw = s.seal(id, owner, "result", plain)
		}
		if err := move(ctx, r.State, target); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sakhi_applications SET state=$2,result_cipher=$3,version=version+1 WHERE application_id=$1 AND version=$4`, id, target, raw, r.version)
		return err
	})
}

// Maintain never retries a source read. It conservatively closes abandoned
// fetches and physically removes at most 32 expired records and sessions per pass.
func (s *Store) Maintain(ctx context.Context) error {
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT application_id FROM sakhi_applications WHERE state='FETCHING' AND fetch_deadline<=clock_timestamp() ORDER BY fetch_deadline FOR UPDATE SKIP LOCKED LIMIT 32`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := move(ctx, "FETCHING", "UNKNOWN"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE sakhi_applications SET state='UNKNOWN',version=version+1 WHERE application_id=$1 AND state='FETCHING'`, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sakhi_applications WHERE application_id IN (SELECT application_id FROM sakhi_applications WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT 32)`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sakhi_sessions WHERE owner_hash IN (SELECT owner_hash FROM sakhi_sessions s WHERE expires_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM sakhi_applications a WHERE a.owner_hash=s.owner_hash) ORDER BY expires_at LIMIT 32)`)
	return err
}
