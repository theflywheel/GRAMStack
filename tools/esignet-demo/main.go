// esignet-demo onboards fictional identities and an ephemeral demo client.
// Run with the auth adapter's module context against loopback or private Railway.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	oidcadapter "github.com/theflywheel/GRAMStack/adapters/auth/oidc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "eSignet demo onboarding:", err)
		os.Exit(1)
	}
}
func run() error {
	callback := flag.String("callback", "", "exact gateway callback on literal loopback")
	directory := flag.String("directory", "", "existing private output directory for ephemeral key and profile")
	railway := flag.Bool("railway-demo", false, "onboard through the fixed private Railway demo APIs")
	flag.Parse()
	if *railway {
		target, err := railwayDemoTarget(*callback, *directory)
		if err != nil {
			return err
		}
		return onboardAt(target, *directory)
	}
	return onboard(*callback, *directory)
}

type onboardingTarget struct {
	callback, issuer, ui, api, mock string
	loopback                        bool
}

func railwayDemoTarget(callback, directory string) (onboardingTarget, error) {
	if os.Getenv("GRAMSTACK_ENV") != "demo" {
		return onboardingTarget{}, errors.New("Railway onboarding requires explicit demo mode")
	}
	if callback != "" {
		return onboardingTarget{}, errors.New("Railway demo uses a fixed callback")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return onboardingTarget{}, errors.New("existing private output directory required")
	}
	const origin = "https://gramstack-esignet-b9b69819.up.railway.app"
	return onboardingTarget{
		callback: "https://gramstack-gateway-b9b69819.up.railway.app/auth/provider/callback",
		issuer:   origin, ui: origin,
		api:  "http://esignet.railway.internal:8080",
		mock: "http://esignet-mock.railway.internal:8082",
	}, nil
}

func onboardingHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func loopbackOrigin(variable, fallback string) (string, error) {
	port := os.Getenv(variable)
	if port == "" {
		port = fallback
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return "", fmt.Errorf("%s must be a canonical TCP port", variable)
	}
	return "http://127.0.0.1:" + port, nil
}
func onboard(callback, directory string) error {
	if os.Getenv("GRAMSTACK_ENV") != "development" {
		return errors.New("explicit development mode required")
	}
	u, err := url.Parse(callback)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/auth/provider/callback" {
		return errors.New("exact loopback callback required")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("literal loopback callback required")
	}
	if directory == "" {
		return errors.New("private output directory required")
	}
	api, err := loopbackOrigin("ESIGNET_DEMO_API_PORT", "19080")
	if err != nil {
		return err
	}
	issuer := api
	if os.Getenv("ESIGNET_DEMO_ISSUER_PORT") != "" {
		issuer, err = loopbackOrigin("ESIGNET_DEMO_ISSUER_PORT", "")
		if err != nil {
			return err
		}
	}
	mock, err := loopbackOrigin("ESIGNET_DEMO_MOCK_PORT", "19082")
	if err != nil {
		return err
	}
	ui, err := loopbackOrigin("ESIGNET_DEMO_UI_PORT", "19300")
	if err != nil {
		return err
	}
	return onboardAt(onboardingTarget{callback: callback, api: api, issuer: issuer, ui: ui, mock: mock, loopback: true}, directory)
}

func onboardAt(target onboardingTarget, directory string) error {
	client := onboardingHTTPClient()
	defer client.CloseIdleConnections()
	post := func(endpoint string, input any, duplicateFixture bool) error {
		body, err := json.Marshal(map[string]any{"requestTime": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "request": input})
		if err != nil {
			return err
		}
		response, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			return errors.New("isolated demo API unavailable")
		}
		defer response.Body.Close()
		var result struct {
			Errors []struct {
				Code string `json:"errorCode"`
			} `json:"errors"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil {
			return errors.New("invalid demo API response")
		}
		if duplicateFixture && len(result.Errors) == 1 && result.Errors[0].Code == "duplicate_individual_id" {
			return nil
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("demo API rejected request: %s", result.Errors[0].Code)
		}
		if response.StatusCode != 200 && response.StatusCode != 201 {
			return errors.New("demo API rejected onboarding")
		}
		return nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	writePrivate := func(name string, b []byte) error {
		f, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("private output file already exists or is unavailable")
		}
		_, err = f.Write(b)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	if err = writePrivate("esignet-client.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		return err
	}
	clientID := "gst-" + rand.Text()
	if err = post(target.api+"/client-mgmt/client", map[string]any{"clientId": clientID, "clientName": "GRAMStack local demo", "clientNameLangMap": map[string]string{"eng": "GRAMStack local demo"}, "relyingPartyId": clientID, "logoUri": "https://example.com/gramstack.svg", "redirectUris": []string{target.callback}, "userClaims": []string{"name"}, "authContextRefs": []string{"mosip:idp:acr:generated-code"}, "publicKey": jose.JSONWebKey{Key: &key.PublicKey, KeyID: "client-key", Algorithm: "PS256", Use: "sig"}, "grantTypes": []string{"authorization_code"}, "clientAuthMethods": []string{"private_key_jwt"}, "additionalConfig": map[string]any{"require_pkce": true, "require_pushed_authorization_requests": false, "dpop_bound_access_tokens": false}}, false); err != nil {
		return err
	}
	for _, person := range []struct{ id, name string }{{"9000000001", "Sunita"}, {"9000000002", "Rani"}} {
		translated := func(value string) []map[string]string {
			return []map[string]string{{"language": "eng", "value": value}}
		}
		err = post(target.mock+"/v1/mock-identity-system/identity", map[string]any{"individualId": person.id, "pin": "111111", "email": person.name + "@example.invalid", "phone": "+12025550123", "dateOfBirth": "1990/01/01", "postalCode": "110001", "country": translated("Demo Country"), "region": translated("Demo Region"), "locality": translated("Demo Town"), "gender": translated("Female"), "encodedPhoto": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGM4YWQERAwQCgAjNgSxRnwWMwAAAABJRU5ErkJggg==", "fullName": translated(person.name + " Demo"), "givenName": translated(person.name), "familyName": translated("Demo"), "preferredLang": "eng", "locale": "en"}, true)
		if err != nil {
			return err
		}
	}
	profile, err := json.Marshal(map[string]any{"schema_version": "gramstack.auth.oidc/v1", "issuer": target.issuer, "client_id": clientID, "token_endpoint_auth_method": "private_key_jwt", "assertion_key_id": "client-key", "assertion_audience": "issuer", "id_token_algorithms": []string{"PS256"}, "jwks_certificate_metadata": "ignore"})
	if err != nil {
		return err
	}
	if err = writePrivate("esignet-profile.json", profile); err != nil {
		return err
	}
	var provider oidcadapter.Profile
	if err = json.Unmarshal(profile, &provider); err != nil {
		return err
	}
	bindings := map[string]string{}
	for _, fixture := range []struct{ person, member string }{{"9000000001", "member-12345"}, {"9000000002", "member-67890"}} {
		subject, err := fixtureSubject(provider, oidcadapter.Credentials{PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})}, target, fixture.person)
		if err != nil {
			return err
		}
		bindings[fixture.member] = subject
	}
	rawBindings, err := json.Marshal(bindings)
	if err != nil {
		return err
	}
	if err = writePrivate("esignet-bindings.json", rawBindings); err != nil {
		return err
	}
	fmt.Println("PASS: isolated eSignet client and fictional Sunita/Rani fixtures onboarded; private key written with mode 0600")
	return nil
}
