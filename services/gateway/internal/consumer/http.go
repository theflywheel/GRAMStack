package consumer

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gowebpki/jcs"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/access-requests", s.createHTTP)
	mux.HandleFunc("GET /v1/access-requests/{id}", s.statusHTTP)
	mux.HandleFunc("POST /v1/access-requests/{id}/cancel", s.cancelHTTP)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, err error) {
	status, code, message, retry := 503, "DEPENDENCY_UNAVAILABLE", "A required service is unavailable.", true
	switch {
	case errors.Is(err, ErrUnauthenticated):
		status, code, message, retry = 401, "UNAUTHENTICATED", "Consumer authentication failed.", false
		w.Header().Set("WWW-Authenticate", `Bearer realm="gramstack"`)
	case errors.Is(err, ErrInput):
		status, code, message, retry = 400, "INVALID_REQUEST", "The request is invalid.", false
	case errors.Is(err, ErrPolicy):
		status, code, message, retry = 403, "SCOPE_NOT_PERMITTED", "The requested scope is not permitted.", false
	case errors.Is(err, access.ErrNotFound):
		status, code, message, retry = 404, "NOT_FOUND", "Request not found.", false
	case errors.Is(err, ErrConflict):
		status, code, message, retry = 409, "IDEMPOTENCY_CONFLICT", "The idempotency key was used for different input.", false
	case errors.Is(err, access.ErrAdmitted):
		status, code, message, retry = 409, "ADMISSION_ALREADY_OCCURRED", "The disclosure admission cutoff has passed.", false
	case errors.Is(err, access.ErrNotCancellable):
		status, code, message, retry = 409, "STATE_CONFLICT", "The request cannot be cancelled in its current state.", false
	}
	respond(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": rand.Text(), "retryable": retry}})
}
func (s *Service) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
		failure(w, ErrUnauthenticated)
		return "", false
	}
	client, err := s.auth.Authenticate(r.Context(), strings.TrimPrefix(headers[0], "Bearer "))
	if err != nil {
		failure(w, err)
		return "", false
	}
	if len(s.policies[client]) == 0 {
		failure(w, ErrPolicy)
		return "", false
	}
	return client, true
}
func (s *Service) createHTTP(w http.ResponseWriter, r *http.Request) {
	client, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		failure(w, ErrInput)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if err != nil {
		failure(w, ErrInput)
		return
	}
	// Canonicalisation rejects duplicate keys before Go's decoder can discard them.
	canonical, err := jcs.Transform(raw)
	if err != nil {
		failure(w, ErrInput)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	var input cm.AccessRequest
	if err = decoder.Decode(&input); err != nil {
		failure(w, ErrInput)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		failure(w, ErrInput)
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 {
		failure(w, ErrInput)
		return
	}
	value, err := s.Create(r.Context(), client, keys[0], input)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Location", value.StatusURL)
	respond(w, http.StatusAccepted, value)
}
func (s *Service) statusHTTP(w http.ResponseWriter, r *http.Request) {
	client, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	value, err := s.Status(r.Context(), client, r.PathValue("id"))
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, value)
}
func (s *Service) cancelHTTP(w http.ResponseWriter, r *http.Request) {
	client, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if r.ContentLength != 0 {
		failure(w, ErrInput)
		return
	}
	value, err := s.Cancel(r.Context(), client, r.PathValue("id"))
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, value)
}
