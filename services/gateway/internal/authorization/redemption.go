package authorization

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
)

func (s *Service) Redeem(ctx context.Context, adapterAudience string, input Redemption) (Reservation, error) {
	var g Grant
	if _, err := verify(input.Grant, GrantType, s.config.GrantKeys, &g); err != nil {
		return Reservation{}, err
	}
	if g.validate() != nil || g.Issuer != s.config.Issuer || !idPattern.MatchString(input.AttemptID) || input.RequestID != g.RequestID || input.Digest != g.Digest || input.Presenter.Consumer != g.Consumer || input.Presenter.Thumbprint != g.Confirmation.Thumbprint || s.config.Presenters[g.Consumer] != g.Confirmation.Thumbprint || adapterAudience != g.Audience {
		return Reservation{}, ErrBinding
	}
	// Resolve outside locks; an expired context cannot prevent a historical,
	// metadata-only acknowledgement of an already admitted identical attempt.
	subject, subjectErr := s.config.Subjects.Resolve(ctx, g.ContextID)
	var terms cm.Terms
	var termsErr error
	// The exact terms version comes from the immutable snapshot below; the resolver
	// is called with that version before entering the authoritative transaction.
	request, err := readRequest(ctx, s.config.Pool, g.RequestID, g.Consumer, false)
	if err != nil {
		return Reservation{}, err
	}
	prepared, prepareErr := loadAuthority(ctx, s.config.Pool, request, false)
	if prepareErr == nil {
		terms, termsErr = s.config.Terms.Resolve(ctx, g.Consumer, prepared.snapshot.Request.RecipientTerms.RetentionPolicyID)
	}
	var result Reservation
	err = pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		current, err := readRequest(ctx, tx, g.RequestID, g.Consumer, true)
		if err != nil {
			return err
		}
		issued, err := stored(ctx, tx, g.RequestID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrDenied
			}
			return err
		}
		if issued.compact != input.Grant || !issued.matches(g) {
			return ErrBinding
		}
		var prior Reservation
		var adapter, jti, digest, thumb string
		err = tx.QueryRow(ctx, `SELECT redemption_id,attempt_id,reserved_at,adapter_id,grant_jti,grant_digest,presenter_thumbprint FROM disclosure_reservations WHERE consent_id=$1`, g.ConsentID).Scan(&prior.ID, &prior.AttemptID, &prior.ReservedAt, &adapter, &jti, &digest, &thumb)
		if err == nil {
			if prior.AttemptID != input.AttemptID {
				return enforcement.ErrConsumed
			}
			if adapter != adapterAudience || jti != g.JTI || digest != cm.Digest([]byte(input.Grant)) || thumb != input.Presenter.Thumbprint {
				return ErrBinding
			}
			result = prior
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if current.Status != "AUTHORIZED" {
			return ErrNotReady
		}
		if prepareErr != nil {
			return prepareErr
		}
		if subjectErr != nil {
			return subjectErr
		}
		if termsErr != nil {
			return termsErr
		}
		key := PolicyKey{prepared.snapshot.Request.Registry, g.Consumer, g.Resource, g.Purpose}
		policy, policyDigest, err := s.policy(ctx, tx, key, true)
		if err != nil {
			return err
		}
		adapterConfig, ok := s.config.Adapters[policy.Registry]
		if !ok || adapterConfig.Audience != adapterAudience {
			return ErrDenied
		}
		live, err := loadAuthority(ctx, tx, current, true)
		if err != nil {
			return err
		}
		clock, err := now(ctx, tx)
		if err != nil {
			return err
		}
		if _, err = s.VerifyGrant(input.Grant, clock.Unix()); err != nil {
			return err
		}
		if err = s.check(live, subject, terms, policy, clock); err != nil {
			return err
		}
		if issued.policyDigest != policyDigest || issued.subjectDigest != live.subjectDigest || issued.consentVersion != live.consentVersion || !matches(g, live, subject, policy, input.Presenter.Thumbprint, adapterAudience) {
			return ErrDenied
		}
		reserved, err := enforcement.RedeemTx(ctx, tx, enforcement.VerifiedRedemption{ConsentID: g.ConsentID, RequestID: g.RequestID, RequestDigest: g.Digest, GrantJTI: g.JTI, GrantDigest: cm.Digest([]byte(input.Grant)), AdapterID: adapterAudience, AttemptID: input.AttemptID, PresenterThumbprint: input.Presenter.Thumbprint, GrantExpiresAt: time.Unix(g.ExpiresAt, 0)})
		if err != nil {
			return err
		}
		if reserved.FirstRedemption {
			if _, err = access.AdvanceTx(ctx, tx, current.ID, current.Version, "retrieval_started"); err != nil {
				return err
			}
		}
		result = Reservation{reserved.ID, reserved.AttemptID, reserved.FirstRedemption, reserved.ReservedAt}
		return nil
	})
	if err != nil {
		return Reservation{}, err
	}
	return result, nil
}
