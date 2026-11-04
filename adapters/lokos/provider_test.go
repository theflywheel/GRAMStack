package lokos

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
	"github.com/theflywheel/GRAMStack/sdk/go/adapter/conformance"
)

type fakeSource struct{ reads int }

func (s *fakeSource) Membership(context.Context, string) (Membership, error) {
	s.reads++
	return Membership{Status: "active", SavingsRupees: json.Number("12500.25")}, nil
}
func TestSDKBackedProjection(t *testing.T) {
	source := &fakeSource{}
	provider, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	subject := sdk.Subject{Reference: "verified-context"}
	reads := func() int { return source.reads }
	conformance.CheckProvider(t, provider, subject, "membership_status", json.RawMessage(`"active"`), reads)
	conformance.CheckProvider(t, provider, subject, "savings_summary", json.RawMessage(`{"amount":12500.25,"currency":"INR"}`), reads)
}

type partialSource struct{}

func (partialSource) Membership(context.Context, string) (Membership, error) {
	return Membership{Status: "active"}, nil
}
func TestUnrequestedFieldNotRequired(t *testing.T) {
	provider, _ := New(partialSource{})
	if _, err := sdk.ReadSelected(context.Background(), provider, sdk.Subject{Reference: "verified"}, []string{"membership_status"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.ReadSelected(context.Background(), provider, sdk.Subject{Reference: "verified"}, []string{"savings_summary"}); err == nil {
		t.Fatal("missing requested savings accepted")
	}
}
