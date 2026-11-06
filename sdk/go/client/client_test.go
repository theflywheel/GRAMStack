package gramstack

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStrictBoundaryJSON(t *testing.T) {
	for _, raw := range []string{`{"status":"CONSENTED","status":"CANCELLED"}`, `{"Status":"CONSENTED"}`, `{"status":"CONSENTED","extra":true}`, `{"status":null}`} {
		var value struct {
			Status string `json:"status"`
		}
		if decode([]byte(raw), &value) == nil {
			t.Fatalf("accepted ambiguous input %s", raw)
		}
	}
}
func TestPinnedOriginsAndExplicitPolling(t *testing.T) {
	for _, origin := range []string{"http://remote.example", "https://user:secret@example.test", "https://example.test/other", "https://example.test?"} {
		if _, err := New(Config{PublicOrigin: origin, TokenURL: "https://issuer.test/oauth2/token", ClientID: "test", ClientSecret: "secret"}); err == nil {
			t.Fatal("accepted untrusted origin")
		}
	}
	if validFields([]string{"membership_status", "membership_status"}) || validFields(nil) {
		t.Fatal("invalid fields accepted")
	}
}
func TestMemoryLoginStateIsBoundAndConsumed(t *testing.T) {
	store := NewMemoryLoginStore()
	transaction := LoginTransaction{State: rand.Text(), Nonce: rand.Text(), Verifier: strings.Repeat("x", 43), StartedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
	binding := rand.Text()
	if err := store.SaveLogin(context.Background(), transaction.State, binding, transaction); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakeLogin(context.Background(), transaction.State, rand.Text()); err == nil {
		t.Fatal("cross-browser state accepted")
	}
	if _, err := store.TakeLogin(context.Background(), transaction.State, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TakeLogin(context.Background(), transaction.State, binding); err == nil {
		t.Fatal("state replay accepted")
	}
}

type fixtureConfig struct {
	PublicOrigin string `json:"public_origin"`
	TokenURL     string `json:"token_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	DataOrigin   string `json:"data_origin"`
	ServerName   string `json:"data_server_name"`
	CAFile       string `json:"ca_file"`
	CertFile     string `json:"certificate_file"`
	KeyFile      string `json:"key_file"`
	Issuer       string `json:"issuer"`
	RedirectURL  string `json:"redirect_url"`
}

func fixture(t *testing.T) (fixtureConfig, *Client) {
	t.Helper()
	path := os.Getenv("GRAMSTACK_SDK_FIXTURE")
	if path == "" {
		t.Skip("run scripts/test-client-sdks.sh for isolated cross-language transport conformance")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config fixtureConfig
	if json.Unmarshal(raw, &config) != nil || !strings.HasPrefix(config.PublicOrigin, "http://127.0.0.1:") {
		t.Fatal("fixture must be isolated loopback")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	client, err := New(Config{PublicOrigin: config.PublicOrigin, TokenURL: config.TokenURL, ClientID: config.ClientID, ClientSecret: config.ClientSecret, DataOrigin: config.DataOrigin, DataServerName: config.ServerName, CA: read(config.CAFile), Certificate: read(config.CertFile), Key: read(config.KeyFile), AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	return config, client
}
func control(t *testing.T, config fixtureConfig, path string, body any) map[string]json.RawMessage {
	t.Helper()
	method := "GET"
	var data []byte
	if body != nil {
		method = "POST"
		data, _ = json.Marshal(body)
	}
	request, _ := http.NewRequest(method, config.PublicOrigin+"/_fixture/"+path, bytes.NewReader(data))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	var value map[string]json.RawMessage
	if err != nil || response.StatusCode != 200 || json.Unmarshal(raw, &value) != nil {
		t.Fatal("fixture control failed")
	}
	return value
}
func requestInput() AccessRequest {
	return AccessRequest{Registry: "lokos", Resource: "shg.membership", SubjectHint: SubjectHint{Type: "member_reference", Value: "member-12345"}, Fields: []string{"membership_status"}, Purpose: "credit_eligibility", Access: Access{Mode: "single_use", ValidForSeconds: 300}, RecipientTerms: RecipientTerms{RetentionPolicyID: "terms-v1"}}
}
func TestSharedProtocolFixture(t *testing.T) {
	ctx := context.Background()
	t.Run("lifecycle", func(t *testing.T) {
		config, client := fixture(t)
		control(t, config, "reset", map[string]string{})
		accepted, err := client.Create(ctx, "test-create", requestInput())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = client.Status(ctx, accepted.RequestID); err != nil {
			t.Fatal(err)
		}
		grant, err := client.IssueGrant(ctx, accepted.RequestID, "test-grant")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprintf("%v %#v", grant, grant), "fixture.grant.jwt") {
			t.Fatal("grant leaked")
		}
		result, err := client.Retrieve(ctx, grant, requestInput().Fields)
		if err != nil || result.ReceiptID != "receipt-1" {
			t.Fatal("retrieval failed", err)
		}
		copy := *grant
		if _, err = client.Retrieve(ctx, &copy, requestInput().Fields); !errors.Is(err, ErrConsumed) {
			t.Fatal("copied grant was reusable")
		}
		counts := control(t, config, "counts", nil)
		if string(counts["fetches"]) != "1" || string(counts["tokens"]) != "1" {
			t.Fatal("unexpected request retry")
		}
		if state, err := client.Cancel(ctx, accepted.RequestID); err != nil || state.Status != "CANCELLED" {
			t.Fatal("cancel failed", err)
		}
	})
	for _, mode := range []string{"redirect", "wrong_version", "duplicate_json", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			config, client := fixture(t)
			control(t, config, "reset", map[string]string{"mode": mode})
			if _, err := client.Status(ctx, "request-1"); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
			if string(control(t, config, "counts", nil)["traps"]) != "0" {
				t.Fatal("redirect followed")
			}
		})
	}
	for _, mode := range []string{"extra_field", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			config, client := fixture(t)
			control(t, config, "reset", map[string]string{"mode": mode})
			if _, err := client.Create(ctx, "test-create", requestInput()); err != nil {
				t.Fatal(err)
			}
			grant, err := client.IssueGrant(ctx, "request-1", "test-grant")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Retrieve(ctx, grant, requestInput().Fields); err == nil {
				t.Fatal("unsafe retrieval accepted")
			}
			if _, err = client.Retrieve(ctx, grant, requestInput().Fields); !errors.Is(err, ErrConsumed) {
				t.Fatal("retrieval was retried")
			}
			if string(control(t, config, "counts", nil)["fetches"]) != "1" {
				t.Fatal("repeated native fetch")
			}
		})
	}
	t.Run("concurrent", func(t *testing.T) {
		config, client := fixture(t)
		control(t, config, "reset", map[string]string{})
		_, _ = client.Create(ctx, "test-create", requestInput())
		grant, err := client.IssueGrant(ctx, "request-1", "test-grant")
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { _, err := client.Retrieve(ctx, grant, requestInput().Fields); results <- err })
		}
		wg.Wait()
		close(results)
		success, blocked := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, ErrConsumed) {
				blocked++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || blocked != 1 {
			t.Fatal("grant was not single-attempt")
		}
	})
}
func TestSharedIdentityFixture(t *testing.T) {
	for _, mode := range []string{"normal", "wrong_nonce", "wrong_audience", "extra_audience", "wrong_issuer", "expired", "invalid_signature", "missing_auth_time"} {
		t.Run(mode, func(t *testing.T) {
			config, _ := fixture(t)
			control(t, config, "reset", map[string]string{"mode": mode})
			auth, err := NewAuth(context.Background(), AuthConfig{Issuer: config.Issuer, ClientID: "app-one", ClientSecret: "fixture-secret", RedirectURL: config.RedirectURL, AllowLoopback: true}, NewMemoryLoginStore())
			if err != nil {
				t.Fatal(err)
			}
			binding := rand.Text()
			authorize, err := auth.Begin(context.Background(), binding)
			if err != nil {
				t.Fatal(err)
			}
			reply := control(t, config, "authorize", map[string]string{"url": authorize})
			var callback string
			if json.Unmarshal(reply["callback"], &callback) != nil {
				t.Fatal("fixture callback")
			}
			if _, err = auth.Complete(context.Background(), rand.Text(), callback); err == nil {
				t.Fatal("cross-browser callback accepted")
			}
			identity, err := auth.Complete(context.Background(), binding, callback)
			if mode == "normal" {
				if err != nil || identity.Subject != "app_fixture_subject" {
					t.Fatal("valid sign-in rejected", err)
				}
			} else if err == nil {
				t.Fatal("invalid ID token accepted")
			}
			if _, err = auth.Complete(context.Background(), binding, callback); err == nil {
				t.Fatal("callback replay accepted")
			}
		})
	}
}
func TestAuthRejectsUnpinnedEndpoints(t *testing.T) {
	_, err := NewAuth(context.Background(), AuthConfig{Issuer: "http://untrusted.example", ClientID: "app-one", RedirectURL: "https://app.example/callback"}, NewMemoryLoginStore())
	if err == nil {
		t.Fatal("unencrypted issuer accepted")
	}
}
