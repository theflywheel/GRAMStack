package gramstack

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gowebpki/jcs"
	"golang.org/x/oauth2"
)

var ErrAuthentication = errors.New("GRAMStack application sign-in failed")

// LoginStore must atomically consume a record only when its browser binding and expiry match.
// Production implementations share encrypted TTL state across application replicas.
type LoginStore interface {
	SaveLogin(context.Context, string, string, LoginTransaction) error
	TakeLogin(context.Context, string, string) (LoginTransaction, error)
}
type LoginTransaction struct {
	State     string    `json:"state"`
	Nonce     string    `json:"nonce"`
	Verifier  string    `json:"verifier"`
	StartedAt time.Time `json:"started_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type AuthConfig struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
	CA                                          []byte
	AllowLoopback                               bool
}
type Identity struct {
	Issuer          string    `json:"issuer"`
	Subject         string    `json:"subject"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}
type AuthClient struct {
	config   AuthConfig
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	http     *http.Client
	store    LoginStore
	cookie   string
}

func NewAuth(ctx context.Context, c AuthConfig, store LoginStore) (*AuthClient, error) {
	if !trustedURL(c.Issuer, c.AllowLoopback, false) || !trustedURL(c.RedirectURL, c.AllowLoopback, false) || !idPattern.MatchString(c.ClientID) || store == nil {
		return nil, ErrInput
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, ErrAuthentication
	}
	if len(c.CA) > 0 && !roots.AppendCertsFromPEM(c.CA) {
		return nil, ErrInput
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrAuthentication }, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}}
	discoveryCtx := oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(discoveryCtx, c.Issuer)
	if err != nil {
		return nil, ErrAuthentication
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if provider.Claims(&metadata) != nil {
		return nil, ErrAuthentication
	}
	endpoint := provider.Endpoint()
	for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL, metadata.JWKS} {
		if !trustedURL(raw, c.AllowLoopback, false) || !sameOrigin(raw, c.Issuer) {
			return nil, ErrAuthentication
		}
	}
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	if c.ClientSecret == "" {
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	config := oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: endpoint, RedirectURL: c.RedirectURL, Scopes: []string{"openid"}}
	prefix := "__Host-gramstack-login-"
	if strings.HasPrefix(c.RedirectURL, "http://") {
		prefix = "gramstack-login-dev-"
	}
	digest := sha256.Sum256([]byte(c.ClientID))
	return &AuthClient{c, config, oidc.NewVerifier(c.Issuer, oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), metadata.JWKS), &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}}), client, store, prefix + hex.EncodeToString(digest[:6])}, nil
}
func sameOrigin(left, right string) bool {
	a, err := url.Parse(left)
	b, other := url.Parse(right)
	return err == nil && other == nil && a.Scheme == b.Scheme && a.Host == b.Host
}
func (c *AuthClient) Begin(ctx context.Context, browserBinding string) (string, error) {
	if len(browserBinding) < 26 || len(browserBinding) > 256 {
		return "", ErrInput
	}
	now := time.Now().UTC()
	transaction := LoginTransaction{State: rand.Text(), Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier(), StartedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	if c.store.SaveLogin(ctx, transaction.State, browserBinding, transaction) != nil {
		return "", ErrAuthentication
	}
	return c.oauth.AuthCodeURL(transaction.State, oidc.Nonce(transaction.Nonce), oauth2.S256ChallengeOption(transaction.Verifier), oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0")), nil
}

// Complete consumes the browser-bound transaction before exchanging the authorization code.
func (c *AuthClient) Complete(ctx context.Context, browserBinding, callbackURL string) (Identity, error) {
	callback, err := url.Parse(callbackURL)
	configured, _ := url.Parse(c.config.RedirectURL)
	if err != nil || callback.Scheme != configured.Scheme || callback.Host != configured.Host || callback.Path != configured.Path || callback.User != nil || callback.Fragment != "" || len(browserBinding) < 26 || len(browserBinding) > 256 {
		return Identity{}, ErrAuthentication
	}
	q := callback.Query()
	if len(q["state"]) != 1 || len(q["code"]) != 1 || q.Get("error") != "" || len(q.Get("state")) != 26 || len(q.Get("code")) < 1 || len(q.Get("code")) > 8192 {
		return Identity{}, ErrAuthentication
	}
	transaction, err := c.store.TakeLogin(ctx, q.Get("state"), browserBinding)
	if err != nil || transaction.State != q.Get("state") || time.Now().After(transaction.ExpiresAt) || len(transaction.Nonce) < 26 || len(transaction.Verifier) < 43 {
		return Identity{}, ErrAuthentication
	}
	token, err := c.oauth.Exchange(oidc.ClientContext(ctx, c.http), q.Get("code"), oauth2.VerifierOption(transaction.Verifier))
	if err != nil {
		return Identity{}, ErrAuthentication
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 32768 {
		return Identity{}, ErrAuthentication
	}
	id, err := c.verifier.Verify(oidc.ClientContext(ctx, c.http), raw)
	if err != nil || id.Subject == "" || len(id.Subject) > 1024 || id.Issuer != c.config.Issuer || len(id.Audience) != 1 || id.Audience[0] != c.config.ClientID || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(transaction.Nonce)) != 1 {
		return Identity{}, ErrAuthentication
	}
	var claims struct {
		IssuedAt        int64  `json:"iat"`
		AuthTime        int64  `json:"auth_time"`
		NotBefore       int64  `json:"nbf"`
		AuthorizedParty string `json:"azp"`
	}
	var payload json.RawMessage
	if id.Claims(&payload) != nil {
		return Identity{}, ErrAuthentication
	}
	if _, err = jcs.Transform(payload); err != nil || id.Claims(&claims) != nil {
		return Identity{}, ErrAuthentication
	}
	now := time.Now()
	if claims.IssuedAt < 1 || claims.AuthTime < transaction.StartedAt.Truncate(time.Second).Unix() || claims.AuthTime > now.Add(30*time.Second).Unix() || now.Unix()-claims.AuthTime > 300 || claims.NotBefore > now.Add(30*time.Second).Unix() || id.IssuedAt.After(now.Add(30*time.Second)) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != c.config.ClientID) {
		return Identity{}, ErrAuthentication
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return Identity{}, ErrAuthentication
	}
	return Identity{Issuer: id.Issuer, Subject: id.Subject, AuthenticatedAt: time.Unix(claims.AuthTime, 0).UTC(), ExpiresAt: id.Expiry.UTC()}, nil
}

// Login and Callback are framework-free HTTP entry points. The application owns its authenticated session.
func (c *AuthClient) Login(w http.ResponseWriter, r *http.Request) {
	binding := rand.Text()
	redirect, err := c.Begin(r.Context(), binding)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err != nil {
		http.Error(w, "Sign-in temporarily unavailable.", 503)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: c.cookie, Value: binding, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(c.config.RedirectURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, redirect, http.StatusFound)
}
func (c *AuthClient) Callback(w http.ResponseWriter, r *http.Request) (Identity, error) {
	values := r.CookiesNamed(c.cookie)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, &http.Cookie{Name: c.cookie, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(c.config.RedirectURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if len(values) != 1 {
		return Identity{}, ErrAuthentication
	}
	return c.Complete(r.Context(), values[0].Value, c.config.RedirectURL+"?"+r.URL.RawQuery)
}
