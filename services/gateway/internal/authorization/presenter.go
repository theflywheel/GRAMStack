package authorization

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
)

// AuthenticateRetrieval verifies possession at the original consumer TLS hop.
// No forwarded certificate or identity header can establish this binding.
func (s *Service) AuthenticateRetrieval(r *http.Request) (Grant, string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return Grant{}, "", consumer.ErrUnauthenticated
	}
	compact := strings.TrimPrefix(values[0], "Bearer ")
	grant, err := s.VerifyGrant(compact, time.Now().Unix())
	if err != nil {
		return Grant{}, "", err
	}
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return Grant{}, "", ErrBinding
	}
	sum := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	thumb := base64.RawURLEncoding.EncodeToString(sum[:])
	if thumb != grant.Confirmation.Thumbprint || s.config.Presenters[grant.Consumer] != thumb {
		return Grant{}, "", ErrBinding
	}
	return grant, compact, nil
}
