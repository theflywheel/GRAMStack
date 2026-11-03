package application

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, err error, app Application) {
	status, code := 503, "PLATFORM_UNAVAILABLE"
	switch {
	case errors.Is(err, ErrInput):
		status, code = 400, "INVALID_INPUT"
	case errors.Is(err, ErrNotFound):
		status, code = 404, "NOT_FOUND"
	case errors.Is(err, ErrConflict):
		status, code = 409, "STATE_CONFLICT"
	}
	var remote *RemoteError
	if errors.As(err, &remote) && remote.Status < 500 && remote.Status != 401 && remote.Status != 429 {
		status, code = 409, "PLATFORM_DECLINED"
	}
	value := map[string]any{"error": map[string]string{"code": code}}
	if app.ID != "" {
		value["application"] = app
	}
	writeJSON(w, status, value)
}
func Handler(service *Service, origin string, allowLoopback bool) (http.Handler, error) {
	if !trustedURL(origin, allowLoopback, true) {
		return nil, errors.New("trusted product public origin required")
	}
	secure := !allowLoopback || origin[:5] == "https"
	cookieName := "__Host-sakhi"
	if !secure {
		cookieName = "sakhi_demo"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", webHandler("index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /assets/app.css", webHandler("app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /assets/app.js", webHandler("app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /assets/mark.svg", webHandler("mark.svg", "image/svg+xml"))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if service == nil || service.store.Ready(r.Context()) != nil {
			writeJSON(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			problem(w, ErrUnavailable, Application{})
			return
		}
		if cookie, err := r.Cookie(cookieName); err == nil {
			if _, csrf, err := service.store.Session(r.Context(), cookie.Value); err == nil {
				writeJSON(w, 200, map[string]string{"csrf_token": csrf})
				return
			} else if !errors.Is(err, ErrNotFound) {
				problem(w, ErrUnavailable, Application{})
				return
			}
		}
		token, csrf, err := service.store.NewSession(r.Context())
		if err != nil {
			problem(w, ErrUnavailable, Application{})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: 7200})
		writeJSON(w, 200, map[string]string{"csrf_token": csrf})
	})
	authenticate := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		if service == nil {
			problem(w, ErrUnavailable, Application{})
			return "", false
		}
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "SESSION_REQUIRED"})
			return "", false
		}
		owner, csrf, err := service.store.Session(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, 401, map[string]string{"error": "SESSION_REQUIRED"})
			} else {
				problem(w, ErrUnavailable, Application{})
			}
			return "", false
		}
		if r.Method != "GET" && (len(r.Header.Values("Origin")) != 1 || len(r.Header.Values("X-CSRF-Token")) != 1 || r.Header.Get("Origin") != origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1) {
			writeJSON(w, 403, map[string]string{"error": "REQUEST_VERIFICATION_FAILED"})
			return "", false
		}
		return owner, true
	}
	// Product concurrency is bounded independently of the platform's disclosure gate.
	slots := make(chan struct{}, 8)
	endpoint := func(fn func(string, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			owner, ok := authenticate(w, r)
			if !ok {
				return
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				writeJSON(w, 429, map[string]string{"error": "BUSY"})
				return
			}
			fn(owner, w, r)
		}
	}
	mux.HandleFunc("POST /api/applications", endpoint(func(owner string, w http.ResponseWriter, r *http.Request) {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			problem(w, ErrInput, Application{})
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
		var input Input
		if err != nil || decode(raw, &input) != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
			problem(w, ErrInput, Application{})
			return
		}
		app, err := service.Create(r.Context(), owner, r.Header.Get("Idempotency-Key"), input)
		if err != nil {
			problem(w, err, app)
			return
		}
		writeJSON(w, 201, app)
	}))
	mux.HandleFunc("GET /api/applications/{id}", endpoint(func(owner string, w http.ResponseWriter, r *http.Request) {
		app, err := service.Get(r.Context(), owner, r.PathValue("id"))
		if err != nil {
			problem(w, err, Application{})
			return
		}
		writeJSON(w, 200, app)
	}))
	operations := map[string]func(string, http.ResponseWriter, *http.Request){
		"refresh": func(owner string, w http.ResponseWriter, r *http.Request) {
			app, err := service.Refresh(r.Context(), owner, r.PathValue("id"))
			if err != nil {
				problem(w, err, app)
				return
			}
			writeJSON(w, 200, app)
		},
		"retrieve": func(owner string, w http.ResponseWriter, r *http.Request) {
			app, err := service.Retrieve(r.Context(), owner, r.PathValue("id"))
			if err != nil {
				problem(w, err, app)
				return
			}
			writeJSON(w, 200, app)
		},
		"cancel": func(owner string, w http.ResponseWriter, r *http.Request) {
			app, err := service.Cancel(r.Context(), owner, r.PathValue("id"))
			if err != nil {
				problem(w, err, app)
				return
			}
			writeJSON(w, 200, app)
		},
	}
	for name, operation := range operations {
		mux.HandleFunc("POST /api/applications/{id}/"+name, endpoint(func(owner string, w http.ResponseWriter, r *http.Request) {
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
			if err != nil || len(raw) != 0 {
				problem(w, ErrInput, Application{})
				return
			}
			operation(owner, w, r)
		}))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Sakhicredit-API-Version", "0.1.0")
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	}), nil
}
