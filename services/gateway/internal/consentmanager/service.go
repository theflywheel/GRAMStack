package consentmanager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

var ErrSigning = errors.New("consent signing is unavailable or returned an invalid artefact")

type SubjectResolver interface {
	Resolve(context.Context, string) (SubjectContext, error)
}
type Terms struct{ Version, Text string }
type TermsResolver interface {
	Resolve(context.Context, string, string) (Terms, error)
}
type Config struct {
	Pool              *pgxpool.Pool
	Subjects          SubjectResolver
	Terms             TermsResolver
	Signer            Signer
	Verifier          *Verifier
	InteractionOrigin string
}
type Service struct{ config Config }
type CreateRequest struct {
	Snapshot                                   Snapshot
	RequestDigest, SubjectContextRef, Language string
}
type Activation struct {
	Status Status
	JWS    string
}

func New(config Config) (*Service, error) {
	if config.Pool == nil || config.Subjects == nil || config.Terms == nil || config.Signer == nil || config.Verifier == nil || !trustedOrigin(config.InteractionOrigin) {
		return nil, errors.New("consent database, trusted subject/terms resolvers, signer, verifier and HTTPS interaction origin required")
	}
	config.InteractionOrigin = strings.TrimSuffix(config.InteractionOrigin, "/")
	return &Service{config}, nil
}

type row struct {
	status                                  Status
	requestID, digest, rawStatus            string
	scope                                   scope
	scopeBytes                              []byte
	subjectRef, subjectDigest, principalRef string
	subjectExpiresAt                        time.Time
	notice                                  Notice
}
type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func load(ctx context.Context, q queryer, id string, locked bool) (row, error) {
	var result row
	query := `SELECT c.consent_id,c.request_id,c.request_digest,c.status,c.status_version,c.use_state,c.valid_until,r.scope_bytes,r.subject_context_ref,r.subject_context_digest,r.principal_context_ref,r.subject_expires_at,n.version,n.language,n.notice_text,n.notice_digest,n.expires_at FROM consent_state c JOIN consent_requests r USING(consent_id) JOIN consent_notices n USING(consent_id) WHERE c.consent_id=$1`
	if locked {
		query += " FOR UPDATE OF c"
	}
	err := q.QueryRow(ctx, query, id).Scan(&result.status.ConsentID, &result.requestID, &result.digest, &result.rawStatus, &result.status.StatusVersion, &result.status.Use, &result.status.ValidUntil, &result.scopeBytes, &result.subjectRef, &result.subjectDigest, &result.principalRef, &result.subjectExpiresAt, &result.notice.NoticeVersion, &result.notice.Language, &result.notice.Text, &result.notice.NoticeDigest, &result.notice.ExpiresAt)
	if err != nil {
		return row{}, err
	}
	if err = json.Unmarshal(result.scopeBytes, &result.scope); err != nil {
		return row{}, err
	}
	result.notice.ConsentRequestID = id
	result.notice.RequestDigest = result.digest
	result.status.Status = result.rawStatus
	if result.rawStatus == "ACTIVE" && result.status.Use == "RESERVED" {
		result.status.Status = "EXHAUSTED"
	}
	return result, nil
}
func databaseTime(ctx context.Context, q queryer) (time.Time, error) {
	var now time.Time
	err := q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
	return now.UTC(), err
}

// Create is called by the authenticated gateway after subject verification.
// Subject and terms bindings are independently checked through trusted resolvers.
func (s *Service) Create(ctx context.Context, input CreateRequest) (Notice, error) {
	if ValidateSnapshot(input.Snapshot) != nil || !identifier.MatchString(input.SubjectContextRef) {
		return Notice{}, ErrBinding
	}
	if input.Language == "" {
		input.Language = "en"
	}
	if input.Language != "en" {
		return Notice{}, errors.New("unsupported notice language")
	}
	digest, err := SnapshotDigest(input.Snapshot)
	if err != nil || digest != input.RequestDigest {
		return Notice{}, ErrBinding
	}
	subject, err := s.config.Subjects.Resolve(ctx, input.SubjectContextRef)
	if err != nil {
		if errors.Is(err, ErrBinding) {
			return Notice{}, ErrBinding
		}
		return Notice{}, ErrUnavailable
	}
	if subject.ContextID != input.SubjectContextRef || subject.Registry != input.Snapshot.Request.Registry || subject.RequestDigest != digest || !identifier.MatchString(subject.PrincipalContextRef) || !identifier.MatchString(subject.SourceMappingRef) || !identifier.MatchString(subject.VerificationMethod) || subject.VerifiedAt.IsZero() || !subject.ExpiresAt.After(subject.VerifiedAt) {
		return Notice{}, ErrBinding
	}
	subjectRaw, err := Canonical(subject)
	if err != nil {
		return Notice{}, err
	}
	subjectDigest := Digest(subjectRaw)
	terms, err := s.config.Terms.Resolve(ctx, input.Snapshot.ConsumerID, input.Snapshot.Request.RecipientTerms.RetentionPolicyID)
	if err != nil {
		if errors.Is(err, ErrBinding) {
			return Notice{}, ErrBinding
		}
		return Notice{}, ErrUnavailable
	}
	if terms.Version != input.Snapshot.Request.RecipientTerms.RetentionPolicyID || terms.Text == "" || len(terms.Text) > 64*1024 || !utf8.ValidString(terms.Text) || Digest([]byte(terms.Text)) != input.Snapshot.RecipientTermsDigest {
		return Notice{}, ErrBinding
	}
	selected := scope{Consumer: input.Snapshot.ConsumerID, Provider: input.Snapshot.Request.Registry, Resource: input.Snapshot.Request.Resource, Purpose: input.Snapshot.Request.Purpose, Fields: input.Snapshot.Request.Fields, TermsVersion: terms.Version, TermsDigest: input.Snapshot.RecipientTermsDigest, ValidForSeconds: input.Snapshot.Request.Access.ValidForSeconds, RequestExpiresAt: input.Snapshot.ExpiresAt}
	scopeBytes, err := Canonical(selected)
	if err != nil {
		return Notice{}, err
	}
	deadline := earlier(input.Snapshot.ExpiresAt, subject.ExpiresAt)
	text := fmt.Sprintf("You authorise %s to receive data from %s.\nResource: %s\nFields: %s\nPurpose: %s\nAccess: once, for up to %d seconds after approval, no later than %s.\nRecipient terms (%s):\n%s\nWithdrawal prevents new retrieval admissions; it cannot recall a disclosure already admitted. Return to this interaction to withdraw: %s/interactions/%s", selected.Consumer, selected.Provider, selected.Resource, strings.Join(selected.Fields, ", "), selected.Purpose, selected.ValidForSeconds, deadline.UTC().Format(time.RFC3339Nano), terms.Version, terms.Text, s.config.InteractionOrigin, url.PathEscape(input.Snapshot.RequestID))
	notice := Notice{ConsentRequestID: rand.Text(), RequestDigest: digest, NoticeDigest: Digest([]byte(text)), NoticeVersion: NoticeVersion, Language: input.Language, Text: text, ExpiresAt: deadline}
	err = pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		// Lock the accepted request so concurrent creates and lifecycle changes have
		// a single ordering; the stored digest binds the entire submitted snapshot.
		var owner, acceptedDigest, status string
		var expires time.Time
		if err := tx.QueryRow(ctx, `SELECT consumer_id,request_digest,status,expires_at FROM access_requests WHERE request_id=$1 FOR UPDATE`, input.Snapshot.RequestID).Scan(&owner, &acceptedDigest, &status, &expires); err != nil {
			return err
		}
		if owner != selected.Consumer || acceptedDigest != digest || !expires.Equal(input.Snapshot.ExpiresAt) {
			return ErrBinding
		}
		var existingID string
		err := tx.QueryRow(ctx, "SELECT consent_id FROM consent_state WHERE request_id=$1", input.Snapshot.RequestID).Scan(&existingID)
		if err == nil {
			existing, err := load(ctx, tx, existingID, false)
			if err != nil {
				return err
			}
			if existing.digest != digest || !bytes.Equal(existing.scopeBytes, scopeBytes) || existing.subjectRef != subject.ContextID || existing.subjectDigest != subjectDigest || existing.principalRef != subject.PrincipalContextRef || existing.notice.NoticeVersion != notice.NoticeVersion || existing.notice.Language != notice.Language || existing.notice.NoticeDigest != notice.NoticeDigest {
				return ErrConflict
			}
			notice = existing.notice
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if status != "CONSENT_PENDING" {
			return ErrNotReady
		}
		now, err := databaseTime(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(deadline) || subject.VerifiedAt.After(now) {
			return ErrExpired
		}
		if _, err = tx.Exec(ctx, `INSERT INTO consent_state(consent_id,request_id,request_digest,valid_until) VALUES($1,$2,$3,$4)`, notice.ConsentRequestID, input.Snapshot.RequestID, digest, deadline); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO consent_requests(consent_id,scope_bytes,subject_context_ref,subject_context_digest,principal_context_ref,subject_expires_at,recipient_terms_text) VALUES($1,$2,$3,$4,$5,$6,$7)`, notice.ConsentRequestID, scopeBytes, subject.ContextID, subjectDigest, subject.PrincipalContextRef, subject.ExpiresAt, terms.Text); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO consent_notices(consent_id,version,language,notice_text,notice_digest,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, notice.ConsentRequestID, notice.NoticeVersion, notice.Language, notice.Text, notice.NoticeDigest, deadline)
		return err
	})
	if err != nil {
		return Notice{}, err
	}
	return notice, nil
}

// GetNotice and Decide accept only a principal reference derived by the first-
// party session layer. Browser-supplied identity strings are never authoritative.
// The HTTP layer must enforce session authentication and CSRF before calling Decide.
func (s *Service) GetNotice(ctx context.Context, id, principal string) (Notice, error) {
	value, err := load(ctx, s.config.Pool, id, false)
	if err != nil {
		return Notice{}, err
	}
	if principal == "" || principal != value.principalRef {
		return Notice{}, ErrForbidden
	}
	return value.notice, nil
}
func (s *Service) Status(ctx context.Context, id string) (Status, error) {
	value, err := load(ctx, s.config.Pool, id, false)
	return value.status, err
}
func (s *Service) Decide(ctx context.Context, id, principal string, decision Decision) (Status, error) {
	if decision.Action != "approve" && decision.Action != "reject" {
		return Status{}, ErrBinding
	}
	var result Status
	err := pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		value, err := load(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if principal == "" || principal != value.principalRef {
			return ErrForbidden
		}
		if decision.RequestDigest != value.digest || decision.NoticeDigest != value.notice.NoticeDigest || decision.NoticeVersion != value.notice.NoticeVersion || decision.Language != value.notice.Language {
			return ErrBinding
		}
		var prior string
		err = tx.QueryRow(ctx, "SELECT action FROM consent_decisions WHERE consent_id=$1", id).Scan(&prior)
		if err == nil {
			if prior != decision.Action {
				return ErrConflict
			}
			result = value.status
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		now, err := databaseTime(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(value.status.ValidUntil) || !now.Before(value.notice.ExpiresAt) || !now.Before(value.subjectExpiresAt) {
			return ErrExpired
		}
		next, err := Transition(ctx, value.rawStatus, decision.Action)
		if err != nil {
			return err
		}
		until := value.status.ValidUntil
		if decision.Action == "approve" {
			until = earlier(value.status.ValidUntil, earlier(now.Add(time.Duration(value.scope.ValidForSeconds)*time.Second), earlier(value.scope.RequestExpiresAt, value.subjectExpiresAt)))
		}
		if _, err = tx.Exec(ctx, `INSERT INTO consent_decisions(consent_id,action,principal_context_ref,request_digest,notice_digest,notice_version,language,decided_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, decision.Action, principal, decision.RequestDigest, decision.NoticeDigest, decision.NoticeVersion, decision.Language, now); err != nil {
			return err
		}
		result = value.status
		result.Status = next
		result.StatusVersion++
		result.ValidUntil = until
		if _, err = tx.Exec(ctx, `UPDATE consent_state SET status=$2,status_version=$3,valid_until=$4 WHERE consent_id=$1`, id, next, result.StatusVersion, until); err != nil {
			return err
		}
		event := "consent.rejected"
		if decision.Action == "approve" {
			event = "consent.approval_recorded"
		}
		return outbox.Append(ctx, tx, "consent", id, result.StatusVersion, event, map[string]any{"type": event, "consent_id": id, "access_request_id": value.requestID, "request_digest": value.digest})
	})
	if err != nil {
		return Status{}, err
	}
	return result, nil
}

// Activate is a trusted worker operation. Signing occurs outside the SQL lock;
// final activation atomically stores the verified artefact, status and outbox.
func (s *Service) Activate(ctx context.Context, id string) (Activation, error) {
	value, err := load(ctx, s.config.Pool, id, false)
	if err != nil {
		return Activation{}, err
	}
	var existing string
	err = s.config.Pool.QueryRow(ctx, "SELECT jws FROM consent_artefacts WHERE consent_id=$1", id).Scan(&existing)
	if err == nil {
		current, loadErr := load(ctx, s.config.Pool, id, false)
		if loadErr != nil {
			return Activation{}, loadErr
		}
		return Activation{current.status, existing}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Activation{}, err
	}
	if value.rawStatus != "APPROVAL_RECORDED" {
		return Activation{}, ErrNotReady
	}
	var approved time.Time
	var action string
	if err = s.config.Pool.QueryRow(ctx, "SELECT action,decided_at FROM consent_decisions WHERE consent_id=$1", id).Scan(&action, &approved); err != nil {
		return Activation{}, err
	}
	if action != "approve" {
		return Activation{}, ErrNotReady
	}
	now, err := databaseTime(ctx, s.config.Pool)
	if err != nil {
		return Activation{}, err
	}
	if !now.Before(value.status.ValidUntil) {
		return Activation{}, ErrExpired
	}
	approved = approved.UTC()
	artefact := Artefact{ConsentID: id, SchemaVersion: "gramstack.consent.v1", Issuer: s.config.Verifier.issuer, RequestDigest: value.digest, SubjectContextDigest: value.subjectDigest, Consumer: value.scope.Consumer, Provider: value.scope.Provider, Resource: value.scope.Resource, Fields: value.scope.Fields, Purpose: value.scope.Purpose, Frequency: "ONCE", RecipientTermsVersion: value.scope.TermsVersion, RecipientTermsDigest: value.scope.TermsDigest, NoticeDigest: value.notice.NoticeDigest, ApprovedAt: approved, ValidUntil: value.status.ValidUntil, StatusEndpoint: s.config.Verifier.statusEndpoint(id)}
	payload, err := Canonical(artefact)
	if err != nil {
		return Activation{}, err
	}
	compact, err := s.config.Signer.Sign(ctx, payload)
	if err != nil {
		return Activation{}, ErrSigning
	}
	now, err = databaseTime(ctx, s.config.Pool)
	if err != nil {
		return Activation{}, err
	}
	_, verified, err := s.config.Verifier.Verify(compact, now)
	if err != nil || !bytes.Equal(payload, verified) {
		return Activation{}, ErrSigning
	}
	var result Activation
	err = pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		current, err := load(ctx, tx, id, true)
		if err != nil {
			return err
		}
		var stored string
		err = tx.QueryRow(ctx, "SELECT jws FROM consent_artefacts WHERE consent_id=$1", id).Scan(&stored)
		if err == nil {
			result = Activation{current.status, stored}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if current.rawStatus != "APPROVAL_RECORDED" || current.status.StatusVersion != value.status.StatusVersion || !current.status.ValidUntil.Equal(value.status.ValidUntil) {
			return ErrConflict
		}
		now, err := databaseTime(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(current.status.ValidUntil) {
			return ErrExpired
		}
		next, err := Transition(ctx, current.rawStatus, "activate")
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO consent_artefacts(consent_id,payload_bytes,jws) VALUES($1,$2,$3)`, id, payload, compact); err != nil {
			return err
		}
		current.status.Status = next
		current.status.StatusVersion++
		if _, err = tx.Exec(ctx, `UPDATE consent_state SET status=$2,status_version=$3 WHERE consent_id=$1`, id, next, current.status.StatusVersion); err != nil {
			return err
		}
		if err = outbox.Append(ctx, tx, "consent", id, current.status.StatusVersion, "consent.active", map[string]any{"type": "consent.active", "consent_id": id, "access_request_id": current.requestID, "request_digest": current.digest}); err != nil {
			return err
		}
		result = Activation{current.status, compact}
		return nil
	})
	if err != nil {
		return Activation{}, err
	}
	return result, nil
}

// GetArtefact is a read-only internal service operation; retrieving an existing
// receipt must not implicitly activate a pending consent or renew its validity.
func (s *Service) GetArtefact(ctx context.Context, id string) (string, error) {
	var compact string
	err := s.config.Pool.QueryRow(ctx, "SELECT jws FROM consent_artefacts WHERE consent_id=$1", id).Scan(&compact)
	return compact, err
}

var ErrNotDue = errors.New("consent expiry deadline not reached")

// Expire is an idempotent deadline-worker operation. It uses the same consent
// row lock as withdrawal and redemption and never restores a consumed use.
func (s *Service) Expire(ctx context.Context, id string) (Status, error) {
	var result Status
	err := pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		value, err := load(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if value.rawStatus == "EXPIRED" || value.rawStatus == "WITHDRAWN" || value.rawStatus == "REJECTED" {
			result = value.status
			return nil
		}
		now, err := databaseTime(ctx, tx)
		if err != nil {
			return err
		}
		if now.Before(value.status.ValidUntil) {
			return ErrNotDue
		}
		next, err := Transition(ctx, value.rawStatus, "expire")
		if err != nil {
			return err
		}
		result = value.status
		result.Status = next
		result.StatusVersion++
		if _, err = tx.Exec(ctx, "UPDATE consent_state SET status=$2,status_version=$3 WHERE consent_id=$1", id, next, result.StatusVersion); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, "consent", id, result.StatusVersion, "consent.expired", map[string]any{"type": "consent.expired", "consent_id": id, "access_request_id": value.requestID, "request_digest": value.digest})
	})
	if err != nil {
		return Status{}, err
	}
	return result, nil
}
