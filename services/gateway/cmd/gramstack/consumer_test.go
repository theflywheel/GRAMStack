package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
