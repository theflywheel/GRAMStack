// esignet-demo onboards synthetic identities and an ephemeral client exclusively
// into the isolated loopback stack. Run with the auth adapter's module context.
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
	flag.Parse()
	return onboard(*callback, *directory)
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
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(endpoint string, input any, duplicateFixture bool) error {
		body, err := json.Marshal(map[string]any{"requestTime": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "request": input})
		if err != nil {
			return err
		}
		response, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			return errors.New("isolated local API unavailable")
		}
		defer response.Body.Close()
		var result struct {
			Errors []struct {
				Code string `json:"errorCode"`
			} `json:"errors"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil {
			return errors.New("invalid local API response")
		}
		if duplicateFixture && len(result.Errors) == 1 && result.Errors[0].Code == "duplicate_individual_id" {
			return nil
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("local API rejected request: %s", result.Errors[0].Code)
		}
		if response.StatusCode != 200 && response.StatusCode != 201 {
			return errors.New("local API rejected onboarding")
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
	if err = post(api+"/client-mgmt/client", map[string]any{"clientId": clientID, "clientName": "GRAMStack local demo", "clientNameLangMap": map[string]string{"eng": "GRAMStack local demo"}, "relyingPartyId": clientID, "logoUri": "https://example.com/gramstack.svg", "redirectUris": []string{callback}, "userClaims": []string{"name"}, "authContextRefs": []string{"mosip:idp:acr:generated-code"}, "publicKey": jose.JSONWebKey{Key: &key.PublicKey, KeyID: "client-key", Algorithm: "PS256", Use: "sig"}, "grantTypes": []string{"authorization_code"}, "clientAuthMethods": []string{"private_key_jwt"}, "additionalConfig": map[string]any{"require_pkce": true, "require_pushed_authorization_requests": false, "dpop_bound_access_tokens": false}}, false); err != nil {
		return err
	}
	for _, person := range []struct{ id, name string }{{"9000000001", "Sunita"}, {"9000000002", "Rani"}} {
		translated := func(value string) []map[string]string {
			return []map[string]string{{"language": "eng", "value": value}}
		}
		err = post(mock+"/v1/mock-identity-system/identity", map[string]any{"individualId": person.id, "pin": "111111", "email": person.name + "@example.invalid", "phone": "+12025550123", "dateOfBirth": "1990/01/01", "postalCode": "110001", "country": translated("Demo Country"), "region": translated("Demo Region"), "locality": translated("Demo Town"), "gender": translated("Female"), "encodedPhoto": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGM4YWQERAwQCgAjNgSxRnwWMwAAAABJRU5ErkJggg==", "fullName": translated(person.name + " Demo"), "givenName": translated(person.name), "familyName": translated("Demo"), "preferredLang": "eng", "locale": "en"}, true)
		if err != nil {
			return err
		}
	}
	profile, err := json.Marshal(map[string]any{"schema_version": "gramstack.auth.oidc/v1", "issuer": issuer, "client_id": clientID, "token_endpoint_auth_method": "private_key_jwt", "assertion_key_id": "client-key", "assertion_audience": "issuer", "id_token_algorithms": []string{"PS256"}, "jwks_certificate_metadata": "ignore"})
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
		subject, err := fixtureSubject(provider, oidcadapter.Credentials{PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})}, callback, fixture.person, ui, api)
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
