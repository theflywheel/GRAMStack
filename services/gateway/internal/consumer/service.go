package consumer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

var (
	ErrInput    = errors.New("invalid request")
	ErrPolicy   = errors.New("request scope is not permitted")
	ErrConflict = errors.New("idempotency key reused with different request")
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
)

type Policy struct {
	Registry     string   `json:"registry"`
	Resource     string   `json:"resource"`
	Fields       []string `json:"fields"`
	Purposes     []string `json:"purposes"`
	HintTypes    []string `json:"hint_types"`
	TermsVersion string   `json:"terms_version"`
	TermsText    string   `json:"terms_text"`
}
type Config struct {
	Pool          *pgxpool.Pool
	Auth          Authenticator
	PublicOrigin  string
	AllowLoopback bool
	// Per-consumer allowlists and immutable terms are trusted deployment config.
	Consumers map[string][]Policy
}
type Service struct {
	pool     *pgxpool.Pool
	auth     Authenticator
	origin   string
	policies map[string][]Policy
}
type Accepted struct {
	RequestID      string    `json:"request_id"`
	Status         string    `json:"status"`
	StatusURL      string    `json:"status_url"`
	InteractionURL string    `json:"interaction_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}
type Status struct {
	RequestID  string    `json:"request_id"`
	Status     string    `json:"status"`
	Version    int64     `json:"version"`
	NextAction string    `json:"next_action"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func New(c Config) (*Service, error) {
	if c.Pool == nil || c.Auth == nil || !trustedURL(c.PublicOrigin, c.AllowLoopback) || len(c.Consumers) == 0 {
		return nil, errors.New("database, authenticator, public origin and consumer policies required")
	}
	origin, _ := url.Parse(c.PublicOrigin)
	if origin.Path != "" && origin.Path != "/" {
		return nil, ErrInput
	}
	c.PublicOrigin = strings.TrimSuffix(c.PublicOrigin, "/")
	// Copy config so later caller mutation cannot silently widen accepted scope.
	raw, err := json.Marshal(c.Consumers)
	if err != nil {
		return nil, err
	}
	var policies map[string][]Policy
	if err = json.Unmarshal(raw, &policies); err != nil {
		return nil, err
	}
	for client, entries := range policies {
		if !idPattern.MatchString(client) || len(entries) == 0 {
			return nil, ErrInput
		}
		terms := map[string]string{}
		for _, p := range entries {
			if old, ok := terms[p.TermsVersion]; ok && old != p.TermsText {
				return nil, ErrInput
			}
			terms[p.TermsVersion] = p.TermsText
			if !idPattern.MatchString(p.Registry) || !idPattern.MatchString(p.Resource) || !idPattern.MatchString(p.TermsVersion) || p.TermsText == "" || len(p.TermsText) > 65536 || len(p.Fields) == 0 || len(p.Purposes) == 0 || len(p.HintTypes) == 0 {
				return nil, ErrInput
			}
		}
	}
	return &Service{c.Pool, c.Auth, c.PublicOrigin, policies}, nil
}
func (s *Service) accepted(value cm.Snapshot) Accepted {
	return Accepted{value.RequestID, "AUTHENTICATION_PENDING", s.origin + "/v1/access-requests/" + value.RequestID, s.origin + "/interactions/" + value.RequestID, value.ExpiresAt}
}
func (s *Service) Create(ctx context.Context, client, key string, input cm.AccessRequest) (Accepted, error) {
	if !idPattern.MatchString(key) {
		return Accepted{}, ErrInput
	}
	if cm.ValidateAccessRequest(input) != nil {
		return Accepted{}, ErrInput
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	snapshot := cm.Snapshot{RequestID: rand.Text(), ConsumerID: client, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute), Request: input}
	// Bind approved text, not merely a mutable policy label.
	for _, p := range s.policies[client] {
		if p.Registry != input.Registry || p.Resource != input.Resource || p.TermsVersion != input.RecipientTerms.RetentionPolicyID || !slices.Contains(p.Purposes, input.Purpose) || !slices.Contains(p.HintTypes, input.SubjectHint.Type) {
			continue
		}
		allowed := true
		for _, field := range input.Fields {
			if !slices.Contains(p.Fields, field) {
				allowed = false
			}
		}
		if allowed {
			snapshot.RecipientTermsDigest = cm.Digest([]byte(p.TermsText))
			break
		}
	}
	if snapshot.RecipientTermsDigest == "" {
		return Accepted{}, ErrPolicy
	}
	if cm.ValidateSnapshot(snapshot) != nil {
		return Accepted{}, ErrInput
	}
	body, err := cm.Canonical(input)
	if err != nil {
		return Accepted{}, ErrInput
	}
	bodyDigest, keyDigest := cm.Digest(body), cm.Digest([]byte(key))
	var result Accepted
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Serialise only this actor/operation/key, including concurrent first inserts.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", client+":create:"+keyDigest); err != nil {
			return err
		}
		var previousDigest string
		var previous []byte
		err := tx.QueryRow(ctx, `SELECT body_digest,snapshot_bytes FROM accepted_requests WHERE consumer_id=$1 AND idempotency_digest=$2`, client, keyDigest).Scan(&previousDigest, &previous)
		if err == nil {
			if previousDigest != bodyDigest {
				return ErrConflict
			}
			var original cm.Snapshot
			if err = json.Unmarshal(previous, &original); err != nil {
				return err
			}
			result = s.accepted(original)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var dbNow time.Time
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&dbNow); err != nil {
			return err
		}
		snapshot.CreatedAt = dbNow.UTC()
		snapshot.ExpiresAt = dbNow.UTC().Add(10 * time.Minute)
		digest, err := cm.SnapshotDigest(snapshot)
		if err != nil {
			return err
		}
		raw, err := cm.Canonical(snapshot)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO access_requests(request_id,consumer_id,request_digest,status,expires_at) VALUES($1,$2,$3,'AUTHENTICATION_PENDING',$4)`, snapshot.RequestID, client, digest, snapshot.ExpiresAt); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO accepted_requests(request_id,consumer_id,idempotency_digest,body_digest,snapshot_bytes) VALUES($1,$2,$3,$4,$5)`, snapshot.RequestID, client, keyDigest, bodyDigest, raw); err != nil {
			return err
		}
		if err = outbox.Append(ctx, tx, "request", snapshot.RequestID, 1, "request.accepted", map[string]any{"request_id": snapshot.RequestID, "request_digest": digest, "status": "AUTHENTICATION_PENDING"}); err != nil {
			return err
		}
		result = s.accepted(snapshot)
		return nil
	})
	if err != nil {
		return Accepted{}, err
	}
	return result, nil
}
func statusOf(r access.Request) Status {
	next := "none"
	switch r.Status {
	case "AUTHENTICATION_PENDING":
		next = "authenticate"
	case "BINDING_PENDING":
		next = "verify_subject"
	case "CONSENT_PENDING":
		next = "review_consent"
	case "CONSENTED":
		next = "request_grant"
	case "AUTHORIZED":
		next = "retrieve"
	case "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED":
		next = "wait"
	}
	return Status{r.ID, r.Status, r.Version, next, r.ExpiresAt}
}
func (s *Service) Status(ctx context.Context, client, id string) (Status, error) {
	if !idPattern.MatchString(id) {
		return Status{}, access.ErrNotFound
	}
	for range 3 {
		var r access.Request
		var now time.Time
		err := s.pool.QueryRow(ctx, `SELECT request_id,consumer_id,request_digest,status,version,expires_at,clock_timestamp() FROM access_requests WHERE request_id=$1 AND consumer_id=$2`, id, client).Scan(&r.ID, &r.ConsumerID, &r.Digest, &r.Status, &r.Version, &r.ExpiresAt, &now)
		if errors.Is(err, pgx.ErrNoRows) {
			return Status{}, access.ErrNotFound
		}
		if err != nil {
			return Status{}, err
		}
		beforeAdmission := r.Status == "AUTHENTICATION_PENDING" || r.Status == "BINDING_PENDING" || r.Status == "CONSENT_PENDING" || r.Status == "CONSENTED" || r.Status == "AUTHORIZED"
		if beforeAdmission && !now.Before(r.ExpiresAt) {
			advanced, err := (access.Repository{Pool: s.pool}).Advance(ctx, id, r.Version, "expired")
			if errors.Is(err, access.ErrVersion) {
				continue
			}
			if err != nil {
				return Status{}, err
			}
			r = advanced
		}
		return statusOf(r), nil
	}
	return Status{}, ErrUnavailable
}
func (s *Service) Cancel(ctx context.Context, client, id string) (Status, error) {
	if !idPattern.MatchString(id) {
		return Status{}, access.ErrNotFound
	}
	value, err := (access.Repository{Pool: s.pool}).Cancel(ctx, id, client)
	if err != nil {
		return Status{}, err
	}
	return statusOf(value), nil
}

// Resolve supplies only the exact trusted recipient terms version, never client text.
func (s *Service) Resolve(ctx context.Context, client, version string) (cm.Terms, error) {
	if err := ctx.Err(); err != nil {
		return cm.Terms{}, err
	}
	for _, policy := range s.policies[client] {
		if policy.TermsVersion == version {
			return cm.Terms{Version: version, Text: policy.TermsText}, nil
		}
	}
	return cm.Terms{}, cm.ErrBinding
}
