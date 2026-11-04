package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) historyDigest(client, registry string, principal Principal) string {
	mac := hmac.New(sha256.New, s.historyKey)
	fmt.Fprintf(mac, "%q:%q:%q:%q", client, registry, principal.Issuer, principal.Subject)
	return "sha256:" + hex.EncodeToString(mac.Sum(nil))
}

// bindHistory keeps ownership stable even after encrypted authentication
// transactions and sessions expire. Only opaque, scoped values survive.
func (s *Service) bindHistory(ctx context.Context, tx pgx.Tx, requestID, registry string, principal Principal) error {
	owner := s.store.historyDigest(s.historyClientID, registry, principal)
	if _, err := tx.Exec(ctx, `INSERT INTO consent_history_owners(request_id,owner_digest,principal_ref)
		VALUES($1,$2,$3) ON CONFLICT(request_id) DO NOTHING`, requestID, owner, principal.Reference); err != nil {
		return err
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT owner_digest=$2 AND principal_ref=$3 FROM consent_history_owners WHERE request_id=$1`,
		requestID, owner, principal.Reference).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return ErrAuthentication
	}
	return nil
}
