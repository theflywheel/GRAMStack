package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProductionRejectsDemoAuthentication(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	for _, tc := range []struct{ name, body, want string }{
		{"demo registry", `{"registries":[{"registry":"lokos","demo":true}]}`, "demo registry verification"},
		{"demo upstream", `{"authentication":{"demo_upstream":true}}`, "fictional identity provider"},
		{"loopback HTTP", `{"allow_loopback_http":true}`, "development-only"},
		{"unknown config", `{"unexpected":true}`, "configuration fields"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GRAMSTACK_CONFIG_FILE", file)
			t.Setenv("GRAMSTACK_ENV", "production")
			close, err := configureConsumers(context.Background(), http.NewServeMux())
			if close != nil {
				close()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unsafe config admitted: %v", err)
			}
		})
	}
}

func TestReadinessDoesNotExposeDependencyErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		check  func(context.Context) error
		status int
	}{
		{"unconfigured", nil, 503},
		{"unavailable", func(context.Context) error { return errors.New("secret database location") }, 503},
		{"configured", func(ctx context.Context) error {
			if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
				t.Error("unbounded readiness")
			}
			return nil
		}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			registerReadiness(mux, tc.check)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("GET", "/readyz", nil))
			if response.Code != tc.status || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "secret") {
				t.Fatal(response)
			}
		})
	}
}
