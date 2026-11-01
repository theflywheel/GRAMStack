package lokos

import (
	"context"
	"encoding/json"
	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
	"testing"
)

type fakeSource struct{}

func (fakeSource) Membership(context.Context, string) (Membership, error) {
	return Membership{Status: "active", SavingsRupees: json.Number("12500.25")}, nil
}
func TestSDKBackedProjection(t *testing.T) {
	provider, err := New(fakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := sdk.ReadSelected(context.Background(), provider, sdk.Subject{Reference: "verified-context"}, []string{"membership_status"})
	if err != nil || len(data) != 1 || string(data["membership_status"]) != `"active"` {
		t.Fatalf("%s %v", data, err)
	}
	data, err = sdk.ReadSelected(context.Background(), provider, sdk.Subject{Reference: "verified-context"}, []string{"savings_summary"})
	if err != nil || string(data["savings_summary"]) != `{"amount":12500.25,"currency":"INR"}` {
		t.Fatalf("%s %v", data, err)
	}
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
