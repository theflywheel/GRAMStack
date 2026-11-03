package consentmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/gowebpki/jcs"
)

var (
	ErrUnavailable = errors.New("consent verification dependency unavailable")
	ErrBinding     = errors.New("consent request or notice binding mismatch")
	ErrConflict    = errors.New("conflicting immutable consent operation")
	ErrForbidden   = errors.New("principal does not own the consent interaction")
	ErrExpired     = errors.New("consent interaction or verified context expired")
	ErrNotReady    = errors.New("consent approval is not ready for activation")
	identifier     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const NoticeVersion = "gramstack.en.v1"
const ConsentType = "gramstack-consent+jwt"

// Snapshot is the immutable gateway request defined by specs/0.1.0. The service
// recomputes its canonical digest; raw subject hints are not persisted by CM.
type Snapshot struct {
	RequestID            string        `json:"request_id"`
	ConsumerID           string        `json:"consumer_id"`
	CreatedAt            time.Time     `json:"created_at"`
	ExpiresAt            time.Time     `json:"expires_at"`
	RecipientTermsDigest string        `json:"recipient_terms_digest"`
	Request              AccessRequest `json:"request"`
}
type AccessRequest struct {
	Registry    string `json:"registry"`
	Resource    string `json:"resource"`
	SubjectHint struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"subject_hint"`
	Fields  []string `json:"fields"`
	Purpose string   `json:"purpose"`
	Access  struct {
		Mode            string `json:"mode"`
		ValidForSeconds int64  `json:"valid_for_seconds"`
	} `json:"access"`
	RecipientTerms struct {
		RetentionPolicyID string `json:"retention_policy_id"`
	} `json:"recipient_terms"`
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
type Notice struct {
	ConsentRequestID string    `json:"consent_request_id"`
	RequestDigest    string    `json:"request_digest"`
	NoticeDigest     string    `json:"notice_digest"`
	NoticeVersion    string    `json:"notice_version"`
	Language         string    `json:"language"`
	Text             string    `json:"text"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type Decision struct {
	Action        string `json:"decision"`
	RequestDigest string `json:"request_digest"`
	NoticeDigest  string `json:"notice_digest"`
	NoticeVersion string `json:"notice_version"`
	Language      string `json:"language"`
}
type Status struct {
	ConsentID     string    `json:"consent_id"`
	Status        string    `json:"status"`
	StatusVersion int64     `json:"status_version"`
	Use           string    `json:"use"`
	ValidUntil    time.Time `json:"valid_until"`
}
type Artefact struct {
	ConsentID             string    `json:"consent_id"`
	SchemaVersion         string    `json:"schema_version"`
	Issuer                string    `json:"issuer"`
	RequestDigest         string    `json:"request_digest"`
	SubjectContextDigest  string    `json:"subject_context_digest"`
	Consumer              string    `json:"consumer"`
	Provider              string    `json:"provider"`
	Resource              string    `json:"resource"`
	Fields                []string  `json:"fields"`
	Purpose               string    `json:"purpose"`
	Frequency             string    `json:"frequency"`
	RecipientTermsVersion string    `json:"recipient_terms_version"`
	RecipientTermsDigest  string    `json:"recipient_terms_digest"`
	NoticeDigest          string    `json:"notice_digest"`
	ApprovedAt            time.Time `json:"approved_at"`
	ValidUntil            time.Time `json:"valid_until"`
	StatusEndpoint        string    `json:"status_endpoint"`
}

// scope contains just the consent-relevant request projection, never a source
// hint, source mapping or business response.
type scope struct {
	Consumer, Provider, Resource, Purpose string
	Fields                                []string
	TermsVersion, TermsDigest             string
	ValidForSeconds                       int64
	RequestExpiresAt                      time.Time
}

func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func Canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(raw)
}
func SnapshotDigest(snapshot Snapshot) (string, error) {
	raw, err := Canonical(snapshot)
	if err != nil {
		return "", err
	}
	return Digest(raw), nil
}
func validFields(fields []string) bool {
	if len(fields) == 0 || len(fields) > 64 {
		return false
	}
	seen := map[string]bool{}
	for _, field := range fields {
		if !identifier.MatchString(field) || seen[field] {
			return false
		}
		seen[field] = true
	}
	return true
}

// ValidateSnapshot checks structural bounds; it does not establish identity or consent.
func ValidateSnapshot(s Snapshot) error {
	for _, id := range []string{s.RequestID, s.ConsumerID} {
		if !identifier.MatchString(id) {
			return ErrBinding
		}
	}
	if !digestPattern.MatchString(s.RecipientTermsDigest) || s.CreatedAt.IsZero() || !s.ExpiresAt.After(s.CreatedAt) {
		return ErrBinding
	}
	return ValidateAccessRequest(s.Request)
}

// ValidateAccessRequest checks wire structure without treating it as approved scope.
func ValidateAccessRequest(r AccessRequest) error {
	for _, id := range []string{r.Registry, r.Resource, r.Purpose, r.SubjectHint.Type, r.RecipientTerms.RetentionPolicyID} {
		if !identifier.MatchString(id) {
			return ErrBinding
		}
	}
	if !validFields(r.Fields) || r.SubjectHint.Value == "" || len(r.SubjectHint.Value) > 1024 || !utf8.ValidString(r.SubjectHint.Value) || r.Access.Mode != "single_use" || r.Access.ValidForSeconds < 1 || r.Access.ValidForSeconds > 300 {
		return ErrBinding
	}
	return nil
}
func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
