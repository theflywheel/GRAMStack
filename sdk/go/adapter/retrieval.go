package adapter

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"time"
)

var ErrRetrievalDependency = errors.New("retrieval dependency unavailable")

type RedemptionRequest struct {
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
type Authority interface {
	Redeem(context.Context, RedemptionRequest) (Reservation, error)
}
type EvidenceCustodian interface {
	Record(context.Context, string) (EvidenceAcknowledgement, error)
}
type EvidenceSigning interface {
	Sign(context.Context, EvidenceEvent) (string, error)
}
type RetrievalConfig struct {
	Registry, AdapterID, GatewayIdentity, VerificationMethod string
	Provider                                                 Provider
	Grants                                                   *GrantVerifier
	Contexts                                                 ContextStore
	Attempts                                                 AttemptStore
	Authority                                                Authority
	Custodian                                                EvidenceCustodian
	Signer                                                   EvidenceSigning
	SigningKeys                                              map[string]*ecdsa.PublicKey
	ReleaseProfile                                           string
	MaxConcurrent                                            int
}
type RetrievalService struct {
	config   RetrievalConfig
	resource Resource
	slots    chan struct{}
}

func NewRetrieval(c RetrievalConfig) (*RetrievalService, error) {
	if !identifier.MatchString(c.Registry) || !identifier.MatchString(c.AdapterID) || c.GatewayIdentity == "" || !identifier.MatchString(c.VerificationMethod) || c.Provider == nil || c.Grants == nil || c.Contexts == nil || c.Attempts == nil || c.Authority == nil || c.Custodian == nil || c.Signer == nil || c.MaxConcurrent < 1 || c.MaxConcurrent > 64 || (c.ReleaseProfile != "batch" && c.ReleaseProfile != "synchronous") {
		return nil, errors.New("complete retrieval dependencies and release profile required")
	}
	keys, err := copyPublicKeys(c.SigningKeys)
	if err != nil {
		return nil, err
	}
	c.SigningKeys = keys
	resource := c.Provider.Resource()
	if !identifier.MatchString(resource.Name) || !identifier.MatchString(resource.Version) || !validFields(resource.Fields) {
		return nil, ErrFields
	}
	resource.Fields = slices.Clone(resource.Fields)
	return &RetrievalService{c, resource, make(chan struct{}, c.MaxConcurrent)}, nil
}
func (s *RetrievalService) validate(ctx context.Context, input RetrievalRequest) (Grant, StoredContext, error) {
	now := time.Now().UTC()
	g, err := s.config.Grants.Verify(input.Grant, now)
	if err != nil {
		return g, StoredContext{}, err
	}
	snap := input.Snapshot
	digest, err := SnapshotDigest(snap)
	if err != nil || !identifier.MatchString(input.AttemptID) || input.Digest != digest || g.Digest != digest || g.RequestID != snap.RequestID || g.Consumer != snap.ConsumerID || input.Presenter.Consumer != g.Consumer || input.Presenter.Thumbprint != g.Confirmation.Thumbprint || snap.Request.Registry != s.config.Registry || snap.Request.Resource != s.resource.Name || g.Resource != s.resource.Name || g.Purpose != snap.Request.Purpose || !slices.Equal(g.Fields, snap.Request.Fields) || snap.Request.Access.Mode != "single_use" || snap.Request.Access.ValidForSeconds < 1 || snap.Request.Access.ValidForSeconds > 300 || !identifier.MatchString(snap.Request.RecipientTerms.RetentionPolicyID) || !digestPattern.MatchString(snap.RecipientTermsDigest) || snap.CreatedAt.IsZero() || snap.CreatedAt.After(now) || !snap.ExpiresAt.After(now) || snap.ExpiresAt.Sub(snap.CreatedAt) > 10*time.Minute || time.Unix(g.ExpiresAt, 0).After(snap.ExpiresAt) {
		return Grant{}, StoredContext{}, ErrGrant
	}
	for _, f := range g.Fields {
		if !slices.Contains(s.resource.Fields, f) {
			return Grant{}, StoredContext{}, ErrFields
		}
	}
	subject, err := s.config.Contexts.Resolve(ctx, g.ContextID)
	if err != nil {
		return Grant{}, StoredContext{}, err
	}
	c := subject.Context
	if subject.RequestID != snap.RequestID || c.ContextID != g.ContextID || c.Registry != s.config.Registry || c.RequestDigest != digest || c.PrincipalContextRef != g.Subject || c.VerificationMethod != s.config.VerificationMethod || !identifier.MatchString(c.SourceMappingRef) || c.VerifiedAt.IsZero() || c.VerifiedAt.After(now) || !c.ExpiresAt.After(now) || time.Unix(g.ExpiresAt, 0).After(c.ExpiresAt) {
		return Grant{}, StoredContext{}, ErrGrant
	}
	return g, subject, nil
}
func (s *RetrievalService) retrieve(ctx context.Context, input RetrievalRequest) (response DataResponse, attempt Attempt, retErr error) {
	g, subject, err := s.validate(ctx, input)
	if err != nil {
		return response, attempt, err
	}
	attempt, err = s.config.Attempts.Claim(ctx, Attempt{ID: input.AttemptID, RequestID: g.RequestID, Digest: g.Digest, GrantJTI: g.JTI, GrantDigest: Digest([]byte(input.Grant)), Consumer: g.Consumer, Thumbprint: input.Presenter.Thumbprint})
	if err != nil {
		return response, attempt, err
	}
	// Never resume a claimed attempt by fetching again. Even an unacknowledged
	// reservation or source timeout leaves this local one-fetch slot occupied.
	defer func() {
		if retErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if stopped, e := s.config.Attempts.Advance(cleanup, attempt.ID, attempt.Version, "failed", AttemptUpdate{}); e == nil {
				attempt = stopped
			}
		}
	}()
	advance := func(event string, update AttemptUpdate) error {
		next, e := s.config.Attempts.Advance(ctx, attempt.ID, attempt.Version, event, update)
		if e == nil {
			attempt = next
		}
		return e
	}
	reservation, err := s.config.Authority.Redeem(ctx, RedemptionRequest{input.AttemptID, input.Grant, g.RequestID, g.Digest, input.Presenter})
	if err != nil {
		return response, attempt, err
	}
	if !reservation.First || reservation.AttemptID != input.AttemptID || !identifier.MatchString(reservation.ID) || reservation.ReservedAt.IsZero() {
		return response, attempt, ErrAttemptExists
	}
	if err = advance("admitted", AttemptUpdate{RedemptionID: reservation.ID}); err != nil {
		return response, attempt, err
	}
	if err = advance("source_started", AttemptUpdate{}); err != nil {
		return response, attempt, err
	}
	data, err := ReadSelected(ctx, s.config.Provider, Subject{subject.Context.SourceMappingRef}, g.Fields)
	if err != nil {
		return response, attempt, ErrRetrievalDependency
	}
	raw, err := Canonical(data)
	if err != nil || len(raw) > 1<<20 {
		return response, attempt, ErrRetrievalDependency
	}
	commitment, salt, err := responseCommitment(data)
	if err != nil {
		return response, attempt, ErrRetrievalDependency
	}
	event := EvidenceEvent{EventID: "ev_" + rand.Text(), RequestID: g.RequestID, Digest: g.Digest, ConsentID: g.ConsentID, GrantJTI: g.JTI, AdapterID: s.config.AdapterID, AttemptID: input.AttemptID, RedemptionID: reservation.ID, Fields: slices.Clone(g.Fields), Outcome: "release_ready", ResponseCommitment: commitment, OccurredAt: time.Now().UTC().Truncate(time.Microsecond)}
	signed, err := s.config.Signer.Sign(ctx, event)
	if err != nil {
		return response, attempt, ErrRetrievalDependency
	}
	verified, err := VerifyEvidence(signed, s.config.SigningKeys)
	expected, _ := Canonical(event)
	actual, _ := Canonical(verified)
	if err != nil || !bytes.Equal(expected, actual) {
		return response, attempt, ErrRetrievalDependency
	}
	if err = advance("evidence_prepared", AttemptUpdate{Evidence: &ProtectedEvidence{signed, salt}}); err != nil {
		return response, attempt, err
	}
	ack, err := s.config.Custodian.Record(ctx, signed)
	if err != nil {
		return response, attempt, ErrRetrievalDependency
	}
	if !identifier.MatchString(ack.ReceiptID) || ack.Digest != Digest([]byte(signed)) || ack.AcceptedAt.IsZero() || ack.AcceptedAt.After(time.Now().Add(30*time.Second)) || (ack.AnchorStatus != "anchored" && (s.config.ReleaseProfile != "batch" || ack.AnchorStatus != "pending")) {
		return response, attempt, ErrRetrievalDependency
	}
	if err = advance("evidence_committed", AttemptUpdate{Ack: &ack}); err != nil {
		return response, attempt, err
	}
	return DataResponse{g.RequestID, data, ack.ReceiptID}, attempt, nil
}
func retrievalError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": "Protected retrieval did not release data.", "request_id": rand.Text(), "retryable": status == 429}})
}
func (s *RetrievalService) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Gramstack-Contract-Version", CoreVersion)
		if r.Method != "POST" || r.URL.Path != "/internal/v1/retrievals" {
			http.NotFound(w, r)
			return
		}
		if CheckService(r, s.config.GatewayIdentity) != nil {
			retrievalError(w, 403, "UNTRUSTED_SERVICE")
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || r.Header.Get("Gramstack-Contract-Version") != CoreVersion {
			retrievalError(w, 400, "INVALID_REQUEST")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
		var input RetrievalRequest
		if err != nil || Decode(raw, &input) != nil {
			retrievalError(w, 400, "INVALID_REQUEST")
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			w.Header().Set("Retry-After", "1")
			retrievalError(w, 429, "CAPACITY_UNAVAILABLE")
			return
		}
		result, attempt, err := s.retrieve(r.Context(), input)
		if err != nil {
			status, code := 503, "DEPENDENCY_UNAVAILABLE"
			switch {
			case errors.Is(err, ErrGrant), errors.Is(err, ErrFields), errors.Is(err, ErrContextMissing):
				status, code = 403, "AUTHORIZATION_DENIED"
			case errors.Is(err, ErrGrantExpired):
				status, code = 410, "EXPIRED"
			case errors.Is(err, ErrAttemptExists), errors.Is(err, ErrAttemptConflict):
				status, code = 409, "ATTEMPT_ALREADY_CLAIMED"
			}
			retrievalError(w, status, code)
			return
		}
		raw, err = json.Marshal(result)
		if err != nil {
			retrievalError(w, 503, "RELEASE_UNAVAILABLE")
			return
		}
		started, err := s.config.Attempts.Advance(r.Context(), attempt.ID, attempt.Version, "release_started", AttemptUpdate{})
		if err != nil {
			retrievalError(w, 503, "RELEASE_UNAVAILABLE")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		n, writeErr := w.Write(raw)
		event := "response_written"
		if writeErr != nil || n != len(raw) {
			event = "ambiguous"
		}
		finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// A lost final checkpoint stays RELEASE_ATTEMPTED and is never replayed.
		s.config.Attempts.Advance(finish, started.ID, started.Version, event, AttemptUpdate{})
	})
}
