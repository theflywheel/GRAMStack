// Package oidcadapter implements transaction-bound OIDC federation without storing citizen accounts.
package oidcadapter

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/gowebpki/jcs"
	"golang.org/x/oauth2"
)

var ErrAuthentication = errors.New("identity provider authentication failed")

// Profile contains public trust configuration. Private keys and secrets are supplied separately.
type Profile struct {
	JWKSCertificateMetadata string   `json:"jwks_certificate_metadata,omitempty"`
	AssertionAudience       string   `json:"assertion_audience,omitempty"`
	SchemaVersion           string   `json:"schema_version"`
	Issuer                  string   `json:"issuer"`
	ClientID                string   `json:"client_id"`
	TokenAuthMethod         string   `json:"token_endpoint_auth_method"`
	IDTokenAlgorithms       []string `json:"id_token_algorithms"`
	AssertionKeyID          string   `json:"assertion_key_id,omitempty"`
}
type Credentials struct {
	ClientSecret  string `json:"-"`
	PrivateKeyPEM []byte `json:"-"`
}
type Options struct {
	AllowLoopback bool
	AllowDemo     bool
}
type Adapter struct {
	config            oauth2.Config
	verifier          *oidc.IDTokenVerifier
	issuer            string
	client            *http.Client
	signer            jose.Signer
	assertionAudience string
}

// Transaction comes from the caller's one-use, browser-bound server-side store.
// The caller consumes state before Complete; this adapter checks nonce and PKCE.
type Transaction struct {
	ID       string
	State    string
	Nonce    string
	Verifier string
}
type VerifiedPrincipal struct {
	Issuer          string    `json:"issuer"`
	Subject         string    `json:"subject"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	Methods         []string  `json:"methods"`
	Assurance       string    `json:"assurance,omitempty"`
	TransactionID   string    `json:"transaction_id"`
}

func validTransaction(t Transaction) bool {
	return len(t.ID) >= 16 && len(t.ID) <= 256 && len(t.State) >= 26 && len(t.State) <= 256 && len(t.Nonce) >= 26 && len(t.Nonce) <= 256 && len(t.Verifier) >= 43 && len(t.Verifier) <= 128
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
func New(ctx context.Context, c Profile, credentials Credentials, callback string, options Options) (*Adapter, error) {
	loopback := options.AllowLoopback
	if c.JWKSCertificateMetadata != "" && c.JWKSCertificateMetadata != "ignore" {
		return nil, errors.New("unsupported JWKS certificate metadata policy")
	}
	if c.JWKSCertificateMetadata == "ignore" && !options.AllowDemo {
		return nil, errors.New("JWKS certificate metadata compatibility requires explicit demo permission")
	}
	if c.SchemaVersion != "gramstack.auth.oidc/v1" || len(c.IDTokenAlgorithms) == 0 {
		return nil, errors.New("versioned OIDC profile and explicit signing algorithms required")
	}
	for _, alg := range c.IDTokenAlgorithms {
		if alg != oidc.RS256 && alg != oidc.ES256 && alg != oidc.PS256 {
			return nil, errors.New("unsupported ID token algorithm")
		}
	}
	var signer jose.Signer
	switch c.TokenAuthMethod {
	case "client_secret_basic":
		if credentials.ClientSecret == "" || len(credentials.PrivateKeyPEM) != 0 || c.AssertionKeyID != "" || c.AssertionAudience != "" {
			return nil, errors.New("client secret required exclusively")
		}
	case "private_key_jwt":
		if c.AssertionAudience != "issuer" && c.AssertionAudience != "token_endpoint" {
			return nil, errors.New("explicit assertion audience selector required")
		}
		if credentials.ClientSecret != "" || c.AssertionKeyID == "" || len(c.AssertionKeyID) > 128 {
			return nil, errors.New("exclusive private key and key ID required")
		}
		block, rest := pem.Decode(credentials.PrivateKeyPEM)
		if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
			return nil, errors.New("PKCS8 private key required")
		}
		rawKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid assertion key")
		}
		key, ok := rawKey.(*rsa.PrivateKey)
		if !ok || key.N.BitLen() < 2048 || key.N.BitLen() > 8192 || key.Validate() != nil {
			return nil, errors.New("RSA assertion key of 2048–8192 bits required")
		}
		signer, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.PS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", c.AssertionKeyID))
		if err != nil {
			return nil, errors.New("invalid assertion signer")
		}
	default:
		return nil, errors.New("unsupported token endpoint authentication method")
	}

	if !trustedURL(c.Issuer, loopback) || !trustedURL(callback, loopback) || c.ClientID == "" {
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
	if metadata.Demo && !options.AllowDemo {
		return nil, errors.New("fictional identity provider requires explicit demo permission")
	}
	for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL, metadata.JWKS} {
		if !trustedURL(raw, loopback) {
			return nil, errors.New("untrusted OIDC protocol endpoint")
		}
	}
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	if signer != nil {
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	config := oauth2.Config{ClientID: c.ClientID, ClientSecret: credentials.ClientSecret, Endpoint: endpoint, RedirectURL: callback, Scopes: []string{oidc.ScopeOpenID}}
	verificationConfig := &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: c.IDTokenAlgorithms}
	verifier := discovered.Verifier(verificationConfig)
	if c.JWKSCertificateMetadata == "ignore" {
		keyClient := *client
		keyClient.Transport = publicJWKTransport{endpoint: metadata.JWKS, base: http.DefaultTransport}
		verifier = oidc.NewVerifier(c.Issuer, oidc.NewRemoteKeySet(oidc.ClientContext(ctx, &keyClient), metadata.JWKS), verificationConfig)
	}
	audience := endpoint.TokenURL
	if c.AssertionAudience == "issuer" {
		audience = c.Issuer
	}
	return &Adapter{config, verifier, c.Issuer, client, signer, audience}, nil
}
func (p *Adapter) Begin(t Transaction) (string, error) {
	if !validTransaction(t) {
		return "", ErrAuthentication
	}
	return p.config.AuthCodeURL(t.State, oidc.Nonce(t.Nonce), oauth2.S256ChallengeOption(t.Verifier), oauth2.SetAuthURLParam("prompt", "login"), oauth2.SetAuthURLParam("max_age", "0")), nil
}
func (p *Adapter) Complete(ctx context.Context, code string, t Transaction) (*VerifiedPrincipal, error) {
	if !validTransaction(t) || code == "" || len(code) > 8192 {
		return nil, ErrAuthentication
	}
	nonce, verifier := t.Nonce, t.Verifier
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	exchange := []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)}
	if p.signer != nil {
		now := time.Now()
		claims := map[string]any{"iss": p.config.ClientID, "sub": p.config.ClientID, "aud": p.assertionAudience, "iat": now.Unix(), "exp": now.Add(time.Minute).Unix(), "jti": rand.Text()}
		raw, err := json.Marshal(claims)
		if err != nil {
			return nil, ErrAuthentication
		}
		signed, err := p.signer.Sign(raw)
		if err != nil {
			return nil, ErrAuthentication
		}
		assertion, err := signed.CompactSerialize()
		if err != nil {
			return nil, ErrAuthentication
		}
		exchange = append(exchange, oauth2.SetAuthURLParam("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"), oauth2.SetAuthURLParam("client_assertion", assertion))
	}
	token, err := p.config.Exchange(ctx, code, exchange...)
	if err != nil {
		return nil, ErrAuthentication
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 32768 {
		return nil, ErrAuthentication
	}
	id, err := p.verifier.Verify(ctx, raw)
	if err != nil || len(id.Audience) != 1 || id.Audience[0] != p.config.ClientID || id.Subject == "" || id.IssuedAt.Unix() <= 0 || len(id.Subject) > 1024 || id.Issuer != p.issuer || id.IssuedAt.After(time.Now().Add(30*time.Second)) || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 || nonce == "" {
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
	var authentication struct {
		AuthTime  int64    `json:"auth_time"`
		Methods   []string `json:"amr"`
		Assurance string   `json:"acr"`
	}
	if id.Claims(&authentication) != nil || authentication.AuthTime <= 0 || authentication.AuthTime > time.Now().Add(30*time.Second).Unix() || time.Since(time.Unix(authentication.AuthTime, 0)) > 5*time.Minute || len(authentication.Methods) > 16 || len(authentication.Assurance) > 256 {
		return nil, ErrAuthentication
	}
	for _, method := range authentication.Methods {
		if method == "" || len(method) > 128 {
			return nil, ErrAuthentication
		}
	}
	return &VerifiedPrincipal{Issuer: id.Issuer, Subject: id.Subject, AuthenticatedAt: time.Unix(authentication.AuthTime, 0).UTC(), ExpiresAt: id.Expiry, Methods: authentication.Methods, Assurance: authentication.Assurance, TransactionID: t.ID}, nil
}
