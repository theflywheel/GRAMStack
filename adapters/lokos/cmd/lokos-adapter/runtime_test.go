package main

import (
	"context"
	"strings"
	"testing"
)

func TestProductionRejectsDemoVerifier(t *testing.T) {
	t.Setenv("ADAPTER_STATE_DATABASE_URL", "postgres://not-contacted")
	t.Setenv("ADAPTER_DEMO_VERIFIER", "true")
	t.Setenv("GRAMSTACK_ENV", "production")
	if _, err := configureAdapter(context.Background(), false); err == nil || !strings.Contains(err.Error(), "not permitted in production") {
		t.Fatal("demo verifier did not fail before contacting dependencies", err)
	}
}
