package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsafeOrIncompleteStartupConfigurationFailsBeforeConnecting(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"sakhicredit"}
	t.Setenv("SAKHI_DATABASE_URL", "")
	t.Setenv("SAKHI_ENV", "production")
	for _, tc := range []struct{ body, reason string }{
		{`{"public_origin":""}`, "public origin"},
		{`{"public_origin":"http://127.0.0.1","allow_loopback_http":true}`, "development environment"},
		{`{"public_origin":"https://credit.example","unknown":true}`, "invalid product configuration"},
	} {
		path := filepath.Join(t.TempDir(), "product.json")
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SAKHI_CONFIG_FILE", path)
		if err := run(); err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Fatal("unsafe startup accepted or progressed beyond its guard", err)
		}
	}
	os.Args = []string{"sakhicredit", "migrate"}
	if err := run(); err == nil || !strings.Contains(err.Error(), "database URL") {
		t.Fatal("migration used an implicit local database", err)
	}
}
