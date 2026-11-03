// Package subjectbinding orchestrates registry-approved ownership verification.
// Registry mechanics and native identity mappings remain behind the adapter.
package subjectbinding

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gowebpki/jcs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/authn"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/discovery"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var ErrDenied = errors.New("registry subject verification unsuccessful")

type Registry struct {
	ContractVersion string `json:"verification_contract_version"`
	ID              string `json:"registry"`
	Origin          string `json:"origin"`
	ServerName      string `json:"server_name"`
	Method          string `json:"verification_method"`
	Demo            bool   `json:"demo"`
	Discovery       *struct {
		Version   string               `json:"version"`
		Origin    string               `json:"origin"`
		Resources []discovery.Resource `json:"resources"`
	} `json:"discovery"`
}
type Config struct {
	ConsentPortalEnabled bool
	Pool                 *pgxpool.Pool
	Auth                 *authn.Service
	Store                *authn.Store
	Registries           []Registry
	CA, Certificate, Key []byte
	AllowDemo            bool
}
type endpoint struct {
	registry Registry
	client   *http.Client
}
type Service struct {
	consentPortalEnabled bool
	pool                 *pgxpool.Pool
	auth                 *authn.Service
	store                *authn.Store
	endpoints            map[string]endpoint
}
type verificationRequest struct {
	Snapshot  cm.Snapshot `json:"snapshot"`
	Digest    string      `json:"request_digest"`
	Principal struct {
		Methods         []string  `json:"methods,omitempty"`
		Assurance       string    `json:"assurance,omitempty"`
		ID              string    `json:"context_id"`
		Issuer          string    `json:"issuer"`
		Subject         string    `json:"provider_subject"`
		TransactionID   string    `json:"authentication_transaction_id"`
		AuthenticatedAt time.Time `json:"authenticated_at"`
		ExpiresAt       time.Time `json:"expires_at"`
	} `json:"principal"`
}

func New(c Config) (*Service, error) {
	if c.Pool == nil || c.Auth == nil || c.Store == nil || len(c.Registries) == 0 {
		return nil, errors.New("binding dependencies required")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(c.CA) {
		return nil, errors.New("registry service CA required")
	}
	pair, err := tls.X509KeyPair(c.Certificate, c.Key)
	if err != nil {
		return nil, errors.New("registry client certificate required")
	}
	s := &Service{c.ConsentPortalEnabled, c.Pool, c.Auth, c.Store, map[string]endpoint{}}
	for _, registry := range c.Registries {
		u, err := url.Parse(registry.Origin)
		if err != nil || registry.ContractVersion != "0.1.0" || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !identifier.MatchString(registry.ID) || registry.ServerName == "" || !identifier.MatchString(registry.Method) || registry.Demo && !c.AllowDemo || strings.HasPrefix(registry.Method, "demo.") && !registry.Demo {
			return nil, errors.New("untrusted registry verification descriptor")
		}
		if _, exists := s.endpoints[registry.ID]; exists {
			return nil, errors.New("duplicate registry verification descriptor")
		}
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}, ServerName: registry.ServerName}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		registry.Origin = strings.TrimSuffix(registry.Origin, "/")
		s.endpoints[registry.ID] = endpoint{registry, client}
	}
	return s, nil
}
func (s *Service) Snapshot(ctx context.Context, id string) (cm.Snapshot, string, int64, error) {
	var snapshot cm.Snapshot
	var raw []byte
	var status, digest string
	var version int64
	var until, now time.Time
	err := s.pool.QueryRow(ctx, `SELECT a.snapshot_bytes,r.request_digest,r.status,r.version,r.expires_at,clock_timestamp() FROM accepted_requests a JOIN access_requests r USING(request_id) WHERE r.request_id=$1`, id).Scan(&raw, &digest, &status, &version, &until, &now)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return snapshot, "", 0, ErrDenied
		}
		return snapshot, "", 0, cm.ErrUnavailable
	}
	if !now.Before(until) || json.Unmarshal(raw, &snapshot) != nil {
		return snapshot, "", 0, ErrDenied
	}
	actual, err := cm.SnapshotDigest(snapshot)
	if err != nil || actual != digest {
		return snapshot, "", 0, ErrDenied
	}
	return snapshot, status, version, nil
}
func (s *Service) verify(ctx context.Context, id string, session authn.Session) (cm.SubjectContext, error) {
	snapshot, status, version, err := s.Snapshot(ctx, id)
	if err != nil {
		return cm.SubjectContext{}, err
	}
	if status == "CONSENT_PENDING" || status == "CONSENTED" || status == "AUTHORIZED" {
		var reference string
		if s.store.Peek(ctx, "subject.request", id, "server", &reference) != nil {
			return cm.SubjectContext{}, ErrDenied
		}
		value, err := s.Resolve(ctx, reference)
		if err != nil || value.PrincipalContextRef != session.PrincipalRef {
			return cm.SubjectContext{}, ErrDenied
		}
		return value, nil
	}
	if status != "BINDING_PENDING" {
		return cm.SubjectContext{}, ErrDenied
	}
	principal, err := s.auth.Principal(ctx, id, session.PrincipalRef)
	if err != nil || !time.Now().Before(principal.ExpiresAt) {
		return cm.SubjectContext{}, ErrDenied
	}
	endpoint, ok := s.endpoints[snapshot.Request.Registry]
	if !ok {
		return cm.SubjectContext{}, ErrDenied
	}
	input := verificationRequest{Snapshot: snapshot, Digest: mustDigest(snapshot)}
	input.Principal.ID = principal.Reference
	input.Principal.Issuer = principal.Issuer
	input.Principal.Subject = principal.Subject
	input.Principal.TransactionID = principal.AuthenticationTransactionID
	input.Principal.AuthenticatedAt = principal.AuthenticatedAt
	input.Principal.ExpiresAt = principal.ExpiresAt
	input.Principal.Methods = principal.Methods
	input.Principal.Assurance = principal.Assurance
	raw, err := json.Marshal(input)
	if err != nil {
		return cm.SubjectContext{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.registry.Origin+"/internal/v1/subject-verifications", bytes.NewReader(raw))
	if err != nil {
		return cm.SubjectContext{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gramstack-Subject-Verification-Version", endpoint.registry.ContractVersion)
	response, err := endpoint.client.Do(req)
	if err != nil {
		return cm.SubjectContext{}, cm.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == 403 || response.StatusCode == 404 {
		return cm.SubjectContext{}, ErrDenied
	}
	if response.StatusCode != 200 || response.Header.Get("Gramstack-Subject-Verification-Version") != endpoint.registry.ContractVersion {
		return cm.SubjectContext{}, cm.ErrUnavailable
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return cm.SubjectContext{}, ErrDenied
	}
	if _, err = jcs.Transform(raw); err != nil {
		return cm.SubjectContext{}, ErrDenied
	}
	var value cm.SubjectContext
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return value, ErrDenied
	}
	if !validContext(value, input, endpoint.registry) {
		return cm.SubjectContext{}, ErrDenied
	}
	// Network verification does not hold a database lock. Recheck request/version
	// and commit both context and FSM transition together, so cancellation wins safely.
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := access.AdvanceTx(ctx, tx, id, version, "subject_verified"); err != nil {
			return err
		}
		if err := s.store.PutTx(ctx, tx, "subject.context", value.ContextID, "server", value, value.ExpiresAt); err != nil {
			return err
		}
		return s.store.PutTx(ctx, tx, "subject.request", id, "server", value.ContextID, value.ExpiresAt)
	})
	if err != nil {
		// Concurrent identical verification may already have committed the same context.
		var reference string
		if s.store.Peek(ctx, "subject.request", id, "server", &reference) == nil && reference == value.ContextID {
			current, resolveErr := s.Resolve(ctx, reference)
			snapshot, status, _, snapshotErr := s.Snapshot(ctx, id)
			if resolveErr == nil && snapshotErr == nil && (status == "CONSENT_PENDING" || status == "CONSENTED" || status == "AUTHORIZED") && mustDigest(snapshot) == current.RequestDigest && current.PrincipalContextRef == session.PrincipalRef {
				return current, nil
			}
		}
		_, status, _, stateErr := s.Snapshot(ctx, id)
		if stateErr != nil {
			return cm.SubjectContext{}, stateErr
		}
		if status != "BINDING_PENDING" {
			return cm.SubjectContext{}, ErrDenied
		}
		return cm.SubjectContext{}, cm.ErrUnavailable
	}
	return value, nil
}
func mustDigest(s cm.Snapshot) string { value, _ := cm.SnapshotDigest(s); return value }
func validContext(c cm.SubjectContext, input verificationRequest, r Registry) bool {
	now := time.Now()
	return identifier.MatchString(c.ContextID) && c.Registry == r.ID && c.RequestDigest == input.Digest && c.PrincipalContextRef == input.Principal.ID && identifier.MatchString(c.SourceMappingRef) && c.VerificationMethod == r.Method && !c.VerifiedAt.IsZero() && !c.VerifiedAt.After(now.Add(30*time.Second)) && now.Sub(c.VerifiedAt) <= 5*time.Minute && c.ExpiresAt.After(now) && !c.ExpiresAt.After(input.Snapshot.ExpiresAt) && !c.ExpiresAt.After(input.Principal.ExpiresAt) && c.ExpiresAt.Sub(c.VerifiedAt) <= 5*time.Minute
}
func (s *Service) Resolve(ctx context.Context, reference string) (cm.SubjectContext, error) {
	var value cm.SubjectContext
	if err := s.store.Peek(ctx, "subject.context", reference, "server", &value); err != nil {
		if errors.Is(err, authn.ErrAuthentication) {
			return value, cm.ErrBinding
		}
		return value, cm.ErrUnavailable
	}
	return value, nil
}
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		id := r.PathValue("id")
		session, err := s.auth.Session(r, id)
		if err != nil {
			http.Error(w, "Session required", 403)
			return
		}
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/x-www-form-urlencoded" {
			http.Error(w, "Invalid interaction", 400)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || len(r.PostForm) != 1 || len(r.PostForm["csrf"]) != 1 || s.auth.VerifyCSRF(r, session, r.PostForm.Get("csrf")) != nil {
			http.Error(w, "Invalid interaction", 403)
			return
		}
		value, err := s.verify(r.Context(), id, session)
		if err != nil {
			if errors.Is(err, cm.ErrUnavailable) {
				http.Error(w, "Verification service unavailable", 503)
			} else {
				http.Error(w, "Registry subject verification unsuccessful", 403)
			}
			return
		}
		if s.consentPortalEnabled && strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/interactions/"+url.PathEscape(id)+"/consent", http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": id, "status": "CONSENT_PENDING", "subject_context_ref": value.ContextID, "next_action": "review_consent"})
	})
}

// ForRequest resolves the opaque subject context only for its bound principal.
func (s *Service) ForRequest(ctx context.Context, id, principal string) (string, error) {
	var reference string
	if err := s.store.Peek(ctx, "subject.request", id, "server", &reference); err != nil {
		if errors.Is(err, authn.ErrAuthentication) {
			return "", cm.ErrBinding
		}
		return "", cm.ErrUnavailable
	}
	value, err := s.Resolve(ctx, reference)
	if err != nil {
		return "", err
	}
	if value.PrincipalContextRef != principal {
		return "", cm.ErrForbidden
	}
	return reference, nil
}
