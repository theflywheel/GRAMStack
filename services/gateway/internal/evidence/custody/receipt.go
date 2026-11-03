package custody

import (
	"bytes"
	"context"
	"slices"

	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

// VerifyReceipt checks retained signed custody before the gateway releases data.
// The response itself stays in memory; the private commitment salt stays adapter-local.
func (s *Service) VerifyReceipt(ctx context.Context, id, adapterID, attempt string, grant az.Grant) error {
	adapter, ok := s.adapters[adapterID]
	if !ok || !identifier.MatchString(id) || !identifier.MatchString(attempt) {
		return ErrDenied
	}
	var compact, digest string
	err := s.pool.QueryRow(ctx, `SELECT jws,evidence_digest FROM evidence_receipts WHERE receipt_id=$1 AND adapter_id=$2 AND request_id=$3 AND request_digest=$4 AND consent_id=$5 AND grant_jti=$6 AND attempt_id=$7 AND outcome='release_ready'`, id, adapterID, grant.RequestID, grant.Digest, grant.ConsentID, grant.JTI, attempt).Scan(&compact, &digest)
	if err != nil {
		return err
	}
	event, err := verify(compact, adapter.Keys)
	if err != nil || cm.Digest([]byte(compact)) != digest || event.RequestID != grant.RequestID || event.Digest != grant.Digest || event.ConsentID != grant.ConsentID || event.GrantJTI != grant.JTI || event.AttemptID != attempt || event.AdapterID != adapterID || event.Outcome != "release_ready" || !slices.Equal(event.Fields, grant.Fields) {
		return ErrDenied
	}
	historical, reservation, err := s.scopes.EvidenceScope(ctx, adapter.Audience, event.RedemptionID)
	if err != nil {
		return err
	}
	expected, _ := cm.Canonical(grant)
	actual, _ := cm.Canonical(historical)
	if reservation.AttemptID != attempt || !bytes.Equal(expected, actual) {
		return ErrDenied
	}
	return nil
}
