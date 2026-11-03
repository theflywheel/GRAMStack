// Package consumer exposes institutional request APIs, never registry plaintext.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

var (
	ErrUnauthenticated = errors.New("invalid consumer credential")
	ErrUnavailable     = errors.New("mandatory dependency unavailable")
)

type Authenticator interface {
	Authenticate(context.Context, string) (string, error)
}

type Hydra struct {
	endpoint, issuer, audience, scope string
	client                            *http.Client
}

// NewHydra accepts only a deployment-pinned endpoint. HTTP is limited to an
// explicitly enabled literal loopback development server. Never proxy admin APIs.
func NewHydra(endpoint, issuer, audience, scope string, allowLoopback bool) (*Hydra, error) {
	if !trustedURL(endpoint, allowLoopback) || !trustedURL(issuer, allowLoopback) || audience == "" || scope == "" {
		return nil, errors.New("trusted Hydra endpoint, issuer, audience and scope required")
	}
	return &Hydra{endpoint, issuer, audience, scope, &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func trustedURL(raw string, allowLoopback bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return allowLoopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()
}
func (h *Hydra) Authenticate(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > 8192 || strings.ContainsAny(token, " \t\r\n") {
		return "", ErrUnauthenticated
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}, "scope": {h.scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := h.client.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return "", ErrUnavailable
	}
	var result struct {
		Active    bool     `json:"active"`
		ClientID  string   `json:"client_id"`
		Issuer    string   `json:"iss"`
		Audience  []string `json:"aud"`
		Scope     string   `json:"scope"`
		Exp       int64    `json:"exp"`
		Nbf       int64    `json:"nbf"`
		Iat       int64    `json:"iat"`
		TokenUse  string   `json:"token_use"`
		TokenType string   `json:"token_type"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", ErrUnavailable
	}
	now := time.Now().Unix()
	if !result.Active || result.ClientID == "" || result.Issuer != h.issuer || !slices.Contains(result.Audience, h.audience) || !slices.Contains(strings.Fields(result.Scope), h.scope) || result.Exp <= now || result.Nbf > now || result.Iat > now || result.TokenUse != "access_token" || result.TokenType != "Bearer" {
		return "", ErrUnauthenticated
	}
	return result.ClientID, nil
}
