package custody

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
)

func (s *Service) peer(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return "", ErrDenied
	}
	identity := ""
	for _, a := range s.adapters {
		if r.TLS.PeerCertificates[0].VerifyHostname(a.PeerName) == nil {
			if identity != "" {
				return "", ErrDenied
			}
			identity = a.ID
		}
	}
	if identity == "" {
		return "", ErrDenied
	}
	return identity, nil
}
func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": "Evidence was not accepted.", "request_id": rand.Text(), "retryable": status == 503}})
}
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		if r.Method != "POST" || r.URL.Path != "/internal/v1/evidence" {
			http.NotFound(w, r)
			return
		}
		adapter, err := s.peer(r)
		if err != nil {
			failure(w, 403, "UNTRUSTED_PRODUCER")
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || r.Header.Get("Gramstack-Contract-Version") != "0.3.0" {
			failure(w, 400, "INVALID_REQUEST")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
		var input struct {
			JWS string `json:"jws"`
		}
		if err != nil || decode(raw, &input) != nil || input.JWS == "" {
			failure(w, 400, "INVALID_REQUEST")
			return
		}
		result, err := s.Record(r.Context(), adapter, input.JWS)
		if err != nil {
			status, code := 503, "EVIDENCE_UNAVAILABLE"
			if errors.Is(err, ErrDenied) || errors.Is(err, az.ErrDenied) || errors.Is(err, az.ErrBinding) {
				status, code = 403, "EVIDENCE_DENIED"
			} else if errors.Is(err, ErrConflict) {
				status, code = 409, "EVIDENCE_CONFLICT"
			}
			failure(w, status, code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(result)
	})
}
