package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFictionalCodePKCEAndReplay(t *testing.T) {
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	config := Config{Issuer: server.URL, ClientID: "test-client", ClientSecret: "synthetic-client-secret", RedirectURI: "http://127.0.0.1:9911/callback", DemoEnabled: true, FormReturnOrigins: []string{"http://127.0.0.1:9922"}}
	provider, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.FormReturnOrigins[0] = "https://untrusted.example"
	handler = provider.Handler()
	if _, err := New(Config{Issuer: server.URL, ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURI: config.RedirectURI}); err == nil {
		t.Fatal("demo permission not required")
	}
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {config.ClientID}, "redirect_uri": {config.RedirectURI}, "response_type": {"code"}, "scope": {"openid"}, "state": {"state-value"}, "nonce": {"nonce-value"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(server.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	csp := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "form-action 'self' http://127.0.0.1:9911 http://127.0.0.1:9922;") || strings.Contains(csp, "*") {
		t.Fatal("registered browser return origin missing or broadened")
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 {
		t.Fatal("authorisation form unavailable")
	}
	find := func(name string) string {
		m := regexp.MustCompile(`name="` + name + `" value="([A-Za-z0-9]+)"`).FindStringSubmatch(string(raw))
		if len(m) != 2 {
			t.Fatal("missing form binding")
		}
		return m[1]
	}
	changed := query.Get("redirect_uri")
	query.Set("redirect_uri", "http://127.0.0.1:9922/other")
	bad, err := client.Get(server.URL + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != 400 {
		t.Fatal("CSP navigation origin became an OAuth redirect registration")
	}
	query.Set("redirect_uri", changed)
	selection := url.Values{"ticket": {find("ticket")}, "csrf": {find("csrf")}, "identity": {"sunita"}}
	response, err = client.PostForm(server.URL+"/authorize", selection)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 303 {
		t.Fatal("selection rejected")
	}
	target, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("state") != "state-value" {
		t.Fatal("state changed")
	}
	code := target.Query().Get("code")
	exchange := func(v string) (int, map[string]any) {
		body := url.Values{"grant_type": {"authorization_code"}, "redirect_uri": {config.RedirectURI}, "code": {code}, "code_verifier": {v}}
		req, _ := http.NewRequest("POST", server.URL+"/token", strings.NewReader(body.Encode()))
		req.SetBasicAuth(config.ClientID, config.ClientSecret)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var value map[string]any
		if json.NewDecoder(res.Body).Decode(&value) != nil {
			t.Fatal("invalid token response")
		}
		return res.StatusCode, value
	}
	if status, _ := exchange("wrong-verifier"); status != 400 {
		t.Fatal("PKCE bypass accepted")
	}
	if status, value := exchange(verifier); status != 200 || value["id_token"] == nil {
		t.Fatal("valid exchange failed")
	}
	if status, _ := exchange(verifier); status != 400 {
		t.Fatal("code replay accepted")
	}
	response, err = client.PostForm(server.URL+"/authorize", selection)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("identity form replay accepted")
	}
}

func TestFormReturnOriginsAreExplicit(t *testing.T) {
	for _, origin := range []string{"https://example.test/path", "https://example.test?", "https://example.test;form-action", "https://*.example.test", "https://user:secret@example.test"} {
		_, err := New(Config{Issuer: "https://idp.example.test", ClientID: "client", ClientSecret: "synthetic-client-secret", RedirectURI: "https://app.example.test/callback", DemoEnabled: true, FormReturnOrigins: []string{origin}})
		if err == nil {
			t.Fatal("unsafe form navigation source accepted")
		}
	}
}
