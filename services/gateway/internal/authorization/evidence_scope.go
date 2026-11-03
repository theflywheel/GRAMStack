package authorization

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

// EvidenceScope authenticates historical admission, not permission for another
// read. Receipts remain recordable after the grant expires or consent is stopped.
func (s *Service) EvidenceScope(ctx context.Context, audience, redemptionID string) (Grant, Reservation, error) {
	if !idPattern.MatchString(audience) || !idPattern.MatchString(redemptionID) {
		return Grant{}, Reservation{}, ErrBinding
	}
	var g storedGrant
	var r Reservation
	var requestID, requestDigest, jti, grantDigest, thumb string
	err := s.config.Pool.QueryRow(ctx, `SELECT g.jws,g.consent_id,g.jti,g.expires_at,r.redemption_id,r.attempt_id,r.reserved_at,c.request_id,c.request_digest,r.grant_jti,r.grant_digest,r.presenter_thumbprint FROM disclosure_reservations r JOIN consent_state c USING(consent_id) JOIN retrieval_grants g ON g.request_id=c.request_id WHERE r.adapter_id=$1 AND r.redemption_id=$2`, audience, redemptionID).Scan(&g.compact, &g.consentID, &g.jti, &g.expires, &r.ID, &r.AttemptID, &r.ReservedAt, &requestID, &requestDigest, &jti, &grantDigest, &thumb)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, Reservation{}, ErrDenied
	}
	if err != nil {
		return Grant{}, Reservation{}, err
	}
	var claims Grant
	if _, err = verify(g.compact, GrantType, s.config.GrantKeys, &claims); err != nil || claims.validate() != nil || claims.Issuer != s.config.Issuer || claims.Audience != audience || claims.RequestID != requestID || claims.Digest != requestDigest || claims.JTI != jti || claims.Confirmation.Thumbprint != thumb || cm.Digest([]byte(g.compact)) != grantDigest || !g.matches(claims) || r.ReservedAt.Before(time.Unix(claims.IssuedAt, 0)) || !r.ReservedAt.Before(time.Unix(claims.ExpiresAt, 0)) {
		return Grant{}, Reservation{}, ErrBinding
	}
	return claims, r, nil
}
