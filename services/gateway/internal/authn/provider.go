package authn

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gowebpki/jcs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type ProviderConfig struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"-"`
	AllowDemo    bool   `json:"-"`
}
type provider struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	issuer   string
	client   *http.Client
}
type Principal struct {
	Assurance                   string
	Reference, Issuer, Subject  string
	AuthenticationTransactionID string
	ExpiresAt                   time.Time
	AuthenticatedAt             time.Time
	Methods                     []string
}

func trustedURL(raw string, loopback bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return loopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()
}
func newProvider(ctx context.Context, c ProviderConfig, callback string, loopback bool) (*provider, error) {
	if !trustedURL(c.Issuer, loopback) || !trustedURL(callback, loopback) || c.ClientID == "" || c.ClientSecret == "" {
		return nil, errors.New("trusted OIDC issuer, confidential client and callback required")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx = oidc.ClientContext(ctx, client)
	discovered, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return nil, errors.New("trusted OIDC discovery unavailable")
	}
	endpoint := discovered.Endpoint()
	var metadata struct {
		JWKS string `json:"jwks_uri"`
		Demo bool   `json:"gramstack_demo"`
	}
	if err = discovered.Claims(&metadata); err != nil {
		return nil, err
	}
	if metadata.Demo && !c.AllowDemo {
		return nil, errors.New("fictional identity provider requires explicit demo permission")
	}
	for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL, metadata.JWKS} {
		if !trustedURL(raw, loopback) {
			return nil, errors.New("untrusted OIDC protocol endpoint")
		}
	}
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	config := oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: endpoint, RedirectURL: callback, Scopes: []string{oidc.ScopeOpenID}}
	verifier := discovered.Verifier(&oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}})
	return &provider{config, verifier, c.Issuer, client}, nil
}
func (p *provider) begin(state, nonce, verifier string) string {
	return p.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0"))
}
func (p *provider) complete(ctx context.Context, code, nonce, verifier string) (*oidc.IDToken, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, ErrAuthentication
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 32768 {
		return nil, ErrAuthentication
	}
	id, err := p.verifier.Verify(ctx, raw)
	if err != nil || len(id.Audience) != 1 || id.Audience[0] != p.config.ClientID || id.Subject == "" || len(id.Subject) > 1024 || id.Issuer != p.issuer || id.IssuedAt.After(time.Now().Add(30*time.Second)) || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 || nonce == "" {
		return nil, ErrAuthentication
	}
	var rawClaims json.RawMessage
	if id.Claims(&rawClaims) != nil {
		return nil, ErrAuthentication
	}
	if _, err = jcs.Transform(rawClaims); err != nil {
		return nil, ErrAuthentication
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
		NotBefore       int64  `json:"nbf"`
	}
	if id.Claims(&claims) != nil || (claims.AuthorizedParty != "" && claims.AuthorizedParty != p.config.ClientID) || claims.NotBefore > time.Now().Add(30*time.Second).Unix() {
		return nil, ErrAuthentication
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return nil, ErrAuthentication
	}
	return id, nil
}
func sameOrigin(a, b string) bool {
	x, e := url.Parse(a)
	y, f := url.Parse(b)
	return e == nil && f == nil && x.Scheme == y.Scheme && x.Host == y.Host && x.User == nil && x.Fragment == ""
}
func cleanOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Path != "" && u.Path != "/") {
		return "", ErrAuthentication
	}
	return strings.TrimSuffix(raw, "/"), nil
}
