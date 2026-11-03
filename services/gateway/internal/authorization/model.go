// Package authorization owns current source policy, scoped grants and admission.
// Transport authentication must establish consumers and adapter service identities.
package authorization

import (
	"errors"
	"regexp"
	"slices"
	"time"
)

const GrantType = "gramstack-grant+jwt"
const PolicyType = "gramstack-policy+jws"

var (
	ErrDenied     = errors.New("disclosure is not permitted")
	ErrBinding    = errors.New("invalid authorization binding")
	ErrNotReady   = errors.New("disclosure authority not ready")
	ErrExpired    = errors.New("authorization deadline passed")
	ErrConflict   = errors.New("immutable authorization conflict")
	ErrSigning    = errors.New("grant signing unavailable or invalid")
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	thumbPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

type Policy struct {
	Registry       string   `json:"registry"`
	Version        string   `json:"version"`
	Consumer       string   `json:"consumer_id"`
	Purpose        string   `json:"purpose"`
	Resource       string   `json:"resource"`
	Fields         []string `json:"fields"`
	RequireSubject bool     `json:"require_verified_subject"`
	RequireConsent bool     `json:"require_human_consent"`
	MaxSeconds     int64    `json:"max_grant_seconds"`
	MaxDisclosures int64    `json:"max_disclosures"`
}
type Grant struct {
	Issuer        string   `json:"iss"`
	Audience      string   `json:"aud"`
	Subject       string   `json:"sub"`
	Consumer      string   `json:"client_id"`
	JTI           string   `json:"jti"`
	IssuedAt      int64    `json:"iat"`
	ExpiresAt     int64    `json:"exp"`
	RequestID     string   `json:"request_id"`
	Digest        string   `json:"request_digest"`
	Resource      string   `json:"resource"`
	Fields        []string `json:"fields"`
	Purpose       string   `json:"purpose"`
	ConsentID     string   `json:"consent_ref"`
	ContextID     string   `json:"subject_context_ref"`
	PolicyVersion string   `json:"policy_version"`
	Confirmation  struct {
		Thumbprint string `json:"x5t#S256"`
	} `json:"cnf"`
}
type GrantResponse struct {
	Grant     string    `json:"grant"`
	ExpiresAt time.Time `json:"expires_at"`
	TokenType string    `json:"token_type"`
}
type Presenter struct {
	Thumbprint string `json:"certificate_thumbprint"`
	Consumer   string `json:"authenticated_client_id"`
}
type Redemption struct {
	AttemptID string    `json:"attempt_id"`
	Grant     string    `json:"grant"`
	RequestID string    `json:"request_id"`
	Digest    string    `json:"request_digest"`
	Presenter Presenter `json:"presenter"`
}
type Reservation struct {
	ID         string    `json:"redemption_id"`
	AttemptID  string    `json:"attempt_id"`
	First      bool      `json:"first_redemption"`
	ReservedAt time.Time `json:"reserved_at"`
}

func validFields(fields []string) bool {
	if len(fields) == 0 || len(fields) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !idPattern.MatchString(f) || seen[f] {
			return false
		}
		seen[f] = true
	}
	return true
}
func (p Policy) validate() error {
	for _, id := range []string{p.Registry, p.Version, p.Consumer, p.Purpose, p.Resource} {
		if !idPattern.MatchString(id) {
			return ErrBinding
		}
	}
	if !validFields(p.Fields) || !p.RequireSubject || !p.RequireConsent || p.MaxDisclosures != 1 || p.MaxSeconds < 1 || p.MaxSeconds > 300 {
		return ErrBinding
	}
	return nil
}
func (p Policy) allows(fields []string) bool {
	for _, f := range fields {
		if !slices.Contains(p.Fields, f) {
			return false
		}
	}
	return true
}
func (g Grant) validate() error {
	for _, id := range []string{g.Audience, g.Subject, g.Consumer, g.JTI, g.RequestID, g.Resource, g.Purpose, g.ConsentID, g.ContextID, g.PolicyVersion} {
		if !idPattern.MatchString(id) {
			return ErrBinding
		}
	}
	if !validFields(g.Fields) || !digestPattern.MatchString(g.Digest) || !thumbPattern.MatchString(g.Confirmation.Thumbprint) || g.IssuedAt < 0 || g.ExpiresAt <= g.IssuedAt || g.ExpiresAt-g.IssuedAt > 300 {
		return ErrBinding
	}
	return nil
}
