package authorization

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type storedGrant struct {
	compact, policyDigest, subjectDigest, consentID, jti string
	consentVersion                                       int64
	expires                                              time.Time
}

func stored(ctx context.Context, q queryer, id string) (storedGrant, error) {
	var g storedGrant
	err := q.QueryRow(ctx, `SELECT jws,policy_digest,subject_digest,consent_id,jti,consent_version,expires_at FROM retrieval_grants WHERE request_id=$1`, id).Scan(&g.compact, &g.policyDigest, &g.subjectDigest, &g.consentID, &g.jti, &g.consentVersion, &g.expires)
	return g, err
}
func (g storedGrant) matches(claims Grant) bool {
	return g.consentID == claims.ConsentID && g.jti == claims.JTI && g.expires.Equal(time.Unix(claims.ExpiresAt, 0))
}
func response(g storedGrant) GrantResponse {
	return GrantResponse{g.compact, g.expires.UTC(), "Bearer"}
}
func (s *Service) Issue(ctx context.Context, consumer, id, key string) (GrantResponse, error) {
	if !idPattern.MatchString(consumer) || !idPattern.MatchString(id) || !idPattern.MatchString(key) {
		return GrantResponse{}, ErrBinding
	}
	thumb, ok := s.config.Presenters[consumer]
	if !ok {
		return GrantResponse{}, ErrDenied
	}
	request, err := readRequest(ctx, s.config.Pool, id, consumer, false)
	if err != nil {
		return GrantResponse{}, err
	}
	if request.Status != "CONSENTED" && request.Status != "AUTHORIZED" {
		return GrantResponse{}, ErrNotReady
	}
	a, err := loadAuthority(ctx, s.config.Pool, request, false)
	if err != nil {
		return GrantResponse{}, err
	}
	subject, err := s.config.Subjects.Resolve(ctx, a.contextID)
	if err != nil {
		return GrantResponse{}, err
	}
	terms, err := s.config.Terms.Resolve(ctx, consumer, a.snapshot.Request.RecipientTerms.RetentionPolicyID)
	if err != nil {
		return GrantResponse{}, err
	}
	policyKey := PolicyKey{a.snapshot.Request.Registry, consumer, a.snapshot.Request.Resource, a.snapshot.Request.Purpose}
	policy, policyDigest, err := s.policy(ctx, s.config.Pool, policyKey, false)
	if err != nil {
		return GrantResponse{}, err
	}
	clock, err := now(ctx, s.config.Pool)
	if err != nil {
		return GrantResponse{}, err
	}
	if err = s.check(a, subject, terms, policy, clock); err != nil {
		return GrantResponse{}, err
	}
	adapter, ok := s.config.Adapters[policy.Registry]
	if !ok {
		return GrantResponse{}, ErrDenied
	}
	// Preparation holds no SQL locks. Final authority and policy are rechecked below.
	until := min(a.validUntil.Unix(), subject.ExpiresAt.Unix(), request.ExpiresAt.Unix(), clock.Unix()+policy.MaxSeconds)
	grant := Grant{Issuer: s.config.Issuer, Audience: adapter.Audience, Subject: subject.PrincipalContextRef, Consumer: consumer, JTI: rand.Text(), IssuedAt: clock.Unix(), ExpiresAt: until, RequestID: id, Digest: request.Digest, Resource: policy.Resource, Fields: slices.Clone(a.snapshot.Request.Fields), Purpose: policy.Purpose, ConsentID: a.consentID, ContextID: a.contextID, PolicyVersion: policy.Version}
	grant.Confirmation.Thumbprint = thumb
	if grant.validate() != nil {
		return GrantResponse{}, ErrExpired
	}
	payload, err := cm.Canonical(grant)
	if err != nil {
		return GrantResponse{}, err
	}
	candidate, candidateErr := stored(ctx, s.config.Pool, id)
	candidateExists := candidateErr == nil
	var compact string
	if candidateExists {
		compact = candidate.compact
	} else {
		if !errors.Is(candidateErr, pgx.ErrNoRows) {
			return GrantResponse{}, candidateErr
		}
		compact, err = s.config.Signer.Sign(ctx, payload)
		if err != nil {
			return GrantResponse{}, ErrSigning
		}
		var signed Grant
		verified, err := verify(compact, GrantType, s.config.GrantKeys, &signed)
		if err != nil || !bytes.Equal(verified, payload) {
			return GrantResponse{}, ErrSigning
		}
	}
	var result GrantResponse
	err = pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		// Serialize actor/key before the request; different requests cannot reuse it.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "grant:"+consumer+":"+cm.Digest([]byte(key))); err != nil {
			return err
		}
		var previous string
		err := tx.QueryRow(ctx, `SELECT request_id FROM grant_idempotency WHERE consumer_id=$1 AND key_digest=$2`, consumer, cm.Digest([]byte(key))).Scan(&previous)
		if err == nil && previous != id {
			return ErrConflict
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		current, err := readRequest(ctx, tx, id, consumer, true)
		if err != nil {
			return err
		}
		if current.Status != "CONSENTED" && current.Status != "AUTHORIZED" {
			return ErrNotReady
		}
		currentPolicy, currentDigest, err := s.policy(ctx, tx, policyKey, true)
		if err != nil {
			return err
		}
		if currentDigest != policyDigest {
			return ErrConflict
		}
		live, err := loadAuthority(ctx, tx, current, true)
		if err != nil {
			return err
		}
		clock, err := now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.check(live, subject, terms, currentPolicy, clock); err != nil {
			return err
		}
		if live.consentVersion != a.consentVersion || live.contextID != a.contextID || live.subjectDigest != a.subjectDigest {
			return ErrConflict
		}
		old, err := stored(ctx, tx, id)
		if err == nil {
			oldClaims, verifyErr := s.VerifyGrant(old.compact, clock.Unix())
			if verifyErr != nil {
				return verifyErr
			}
			if !old.matches(oldClaims) || old.policyDigest != policyDigest || old.subjectDigest != live.subjectDigest || old.consentVersion != live.consentVersion || !matches(oldClaims, live, subject, currentPolicy, thumb, adapter.Audience) {
				return ErrDenied
			}
			result = response(old)
		} else {
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if candidateExists {
				return ErrConflict
			}
			if current.Status != "CONSENTED" || grant.ExpiresAt <= clock.Unix() {
				return ErrExpired
			}
			if _, err = tx.Exec(ctx, `INSERT INTO retrieval_grants(request_id,consent_id,consent_version,jti,policy_digest,subject_digest,jws,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, live.consentID, live.consentVersion, grant.JTI, policyDigest, live.subjectDigest, compact, time.Unix(grant.ExpiresAt, 0).UTC()); err != nil {
				return err
			}
			if _, err = access.AdvanceTx(ctx, tx, id, current.Version, "grant_issued"); err != nil {
				return err
			}
			result = GrantResponse{compact, time.Unix(grant.ExpiresAt, 0).UTC(), "Bearer"}
		}
		_, err = tx.Exec(ctx, `INSERT INTO grant_idempotency(consumer_id,key_digest,request_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, consumer, cm.Digest([]byte(key)), id)
		return err
	})
	if err != nil {
		return GrantResponse{}, err
	}
	return result, nil
}
func matches(g Grant, a authority, subject cm.SubjectContext, p Policy, thumb, audience string) bool {
	return g.Audience == audience && g.Consumer == a.request.ConsumerID && g.Subject == subject.PrincipalContextRef && g.RequestID == a.request.ID && g.Digest == a.request.Digest && g.ConsentID == a.consentID && g.ContextID == a.contextID && g.PolicyVersion == p.Version && g.Resource == a.snapshot.Request.Resource && g.Purpose == a.snapshot.Request.Purpose && slices.Equal(g.Fields, a.snapshot.Request.Fields) && g.Confirmation.Thumbprint == thumb && g.ExpiresAt <= min(a.validUntil.Unix(), subject.ExpiresAt.Unix(), a.request.ExpiresAt.Unix()) && g.ExpiresAt-g.IssuedAt <= p.MaxSeconds
}
