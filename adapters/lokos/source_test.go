package lokos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

func TestDemoSourceCannotMasqueradeAsProduction(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			w.Write([]byte(`{"source_mapping_ref":"member-12345","demo":true}`))
		} else {
			w.Write([]byte(`{"membership_status":"active","savings_rupees":12500.75,"demo":true}`))
		}
	}))
	defer server.Close()
	source := HTTPSource{Origin: server.URL, Client: server.Client()}
	if _, err := source.VerifySubject(context.Background(), sdk.PrincipalContext{}, sdk.Hint{}); err == nil {
		t.Fatal("demo identity mapping accepted without explicit permission")
	}
	if _, err := source.Membership(context.Background(), "member-12345"); err == nil {
		t.Fatal("demo business record accepted without explicit permission")
	}
	source.AllowDemo = true
	if ref, err := source.VerifySubject(context.Background(), sdk.PrincipalContext{}, sdk.Hint{}); err != nil || ref != "member-12345" {
		t.Fatal("explicit demo mapping rejected", err)
	}
	if value, err := source.Membership(context.Background(), "member-12345"); err != nil || value.Status != "active" || value.SavingsRupees != "12500.75" {
		t.Fatal("explicit demo record rejected", err)
	}
}
