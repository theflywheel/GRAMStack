// Package enforcement owns the transaction authority shared by consent
// withdrawal and grant redemption. Callers must authenticate and cryptographically
// validate the grant before invoking Redeem; this repository is not a token verifier.
package enforcement

import (
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/platform/outbox"
)

var (
	ErrInactive       = errors.New("consent is not active or has expired")
	ErrConsumed       = errors.New("disclosure already reserved")
	ErrBinding        = errors.New("grant or attempt binding mismatch")
	ErrVersion        = errors.New("consent version conflict")
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	thumbprintPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

type Repository struct{ Pool *pgxpool.Pool }
type Consent struct {
	ID, RequestID, Digest, Status, Use string
	Version                            int64
	ValidUntil                         time.Time
}

// VerifiedRedemption is constructed only after signature, policy, subject,
// audience and authenticated presenter checks in the authorization layer.
type VerifiedRedemption struct {
	ConsentID, RequestID, RequestDigest                              string
	GrantJTI, GrantDigest, AdapterID, AttemptID, PresenterThumbprint string
	GrantExpiresAt                                                   time.Time
}
type Reservation struct {
	ID, AttemptID   string
	FirstRedemption bool
	ReservedAt      time.Time
}

func (r Repository) CreatePending(ctx context.Context, id, requestID, digest string, until time.Time) error {
	if id == "" || requestID == "" || !digestPattern.MatchString(digest) || !until.After(time.Now()) {
		return ErrBinding
	}
	_, err := r.Pool.Exec(ctx, `INSERT INTO consent_state(consent_id,request_id,request_digest,valid_until) VALUES($1,$2,$3,$4)`, id, requestID, digest, until)
	return err
}
func lockConsent(ctx context.Context, tx pgx.Tx, id string) (Consent, error) {
	var c Consent
	err := tx.QueryRow(ctx, `SELECT consent_id,request_id,request_digest,status,use_state,status_version,valid_until FROM consent_state WHERE consent_id=$1 FOR UPDATE`, id).Scan(&c.ID, &c.RequestID, &c.Digest, &c.Status, &c.Use, &c.Version, &c.ValidUntil)
	return c, err
}
func (r Repository) Withdraw(ctx context.Context, id string, expectedVersion int64) (Consent, error) {
	var result Consent
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		c, err := lockConsent(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.Status == "WITHDRAWN" {
			result = c
			return nil
		}
		// A bound authenticated withdrawal need not carry a version (zero); when
		// supplied, enforce it rather than silently applying a stale browser action.
		if expectedVersion != 0 && c.Version != expectedVersion {
			return ErrVersion
		}
		next, err := consentmanager.Transition(ctx, c.Status, "withdraw")
		if err != nil {
			return err
		}
		c.Status = next
		c.Version++
		if _, err = tx.Exec(ctx, `UPDATE consent_state SET status=$2,status_version=$3 WHERE consent_id=$1`, id, c.Status, c.Version); err != nil {
			return err
		}
		if err = outbox.Append(ctx, tx, "consent", id, c.Version, "consent.withdrawn", map[string]any{"consent_id": id, "access_request_id": c.RequestID, "request_digest": c.Digest, "type": "consent.withdrawn"}); err != nil {
			return err
		}
		result = c
		return nil
	})
	if err != nil {
		return Consent{}, err
	}
	return result, nil
}
func (r Repository) Redeem(ctx context.Context, input VerifiedRedemption) (Reservation, error) {
	if input.ConsentID == "" || input.RequestID == "" || input.GrantJTI == "" || input.AdapterID == "" || input.AttemptID == "" || !digestPattern.MatchString(input.RequestDigest) || !digestPattern.MatchString(input.GrantDigest) || !thumbprintPattern.MatchString(input.PresenterThumbprint) {
		return Reservation{}, ErrBinding
	}
	var result Reservation
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		c, err := lockConsent(ctx, tx, input.ConsentID)
		if err != nil {
			return err
		}
		if c.RequestID != input.RequestID || c.Digest != input.RequestDigest {
			return ErrBinding
		}
		var adapter, jti, grantDigest, presenter string
		err = tx.QueryRow(ctx, `SELECT redemption_id,attempt_id,reserved_at,adapter_id,grant_jti,grant_digest,presenter_thumbprint FROM disclosure_reservations WHERE consent_id=$1`, c.ID).Scan(&result.ID, &result.AttemptID, &result.ReservedAt, &adapter, &jti, &grantDigest, &presenter)
		if err == nil {
			if result.AttemptID != input.AttemptID {
				return ErrConsumed
			}
			if adapter != input.AdapterID || jti != input.GrantJTI || grantDigest != input.GrantDigest || presenter != input.PresenterThumbprint {
				return ErrBinding
			}
			// Metadata-only idempotent acknowledgement, never a new source-read admission.
			result.FirstRedemption = false
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var now time.Time
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		if c.Status != "ACTIVE" || !now.Before(c.ValidUntil) || !now.Before(input.GrantExpiresAt) {
			return ErrInactive
		}
		if c.Use != "AVAILABLE" {
			return ErrConsumed
		}
		result.ID = rand.Text()
		result.AttemptID = input.AttemptID
		result.FirstRedemption = true
		if err = tx.QueryRow(ctx, `INSERT INTO disclosure_reservations(consent_id,redemption_id,grant_jti,grant_digest,adapter_id,presenter_thumbprint,attempt_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING reserved_at`, c.ID, result.ID, input.GrantJTI, input.GrantDigest, input.AdapterID, input.PresenterThumbprint, input.AttemptID).Scan(&result.ReservedAt); err != nil {
			return err
		}
		c.Version++
		if _, err = tx.Exec(ctx, `UPDATE consent_state SET use_state='RESERVED',status_version=$2 WHERE consent_id=$1`, c.ID, c.Version); err != nil {
			return err
		}
		return outbox.Append(ctx, tx, "consent", c.ID, c.Version, "consent.exhausted", map[string]any{"consent_id": c.ID, "access_request_id": c.RequestID, "request_digest": c.Digest, "type": "consent.exhausted"})
	})
	if err != nil {
		return Reservation{}, err
	}
	return result, nil
}
