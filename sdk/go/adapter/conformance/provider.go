// Package conformance provides contract checks for registry adapter authors.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

// CheckProvider runs projection checks against a provider backed by a fixture
// that counts native reads. expected is the exact JSON for one authorized field
// of subject. Run with a fresh fixture and without concurrent source requests.
func CheckProvider(t *testing.T, provider adapter.Provider, subject adapter.Subject, field string, expected json.RawMessage, reads func() int) {
	t.Helper()
	if field == "" || !json.Valid(expected) {
		t.Fatal("conformance fixture requires a field and its valid JSON value")
	}
	baseline := reads()
	unsupported := field + ".unsupported"
	for _, name := range provider.Resource().Fields {
		if name == unsupported {
			t.Fatal("conformance fixture declares the reserved unsupported field", unsupported)
		}
	}
	ctx := context.Background()
	for _, fields := range [][]string{nil, {unsupported}, {field, field}} {
		data, err := adapter.ReadSelected(ctx, provider, subject, fields)
		if data != nil || !errors.Is(err, adapter.ErrFields) || reads() != baseline {
			t.Fatalf("invalid selection %v must fail before a source read", fields)
		}
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if data, err := adapter.ReadSelected(cancelled, provider, subject, []string{field}); data != nil || !errors.Is(err, context.Canceled) || reads() != baseline {
		t.Fatal("cancelled context must fail before a source read")
	}
	if data, err := adapter.ReadSelected(ctx, provider, adapter.Subject{}, []string{field}); data != nil || err == nil || reads() != baseline {
		t.Fatal("missing subject mapping must fail before a source read")
	}

	data, err := adapter.ReadSelected(ctx, provider, subject, []string{field})
	if err != nil || reads() != baseline+1 || len(data) != 1 || string(data[field]) != string(expected) {
		t.Fatalf("expected one source read and only the selected fixture field, got %v, %v", data, err)
	}
}
