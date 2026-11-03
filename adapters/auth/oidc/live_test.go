package oidcadapter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// This test onboards an ephemeral client and a fictional person ONLY in the
// loopback official eSignet + mock identity stack. It never accepts a remote URL.
func TestActualESignet(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_ESIGNET") != "1" {
		t.Skip("requires isolated local official eSignet stack")
	}
	const issuer = "http://127.0.0.1:19080"
	const callback = "http://127.0.0.1:19999/auth/provider/callback"
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(endpoint string, input, output any) {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Post(endpoint, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal("local API unavailable:", err)
		}
		defer response.Body.Close()

		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Errors []struct {
				Code    string `json:"errorCode"`
				Message string `json:"errorMessage"`
			} `json:"errors"`
		}
		parseErr := json.Unmarshal(body, &envelope)
		if parseErr == nil && endpoint == "http://127.0.0.1:19082/v1/mock-identity-system/identity" && len(envelope.Errors) == 1 && envelope.Errors[0].Code == "duplicate_individual_id" {
			return
		}
		if parseErr != nil || len(envelope.Errors) != 0 || (response.StatusCode != 200 && response.StatusCode != 201) {
			t.Fatalf("local API %s returned errors: %+v", endpoint, envelope.Errors)
		}
		if json.Unmarshal(body, output) != nil {
			t.Fatal("invalid local API JSON")
		}
	}
	envelope := func(request any) any {
		return map[string]any{"requestTime": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "request": request}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	clientID := "gst-" + rand.Text()
	jwk := jose.JSONWebKey{Key: &key.PublicKey, KeyID: "client-key", Algorithm: "PS256", Use: "sig"}
	var onboard map[string]any
	post(issuer+"/client-mgmt/client", envelope(map[string]any{"clientId": clientID, "clientName": "GRAMStack local conformance", "clientNameLangMap": map[string]string{"eng": "GRAMStack local conformance"}, "relyingPartyId": clientID, "logoUri": "https://example.com/gramstack.svg", "redirectUris": []string{callback}, "userClaims": []string{"name"}, "authContextRefs": []string{"mosip:idp:acr:generated-code"}, "publicKey": jwk, "grantTypes": []string{"authorization_code"}, "clientAuthMethods": []string{"private_key_jwt"}, "additionalConfig": map[string]any{"require_pkce": true, "require_pushed_authorization_requests": false, "dpop_bound_access_tokens": false}}), &onboard)
	// Public fictional fixture. Never use a real government identifier or contact.
	var seeded map[string]any
	post("http://127.0.0.1:19082/v1/mock-identity-system/identity", envelope(map[string]any{"individualId": "9000000001", "pin": "111111", "email": "sunita@example.invalid", "phone": "+12025550123", "dateOfBirth": "1990/01/01", "postalCode": "110001", "country": []map[string]string{{"language": "eng", "value": "Demo Country"}}, "region": []map[string]string{{"language": "eng", "value": "Demo Region"}}, "locality": []map[string]string{{"language": "eng", "value": "Demo Town"}}, "gender": []map[string]string{{"language": "eng", "value": "Female"}}, "encodedPhoto": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGM4YWQERAwQCgAjNgSxRnwWMwAAAABJRU5ErkJggg==", "fullName": []map[string]string{{"language": "eng", "value": "Sunita Demo"}}, "givenName": []map[string]string{{"language": "eng", "value": "Sunita"}}, "familyName": []map[string]string{{"language": "eng", "value": "Demo"}}, "preferredLang": "eng", "locale": "en"}), &seeded)
	profile := Profile{SchemaVersion: "gramstack.auth.oidc/v1", JWKSCertificateMetadata: "ignore", Issuer: issuer, ClientID: clientID, TokenAuthMethod: "private_key_jwt", AssertionAudience: "issuer", IDTokenAlgorithms: []string{"PS256"}, AssertionKeyID: "client-key"}
	adapter, err := New(context.Background(), profile, Credentials{PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})}, callback, Options{AllowLoopback: true, AllowDemo: true})
	if err != nil {
		t.Fatal(err)
	}
	tx := Transaction{ID: rand.Text(), State: rand.Text(), Nonce: rand.Text(), Verifier: rand.Text() + rand.Text()}
	authURL, err := adapter.Begin(tx)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 302 {
		t.Fatalf("authorization status %d", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	executionID, authID := location.Query().Get("executionId"), location.Query().Get("authId")
	if executionID == "" || authID == "" {
		t.Fatal("missing browser flow correlation")
	}
	var flow struct {
		Challenge string `json:"challengeToken"`
		Status    string `json:"flowStatus"`
		Assertion string `json:"assertion"`
		Data      struct {
			Inputs []struct {
				ID string `json:"identifier"`
			} `json:"inputs"`
			Actions []struct {
				Ref string `json:"ref"`
			} `json:"actions"`
		} `json:"data"`
	}
	post(issuer+"/flow/execute", map[string]any{"executionId": executionID}, &flow)
	for step := 0; step < 8 && flow.Status != "COMPLETE"; step++ {
		if len(flow.Data.Actions) == 0 {
			t.Fatalf("no action at step %d status %s", step, flow.Status)
		}
		inputs := map[string]string{}
		for _, input := range flow.Data.Inputs {
			switch input.ID {
			case "username":
				inputs[input.ID] = "9000000001"
			case "otp":
				inputs[input.ID] = "111111"
			case "captcha_token":
				inputs[input.ID] = "local-demo-captcha-token"
			case "consent_decisions":
				inputs[input.ID] = `{"approved":true,"purposes":[]}`
			default:
				t.Fatalf("unexpected eSignet input %q", input.ID)
			}
		}
		payload := map[string]any{"executionId": executionID, "challengeToken": flow.Challenge, "action": flow.Data.Actions[0].Ref, "inputs": inputs}
		flow = struct {
			Challenge string `json:"challengeToken"`
			Status    string `json:"flowStatus"`
			Assertion string `json:"assertion"`
			Data      struct {
				Inputs []struct {
					ID string `json:"identifier"`
				} `json:"inputs"`
				Actions []struct {
					Ref string `json:"ref"`
				} `json:"actions"`
			} `json:"data"`
		}{}
		post(issuer+"/flow/execute", payload, &flow)
	}
	if flow.Status != "COMPLETE" || flow.Assertion == "" {
		t.Fatal("identity authentication did not complete")
	}
	var completed struct {
		Redirect      string `json:"redirect_uri"`
		RedirectCamel string `json:"redirectUri"`
	}
	post(issuer+"/oauth2/auth/callback", map[string]string{"authId": authID, "assertion": flow.Assertion}, &completed)
	redirect := completed.Redirect
	if redirect == "" {
		redirect = completed.RedirectCamel
	}
	result, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scheme != "http" || result.Host != "127.0.0.1:19999" || result.Path != "/auth/provider/callback" || result.Query().Get("state") != tx.State || result.Query().Get("code") == "" || result.Query().Get("error") != "" {
		t.Fatal("invalid callback binding")
	}
	principal, err := adapter.Complete(context.Background(), result.Query().Get("code"), tx)
	if err != nil {
		t.Fatal("real eSignet token exchange:", err)
	}
	if principal.Issuer != issuer || principal.Subject == "" || principal.AuthenticatedAt.IsZero() || principal.TransactionID != tx.ID {
		t.Fatal("missing verified principal")
	}
	if _, err = adapter.Complete(context.Background(), result.Query().Get("code"), tx); err == nil {
		t.Fatal("authorization code replay accepted")
	}
	t.Log("actual eSignet private_key_jwt + PS256 + PKCE + nonce + fresh authentication + code replay denial passed; identity backend is mock")
}
