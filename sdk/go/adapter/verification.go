package adapter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/gowebpki/jcs"
)

const VerificationVersion = "0.1.0"
const VerificationVersionHeader = "Gramstack-Subject-Verification-Version"

var ErrVerification = errors.New("subject verification unsuccessful")
var ErrContextMissing = errors.New("verified context unavailable or expired")
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Hint struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type AccessRequest struct {
	Registry    string   `json:"registry"`
	Resource    string   `json:"resource"`
	SubjectHint Hint     `json:"subject_hint"`
	Fields      []string `json:"fields"`
	Purpose     string   `json:"purpose"`
	Access      struct {
		Mode            string `json:"mode"`
		ValidForSeconds int64  `json:"valid_for_seconds"`
	} `json:"access"`
	RecipientTerms struct {
		RetentionPolicyID string `json:"retention_policy_id"`
	} `json:"recipient_terms"`
}
type Snapshot struct {
	RequestID            string        `json:"request_id"`
	ConsumerID           string        `json:"consumer_id"`
	CreatedAt            time.Time     `json:"created_at"`
	ExpiresAt            time.Time     `json:"expires_at"`
	RecipientTermsDigest string        `json:"recipient_terms_digest"`
	Request              AccessRequest `json:"request"`
}
type PrincipalContext struct {
	Methods                     []string  `json:"methods,omitempty"`
	Assurance                   string    `json:"assurance,omitempty"`
	ContextID                   string    `json:"context_id"`
	Issuer                      string    `json:"issuer"`
	ProviderSubject             string    `json:"provider_subject"`
	AuthenticationTransactionID string    `json:"authentication_transaction_id"`
	AuthenticatedAt             time.Time `json:"authenticated_at"`
	ExpiresAt                   time.Time `json:"expires_at"`
}
type VerificationRequest struct {
	Snapshot      Snapshot         `json:"snapshot"`
	RequestDigest string           `json:"request_digest"`
	Principal     PrincipalContext `json:"principal"`
}
type SubjectContext struct {
	ContextID           string    `json:"context_id"`
	Registry            string    `json:"registry"`
	RequestDigest       string    `json:"request_digest"`
	PrincipalContextRef string    `json:"principal_context_ref"`
	SourceMappingRef    string    `json:"source_mapping_ref"`
	VerificationMethod  string    `json:"verification_method"`
	VerifiedAt          time.Time `json:"verified_at"`
	ExpiresAt           time.Time `json:"expires_at"`
}
type StoredContext struct {
	PrincipalDigest string
	RequestID       string
	Context         SubjectContext
}
type ContextStore interface {
	Find(context.Context, string, string, string) (StoredContext, error)
	Save(context.Context, StoredContext) (StoredContext, error)
	Resolve(context.Context, string) (StoredContext, error)
}
type SubjectVerifier interface {
	VerifySubject(context.Context, PrincipalContext, Hint) (string, error)
}
type VerificationConfig struct {
	ContractVersion                   string
	Registry, GatewayIdentity, Method string
	Resource                          Resource
	Verifier                          SubjectVerifier
	Store                             ContextStore
}
type VerificationService struct{ config VerificationConfig }

func Canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(raw)
}
func Digest(raw []byte) string {
	value := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(value[:])
}
func SnapshotDigest(snapshot Snapshot) (string, error) {
	raw, err := Canonical(snapshot)
	if err != nil {
		return "", err
	}
	return Digest(raw), nil
}
func Decode(raw []byte, target any) error {
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return err
	}
	if bytes.Equal(canonical, []byte("null")) {
		return ErrVerification
	}
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if err = d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrVerification
	}
	return nil
}
func NewVerification(c VerificationConfig) (*VerificationService, error) {
	if c.ContractVersion != VerificationVersion || !identifier.MatchString(c.Registry) || c.GatewayIdentity == "" || !identifier.MatchString(c.Method) || c.Verifier == nil || c.Store == nil || !identifier.MatchString(c.Resource.Name) || len(c.Resource.Fields) == 0 || len(c.Resource.Fields) > 64 {
		return nil, errors.New("registry, caller, method, verifier, resource and durable context store required")
	}
	seen := map[string]bool{}
	for _, field := range c.Resource.Fields {
		if !identifier.MatchString(field) || seen[field] {
			return nil, errors.New("unique supported resource fields required")
		}
		seen[field] = true
	}
	return &VerificationService{c}, nil
}
func (s *VerificationService) Verify(ctx context.Context, input VerificationRequest) (SubjectContext, error) {
	now := time.Now().UTC()
	snapshot, p := input.Snapshot, input.Principal
	digest, err := SnapshotDigest(snapshot)
	if err != nil || digest != input.RequestDigest || !digestPattern.MatchString(digest) || !identifier.MatchString(snapshot.RequestID) || !identifier.MatchString(snapshot.ConsumerID) || snapshot.Request.Registry != s.config.Registry || snapshot.Request.Resource != s.config.Resource.Name || !identifier.MatchString(snapshot.Request.Purpose) || !identifier.MatchString(snapshot.Request.RecipientTerms.RetentionPolicyID) || !digestPattern.MatchString(snapshot.RecipientTermsDigest) || snapshot.CreatedAt.After(now) || !snapshot.ExpiresAt.After(now) || snapshot.ExpiresAt.Sub(snapshot.CreatedAt) > 10*time.Minute || snapshot.Request.Access.Mode != "single_use" || snapshot.Request.Access.ValidForSeconds < 1 || snapshot.Request.Access.ValidForSeconds > 300 {
		return SubjectContext{}, ErrVerification
	}
	if !identifier.MatchString(p.ContextID) || !identifier.MatchString(p.AuthenticationTransactionID) || p.Issuer == "" || len(p.Issuer) > 2048 || len(p.Assurance) > 256 || len(p.Methods) > 16 || p.ProviderSubject == "" || len(p.ProviderSubject) > 1024 || p.AuthenticatedAt.IsZero() || p.AuthenticatedAt.After(now.Add(30*time.Second)) || now.Sub(p.AuthenticatedAt) > 5*time.Minute || !p.ExpiresAt.After(now) || !identifier.MatchString(snapshot.Request.SubjectHint.Type) || snapshot.Request.SubjectHint.Value == "" || len(snapshot.Request.SubjectHint.Value) > 1024 {
		return SubjectContext{}, ErrVerification
	}
	for _, method := range p.Methods {
		if method == "" || len(method) > 128 {
			return SubjectContext{}, ErrVerification
		}
	}
	seen := map[string]bool{}
	if len(snapshot.Request.Fields) == 0 || len(snapshot.Request.Fields) > 64 {
		return SubjectContext{}, ErrVerification
	}
	for _, field := range snapshot.Request.Fields {
		if seen[field] || !slices.Contains(s.config.Resource.Fields, field) {
			return SubjectContext{}, ErrVerification
		}
		seen[field] = true
	}
	principalRaw, err := Canonical([]string{p.Issuer, p.ProviderSubject})
	if err != nil {
		return SubjectContext{}, ErrVerification
	}
	principalDigest := Digest(principalRaw)
	existing, err := s.config.Store.Find(ctx, snapshot.RequestID, digest, p.ContextID)
	if err == nil {
		c := existing.Context
		if existing.PrincipalDigest != principalDigest || existing.RequestID != snapshot.RequestID || c.RequestDigest != digest || c.PrincipalContextRef != p.ContextID || c.Registry != s.config.Registry || c.VerificationMethod != s.config.Method || !c.ExpiresAt.After(now) || c.ExpiresAt.After(p.ExpiresAt) || c.ExpiresAt.After(snapshot.ExpiresAt) {
			return SubjectContext{}, ErrVerification
		}
		return c, nil
	}
	if !errors.Is(err, ErrContextMissing) {
		return SubjectContext{}, err
	}
	mapping, err := s.config.Verifier.VerifySubject(ctx, p, snapshot.Request.SubjectHint)
	if err != nil {
		return SubjectContext{}, err
	}
	if !identifier.MatchString(mapping) {
		return SubjectContext{}, ErrVerification
	}
	until := now.Add(5 * time.Minute)
	for _, bound := range []time.Time{p.ExpiresAt, snapshot.ExpiresAt} {
		if bound.Before(until) {
			until = bound
		}
	}
	value := SubjectContext{ContextID: "ctx_" + rand.Text(), Registry: s.config.Registry, RequestDigest: digest, PrincipalContextRef: p.ContextID, SourceMappingRef: mapping, VerificationMethod: s.config.Method, VerifiedAt: now.Truncate(time.Microsecond), ExpiresAt: until.Truncate(time.Microsecond)}
	saved, err := s.config.Store.Save(ctx, StoredContext{RequestID: snapshot.RequestID, Context: value, PrincipalDigest: principalDigest})
	return saved.Context, err
}
func (s *VerificationService) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set(VerificationVersionHeader, VerificationVersion)
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" || r.URL.Path != "/internal/v1/subject-verifications" {
			http.NotFound(w, r)
			return
		}
		if CheckService(r, s.config.GatewayIdentity) != nil {
			http.Error(w, "Untrusted service", 403)
			return
		}
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			http.Error(w, "Invalid content type", 400)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		if err != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		var input VerificationRequest
		if Decode(raw, &input) != nil {
			http.Error(w, "Invalid request", 400)
			return
		}
		if r.Header.Get(VerificationVersionHeader) != VerificationVersion {
			http.Error(w, "Unsupported verification contract", 400)
			return
		}
		result, err := s.Verify(r.Context(), input)
		if err != nil {
			if errors.Is(err, ErrVerification) || errors.Is(err, ErrContextMissing) {
				http.Error(w, "Verification unsuccessful", 403)
			} else {
				http.Error(w, "Verification dependency unavailable", 503)
			}
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
