package authorization

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
)

func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		next.ServeHTTP(w, r)
	})
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func invalidInput(w http.ResponseWriter) {
	jsonResponse(w, 400, map[string]any{"error": map[string]any{"code": "INVALID_REQUEST", "message": "Input does not match this operation.", "request_id": rand.Text(), "retryable": false}})
}
func failure(w http.ResponseWriter, err error) {
	status, code, message, retry := 503, "DEPENDENCY_UNAVAILABLE", "Authorization dependency unavailable.", true
	switch {
	case errors.Is(err, consumer.ErrUnauthenticated):
		status, code, message, retry = 401, "UNAUTHENTICATED", "Consumer authentication required.", false
		w.Header().Set("WWW-Authenticate", `Bearer realm="gramstack"`)
	case errors.Is(err, access.ErrNotFound):
		status, code, message, retry = 404, "NOT_FOUND", "Authorization object not found.", false
	case errors.Is(err, ErrBinding), errors.Is(err, cm.ErrBinding), errors.Is(err, ErrDenied):
		status, code, message, retry = 403, "AUTHORIZATION_DENIED", "This disclosure is not permitted.", false
	case errors.Is(err, ErrExpired), errors.Is(err, cm.ErrExpired):
		status, code, message, retry = 410, "EXPIRED", "Authorization validity ended.", false
	case errors.Is(err, ErrNotReady), errors.Is(err, cm.ErrNotReady):
		status, code, message, retry = 409, "AUTHORITY_NOT_READY", "Current consent or workflow does not permit this operation.", false
	case errors.Is(err, ErrConflict), errors.Is(err, enforcement.ErrConsumed):
		status, code, message, retry = 409, "AUTHORITY_CONFLICT", "The operation conflicts with existing authority or an admitted attempt.", false
	}
	jsonResponse(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": rand.Text(), "retryable": retry}})
}
func (s *Service) PublicHandler(auth consumer.Authenticator) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/access-requests/{id}/grant", func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if auth == nil || len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			failure(w, consumer.ErrUnauthenticated)
			return
		}
		client, err := auth.Authenticate(r.Context(), strings.TrimPrefix(values[0], "Bearer "))
		if err != nil {
			failure(w, err)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		keys := r.Header.Values("Idempotency-Key")
		if err != nil || len(raw) != 0 || len(keys) != 1 || !idPattern.MatchString(keys[0]) {
			jsonResponse(w, 400, map[string]any{"error": map[string]any{"code": "INVALID_REQUEST", "message": "An empty body and one Idempotency-Key are required.", "request_id": rand.Text(), "retryable": false}})
			return
		}
		result, err := s.Issue(r.Context(), client, r.PathValue("id"), keys[0])
		if err != nil {
			failure(w, err)
			return
		}
		jsonResponse(w, 200, result)
	})
	mux.HandleFunc("GET /.well-known/gramstack-jwks.json", func(w http.ResponseWriter, r *http.Request) {
		ids := make([]string, 0, len(s.config.GrantKeys))
		for kid := range s.config.GrantKeys {
			ids = append(ids, kid)
		}
		slices.Sort(ids)
		keys := jose.JSONWebKeySet{}
		for _, kid := range ids {
			copy, _ := copyKeys(map[string]*ecdsa.PublicKey{kid: s.config.GrantKeys[kid]})
			keys.Keys = append(keys.Keys, jose.JSONWebKey{Key: copy[kid], KeyID: kid, Algorithm: "ES256", Use: "sig"})
		}
		w.Header().Set("Cache-Control", "public, max-age=60")
		jsonResponse(w, 200, keys)
	})
	return headers(mux)
}
func (s *Service) peer(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return "", ErrDenied
	}
	audience := ""
	for _, a := range s.config.Adapters {
		if r.TLS.PeerCertificates[0].VerifyHostname(a.PeerName) == nil {
			if audience != "" {
				return "", ErrDenied
			}
			audience = a.Audience
		}
	}
	if audience == "" {
		return "", ErrDenied
	}
	return audience, nil
}
func (s *Service) InternalHandler(consents *cm.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/grants/redeem", func(w http.ResponseWriter, r *http.Request) {
		audience, err := s.peer(r)
		if err != nil {
			failure(w, err)
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			invalidInput(w)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
		var input Redemption
		if err != nil || decode(raw, &input) != nil {
			invalidInput(w)
			return
		}
		result, err := s.Redeem(r.Context(), audience, input)
		if err != nil {
			failure(w, err)
			return
		}
		jsonResponse(w, 200, result)
	})
	mux.HandleFunc("GET /internal/consents/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		audience, err := s.peer(r)
		if err != nil {
			failure(w, err)
			return
		}
		if err = s.statusOwner(r.Context(), audience, r.PathValue("id")); err != nil {
			failure(w, err)
			return
		}
		status, err := consents.Status(r.Context(), r.PathValue("id"))
		if err != nil {
			failure(w, err)
			return
		}
		jsonResponse(w, 200, status)
	})
	return headers(mux)
}
func (s *Service) statusOwner(ctx context.Context, audience, id string) error {
	if !idPattern.MatchString(id) {
		return access.ErrNotFound
	}
	var compact string
	if err := s.config.Pool.QueryRow(ctx, `SELECT jws FROM retrieval_grants WHERE consent_id=$1`, id).Scan(&compact); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return access.ErrNotFound
		}
		return err
	}
	var g Grant
	if _, err := verify(compact, GrantType, s.config.GrantKeys, &g); err != nil || g.ConsentID != id || g.Audience != audience {
		return access.ErrNotFound
	}
	return nil
}
