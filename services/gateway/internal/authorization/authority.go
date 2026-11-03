package authorization

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type authority struct {
	request                                                                        access.Request
	snapshot                                                                       cm.Snapshot
	consentID, status, use, contextID, subjectDigest, principal, noticeDigest, jws string
	consentVersion                                                                 int64
	validUntil                                                                     time.Time
	payload                                                                        []byte
}

func readRequest(ctx context.Context, q queryer, id, owner string, lock bool) (access.Request, error) {
	var r access.Request
	sql := `SELECT request_id,consumer_id,request_digest,status,version,expires_at FROM access_requests WHERE request_id=$1`
	args := []any{id}
	if owner != "" {
		sql += " AND consumer_id=$2"
		args = append(args, owner)
	}
	if lock {
		sql += " FOR UPDATE"
	}
	err := q.QueryRow(ctx, sql, args...).Scan(&r.ID, &r.ConsumerID, &r.Digest, &r.Status, &r.Version, &r.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, access.ErrNotFound
	}
	return r, err
}
func loadAuthority(ctx context.Context, q queryer, r access.Request, lock bool) (authority, error) {
	a := authority{request: r}
	var snapshot []byte
	if err := q.QueryRow(ctx, `SELECT snapshot_bytes FROM accepted_requests WHERE request_id=$1`, r.ID).Scan(&snapshot); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return a, ErrNotReady
		}
		return a, err
	}
	if decode(snapshot, &a.snapshot) != nil || cm.ValidateSnapshot(a.snapshot) != nil {
		return a, ErrBinding
	}
	digest, err := cm.SnapshotDigest(a.snapshot)
	if err != nil || digest != r.Digest || a.snapshot.RequestID != r.ID || a.snapshot.ConsumerID != r.ConsumerID || !a.snapshot.ExpiresAt.Equal(r.ExpiresAt) {
		return a, ErrBinding
	}
	sql := `SELECT c.consent_id,c.status,c.use_state,c.status_version,c.valid_until,r.subject_context_ref,r.subject_context_digest,r.principal_context_ref,n.notice_digest,a.jws,a.payload_bytes FROM consent_state c JOIN consent_requests r USING(consent_id) JOIN consent_notices n USING(consent_id) JOIN consent_artefacts a USING(consent_id) WHERE c.request_id=$1`
	if lock {
		sql += " FOR UPDATE OF c"
	}
	err = q.QueryRow(ctx, sql, r.ID).Scan(&a.consentID, &a.status, &a.use, &a.consentVersion, &a.validUntil, &a.contextID, &a.subjectDigest, &a.principal, &a.noticeDigest, &a.jws, &a.payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotReady
	}
	return a, err
}
func (s *Service) check(a authority, subject cm.SubjectContext, terms cm.Terms, p Policy, now time.Time) error {
	r := a.snapshot.Request
	if !now.Before(a.request.ExpiresAt) || !now.Before(a.validUntil) || !now.Before(subject.ExpiresAt) {
		return ErrExpired
	}
	if a.status != "ACTIVE" || a.use != "AVAILABLE" {
		return ErrDenied
	}
	if subject.ContextID != a.contextID || subject.Registry != r.Registry || subject.RequestDigest != a.request.Digest || subject.PrincipalContextRef != a.principal || !idPattern.MatchString(subject.SourceMappingRef) || !idPattern.MatchString(subject.VerificationMethod) || subject.VerifiedAt.IsZero() || subject.VerifiedAt.After(now) || subject.ExpiresAt.After(a.request.ExpiresAt) {
		return ErrBinding
	}
	raw, err := cm.Canonical(subject)
	if err != nil || cm.Digest(raw) != a.subjectDigest {
		return ErrBinding
	}
	if terms.Version != r.RecipientTerms.RetentionPolicyID || cm.Digest([]byte(terms.Text)) != a.snapshot.RecipientTermsDigest || terms.Text == "" {
		return ErrDenied
	}
	artefact, payload, err := s.config.Consents.Verify(a.jws, now)
	if err != nil || !bytes.Equal(payload, a.payload) {
		return ErrBinding
	}
	if artefact.ConsentID != a.consentID || artefact.RequestDigest != a.request.Digest || artefact.SubjectContextDigest != a.subjectDigest || artefact.Consumer != a.request.ConsumerID || artefact.Provider != r.Registry || artefact.Resource != r.Resource || !slices.Equal(artefact.Fields, r.Fields) || artefact.Purpose != r.Purpose || artefact.RecipientTermsVersion != terms.Version || artefact.RecipientTermsDigest != a.snapshot.RecipientTermsDigest || artefact.NoticeDigest != a.noticeDigest || !artefact.ValidUntil.Equal(a.validUntil) || artefact.ApprovedAt.Before(a.snapshot.CreatedAt) || artefact.ValidUntil.After(artefact.ApprovedAt.Add(time.Duration(r.Access.ValidForSeconds)*time.Second)) {
		return ErrBinding
	}
	if keyOf(p) != (PolicyKey{r.Registry, a.request.ConsumerID, r.Resource, r.Purpose}) || !p.allows(r.Fields) {
		return ErrDenied
	}
	return nil
}
func now(ctx context.Context, q queryer) (time.Time, error) {
	var value time.Time
	err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&value)
	return value.UTC(), err
}
