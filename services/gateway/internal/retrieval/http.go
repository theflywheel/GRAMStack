package retrieval

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
)

func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": "Protected retrieval did not complete.", "request_id": rand.Text(), "retryable": status == 429}})
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/access-requests/{id}/data", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			failure(w, 405, "INVALID_METHOD")
			return
		}
		g, compact, err := s.authority.AuthenticateRetrieval(r)
		if err != nil {
			status, code := 403, "AUTHORIZATION_DENIED"
			if errors.Is(err, consumer.ErrUnauthenticated) {
				status, code = 401, "UNAUTHENTICATED"
				w.Header().Set("WWW-Authenticate", `Bearer realm="gramstack-retrieval"`)
			} else if errors.Is(err, az.ErrExpired) {
				status, code = 410, "EXPIRED"
			}
			failure(w, status, code)
			return
		}
		if r.PathValue("id") != g.RequestID || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 {
			failure(w, 403, "REQUEST_BINDING_DENIED")
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			w.Header().Set("Retry-After", "1")
			failure(w, 429, "CAPACITY_UNAVAILABLE")
			return
		}
		input, e, err := s.claim(r.Context(), g, compact)
		if err != nil {
			status, code := 503, "RETRIEVAL_UNAVAILABLE"
			if errors.Is(err, ErrConflict) {
				status, code = 409, "ATTEMPT_ALREADY_CLAIMED"
			} else if errors.Is(err, ErrDenied) {
				status, code = 403, "REQUEST_BINDING_DENIED"
			} else if errors.Is(err, az.ErrExpired) {
				status, code = 410, "EXPIRED"
			}
			failure(w, status, code)
			return
		}
		releaseBegan := false
		defer func() {
			if !releaseBegan {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				s.fail(ctx, g.RequestID, input.AttemptID, false)
			}
		}()
		result, err := s.fetch(r.Context(), e, input)
		if err != nil {
			status, code := 503, "ADAPTER_UNAVAILABLE"
			if errors.Is(err, ErrDenied) {
				status, code = 403, "AUTHORIZATION_DENIED"
			} else if errors.Is(err, ErrConflict) {
				status, code = 409, "DISCLOSURE_NOT_AVAILABLE"
			} else if errors.Is(err, az.ErrExpired) {
				status, code = 410, "EXPIRED"
			}
			failure(w, status, code)
			return
		}
		if err = s.receipts.VerifyReceipt(r.Context(), result.ReceiptID, e.descriptor.AdapterID, input.AttemptID, g); err != nil {
			failure(w, 503, "EVIDENCE_UNAVAILABLE")
			return
		}
		raw, err := json.Marshal(result)
		if err != nil || len(raw) > 1<<20 {
			failure(w, 503, "RELEASE_UNAVAILABLE")
			return
		}
		if err = s.release(r.Context(), g, input, e, result.ReceiptID); err != nil {
			failure(w, 503, "RELEASE_UNAVAILABLE")
			return
		}
		releaseBegan = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		n, writeErr := w.Write(raw)
		finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// A failed final checkpoint remains an occupied attempt for the sweeper.
		s.finish(finish, g.RequestID, input.AttemptID, writeErr == nil && n == len(raw))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Gramstack-Contract-Version", "0.3.0")
		mux.ServeHTTP(w, r)
	})
}
