// Package authn owns first-party authentication transactions and federation.
// Hydra remains the OAuth/OIDC protocol engine; citizen credentials stay upstream.
package authn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/access"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"golang.org/x/oauth2"
)

type Config struct {
	Pool               *pgxpool.Pool
	Store              *Store
	Origin, HydraAdmin string
	Hydra, Upstream    ProviderConfig
	AllowLoopback      bool
}
type Service struct {
	pool            *pgxpool.Pool
	store           *Store
	origin, admin   string
	hydra, upstream *provider
	client          *http.Client
	secure          bool
}
type flow struct {
	RequestID, Digest, State, Nonce, Verifier, PrincipalRef string
	ExpiresAt                                               time.Time
}
type upstreamFlow struct {
	Outer                      flow
	Challenge, Nonce, Verifier string
}
type Session struct {
	RequestID, PrincipalRef, CSRF string
	AuthenticatedAt, ExpiresAt    time.Time
}
type request struct {
	Snapshot cm.Snapshot
	State    string
	Version  int64
}

func New(ctx context.Context, c Config) (*Service, error) {
	if c.Pool == nil || c.Store == nil || !trustedURL(c.Origin, c.AllowLoopback) || !trustedURL(c.HydraAdmin, c.AllowLoopback) {
		return nil, ErrAuthentication
	}
	origin, err := cleanOrigin(c.Origin)
	if err != nil {
		return nil, err
	}
	hydra, err := newProvider(ctx, c.Hydra, origin+"/auth/callback", c.AllowLoopback)
	if err != nil {
		return nil, err
	}
	upstream, err := newProvider(ctx, c.Upstream, origin+"/auth/provider/callback", c.AllowLoopback)
	if err != nil {
		return nil, err
	}
	return &Service{c.Pool, c.Store, origin, strings.TrimSuffix(c.HydraAdmin, "/"), hydra, upstream, &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, strings.HasPrefix(origin, "https://")}, nil
}
func (s *Service) loadRequest(ctx context.Context, id string) (request, error) {
	var result request
	var raw []byte
	var digest string
	var expires, now time.Time
	err := s.pool.QueryRow(ctx, `SELECT a.snapshot_bytes,r.request_digest,r.status,r.version,r.expires_at,clock_timestamp() FROM accepted_requests a JOIN access_requests r USING(request_id) WHERE r.request_id=$1`, id).Scan(&raw, &digest, &result.State, &result.Version, &expires, &now)
	if err != nil {
		return request{}, ErrAuthentication
	}
	if json.Unmarshal(raw, &result.Snapshot) != nil || !now.Before(expires) {
		return request{}, ErrAuthentication
	}
	actual, err := cm.SnapshotDigest(result.Snapshot)
	if err != nil || actual != digest {
		return request{}, ErrAuthentication
	}
	switch result.State {
	case "AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "WITHDRAWN", "REJECTED", "CANCELLED", "COMPLETED", "DELIVERY_UNKNOWN":
		return result, nil
	default:
		return request{}, ErrAuthentication
	}
}
func (s *Service) cookieName(kind string) string {
	if s.secure {
		return "__Host-gramstack-" + kind
	}
	return "gramstack-" + kind + "-dev"
}
func (s *Service) setCookie(w http.ResponseWriter, kind, value string, until time.Time) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(kind), Value: value, Path: "/", Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: until, MaxAge: int(time.Until(until).Seconds())})
}
func (s *Service) binding(r *http.Request) (string, error) {
	values := r.CookiesNamed(s.cookieName("flow"))
	if len(values) != 1 || len(values[0].Value) < 26 || len(values[0].Value) > 128 {
		return "", ErrAuthentication
	}
	return values[0].Value, nil
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /interactions/{id}", s.interaction)
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/provider/callback", s.upstreamCallback)
	mux.HandleFunc("GET /auth/oauth-consent", s.oauthConsent)
	mux.HandleFunc("GET /auth/callback", s.callback)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}
func fail(w http.ResponseWriter) {
	http.Error(w, "Authentication could not be completed. Restart from the requesting application.", http.StatusForbidden)
}
func (s *Service) interaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	value, err := s.loadRequest(r.Context(), id)
	if err != nil {
		fail(w)
		return
	}
	if session, err := s.Session(r, id); err == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Authenticated interaction. Registry verification and consent are separate steps.\nPrincipal reference: " + session.PrincipalRef + "\n"))
		return
	}
	if value.State == "CANCELLED" {
		fail(w)
		return
	}
	binding, state := rand.Text(), rand.Text()
	deadline := value.Snapshot.ExpiresAt
	if time.Until(deadline) > 5*time.Minute {
		deadline = time.Now().Add(5 * time.Minute)
	}
	flow := flow{RequestID: id, Digest: mustDigest(value.Snapshot), State: state, Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier(), ExpiresAt: deadline}
	if s.store.Put(r.Context(), "auth.flow", state, binding, flow, deadline) != nil {
		fail(w)
		return
	}
	s.setCookie(w, "flow", binding, deadline)
	http.Redirect(w, r, s.hydra.begin(state, flow.Nonce, flow.Verifier), http.StatusFound)
}
func mustDigest(snapshot cm.Snapshot) string { digest, _ := cm.SnapshotDigest(snapshot); return digest }

// Hydra admin responses are resolved through a configured origin and never proxied.
func (s *Service) hydraJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.admin+path, body)
	if err != nil {
		return ErrAuthentication
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return ErrAuthentication
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrAuthentication
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, output) != nil {
		return ErrAuthentication
	}
	return nil
}

type hydraRequest struct {
	Subject        string   `json:"subject"`
	RequestURL     string   `json:"request_url"`
	RequestedScope []string `json:"requested_scope"`
	Client         struct {
		ID string `json:"client_id"`
	} `json:"client"`
}

func (s *Service) outerFlow(r *http.Request, hydra hydraRequest) (flow, string, error) {
	binding, err := s.binding(r)
	if err != nil {
		return flow{}, "", err
	}
	u, err := url.Parse(hydra.RequestURL)
	if err != nil || hydra.Client.ID != s.hydra.config.ClientID || !sameOrigin(hydra.RequestURL, s.hydra.issuer) || u.Query().Get("client_id") != s.hydra.config.ClientID || u.Query().Get("redirect_uri") != s.hydra.config.RedirectURL {
		return flow{}, "", ErrAuthentication
	}
	if len(u.Query()["state"]) != 1 {
		return flow{}, "", ErrAuthentication
	}
	state := u.Query().Get("state")
	var value flow
	if s.store.Peek(r.Context(), "auth.flow", state, binding, &value) != nil || value.State != state {
		return flow{}, "", ErrAuthentication
	}
	if u.Query().Get("nonce") != value.Nonce || u.Query().Get("code_challenge") != oauth2.S256ChallengeFromVerifier(value.Verifier) || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("scope") != "openid" || u.Query().Get("response_type") != "code" {
		return flow{}, "", ErrAuthentication
	}
	return value, binding, nil
}
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("login_challenge")
	if challenge == "" || len(challenge) > 8192 {
		fail(w)
		return
	}
	var request hydraRequest
	if s.hydraJSON(r.Context(), "GET", "/admin/oauth2/auth/requests/login?login_challenge="+url.QueryEscape(challenge), nil, &request) != nil {
		fail(w)
		return
	}
	outer, binding, err := s.outerFlow(r, request)
	if err != nil {
		fail(w)
		return
	}
	state := rand.Text()
	inner := upstreamFlow{Outer: outer, Challenge: challenge, Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier()}
	if s.store.Put(r.Context(), "auth.upstream", state, binding, inner, outer.ExpiresAt) != nil {
		fail(w)
		return
	}
	http.Redirect(w, r, s.upstream.begin(state, inner.Nonce, inner.Verifier), http.StatusFound)
}
func callbackParameters(r *http.Request) (string, string, error) {
	q := r.URL.Query()
	if len(q["state"]) != 1 || len(q["code"]) != 1 || len(q.Get("state")) != 26 || len(q.Get("code")) < 1 || len(q.Get("code")) > 8192 || q.Get("error") != "" {
		return "", "", ErrAuthentication
	}
	return q.Get("state"), q.Get("code"), nil
}

func (s *Service) upstreamCallback(w http.ResponseWriter, r *http.Request) {
	state, code, parseErr := callbackParameters(r)
	if parseErr != nil {
		fail(w)
		return
	}
	binding, err := s.binding(r)
	if err != nil {
		fail(w)
		return
	}
	var inner upstreamFlow
	if s.store.Take(r.Context(), "auth.upstream", state, binding, &inner) != nil {
		fail(w)
		return
	}
	token, err := s.upstream.complete(r.Context(), code, inner.Nonce, inner.Verifier)
	if err != nil {
		fail(w)
		return
	}
	var claims struct {
		AuthTime int64    `json:"auth_time"`
		Methods  []string `json:"amr"`
	}
	if token.Claims(&claims) != nil || claims.AuthTime <= 0 || claims.AuthTime > time.Now().Add(30*time.Second).Unix() || time.Since(time.Unix(claims.AuthTime, 0)) > 5*time.Minute || len(claims.Methods) > 16 {
		fail(w)
		return
	}
	for _, method := range claims.Methods {
		if len(method) > 128 {
			fail(w)
			return
		}
	}
	principal := Principal{Reference: rand.Text(), Issuer: token.Issuer, Subject: token.Subject, AuthenticationTransactionID: inner.Outer.State, ExpiresAt: inner.Outer.ExpiresAt, AuthenticatedAt: time.Unix(claims.AuthTime, 0).UTC(), Methods: claims.Methods}
	// Request-scoped reauthentication reuses an opaque reference only after the
	// same issuer/subject is freshly verified. It creates no cross-registry index.
	var previous Principal
	err = s.store.Peek(r.Context(), "auth.principal", inner.Outer.RequestID, "server", &previous)
	if err == nil {
		if previous.Issuer != principal.Issuer || previous.Subject != principal.Subject {
			fail(w)
			return
		}
		principal.Reference = previous.Reference
	} else if !errors.Is(err, ErrAuthentication) {
		fail(w)
		return
	}
	request, err := s.loadRequest(r.Context(), inner.Outer.RequestID)
	if err != nil || mustDigest(request.Snapshot) != inner.Outer.Digest || request.State == "CANCELLED" {
		fail(w)
		return
	}
	until := request.Snapshot.ExpiresAt
	if previous.Reference == "" {
		err = s.store.Put(r.Context(), "auth.principal", inner.Outer.RequestID, "server", principal, until)
	} else {
		err = s.store.Replace(r.Context(), "auth.principal", inner.Outer.RequestID, "server", principal, until)
	}
	if err != nil {
		fail(w)
		return
	}
	inner.Outer.PrincipalRef = principal.Reference
	if s.store.Replace(r.Context(), "auth.flow", inner.Outer.State, binding, inner.Outer, inner.Outer.ExpiresAt) != nil {
		fail(w)
		return
	}
	var accepted struct {
		Redirect string `json:"redirect_to"`
	}
	if s.hydraJSON(r.Context(), "PUT", "/admin/oauth2/auth/requests/login/accept?login_challenge="+url.QueryEscape(inner.Challenge), map[string]any{"subject": principal.Reference, "remember": false}, &accepted) != nil || !sameOrigin(accepted.Redirect, s.hydra.issuer) {
		fail(w)
		return
	}
	http.Redirect(w, r, accepted.Redirect, http.StatusFound)
}
func (s *Service) oauthConsent(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("consent_challenge")
	if challenge == "" || len(challenge) > 8192 {
		fail(w)
		return
	}
	var request hydraRequest
	if s.hydraJSON(r.Context(), "GET", "/admin/oauth2/auth/requests/consent?consent_challenge="+url.QueryEscape(challenge), nil, &request) != nil {
		fail(w)
		return
	}
	outer, _, err := s.outerFlow(r, request)
	if err != nil || outer.PrincipalRef == "" || request.Subject != outer.PrincipalRef || len(request.RequestedScope) != 1 || request.RequestedScope[0] != "openid" {
		fail(w)
		return
	}
	// OAuth consent is only for this first-party login client. It never approves
	// registry disclosure, data scopes, offline access or recipient retention.
	var accepted struct {
		Redirect string `json:"redirect_to"`
	}
	if s.hydraJSON(r.Context(), "PUT", "/admin/oauth2/auth/requests/consent/accept?consent_challenge="+url.QueryEscape(challenge), map[string]any{"grant_scope": []string{"openid"}, "grant_access_token_audience": []string{}, "remember": false}, &accepted) != nil || !sameOrigin(accepted.Redirect, s.hydra.issuer) {
		fail(w)
		return
	}
	http.Redirect(w, r, accepted.Redirect, http.StatusFound)
}
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	state, code, parseErr := callbackParameters(r)
	if parseErr != nil {
		fail(w)
		return
	}
	binding, err := s.binding(r)
	if err != nil {
		fail(w)
		return
	}
	var outer flow
	if s.store.Take(r.Context(), "auth.flow", state, binding, &outer) != nil || outer.PrincipalRef == "" {
		fail(w)
		return
	}
	token, err := s.hydra.complete(r.Context(), code, outer.Nonce, outer.Verifier)
	if err != nil || token.Subject != outer.PrincipalRef {
		fail(w)
		return
	}
	value, err := s.loadRequest(r.Context(), outer.RequestID)
	if err != nil || mustDigest(value.Snapshot) != outer.Digest || value.State == "CANCELLED" {
		fail(w)
		return
	}
	if value.State == "AUTHENTICATION_PENDING" {
		if _, err = (access.Repository{Pool: s.pool}).Advance(r.Context(), outer.RequestID, value.Version, "authenticated"); err != nil {
			fail(w)
			return
		}
	}
	var principal Principal
	if s.store.Peek(r.Context(), "auth.principal", outer.RequestID, "server", &principal) != nil || principal.Reference != token.Subject {
		fail(w)
		return
	}
	until := value.Snapshot.ExpiresAt
	if time.Until(until) > 10*time.Minute {
		until = time.Now().Add(10 * time.Minute)
	}
	sessionID := rand.Text()
	session := Session{outer.RequestID, principal.Reference, rand.Text(), principal.AuthenticatedAt, until}
	if s.store.Put(r.Context(), "auth.session", sessionID, sessionID, session, until) != nil {
		fail(w)
		return
	}
	if old, err := r.Cookie(s.cookieName("session")); err == nil {
		var prior Session
		_ = s.store.Take(r.Context(), "auth.session", old.Value, old.Value, &prior)
	}
	s.setCookie(w, "session", sessionID, until)
	http.SetCookie(w, &http.Cookie{Name: s.cookieName("flow"), Value: "", Path: "/", Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, s.origin+"/interactions/"+url.PathEscape(outer.RequestID), http.StatusSeeOther)
}
func (s *Service) Session(r *http.Request, requestID string) (Session, error) {
	cookies := r.CookiesNamed(s.cookieName("session"))
	if len(cookies) != 1 || len(cookies[0].Value) < 26 || len(cookies[0].Value) > 128 {
		return Session{}, ErrAuthentication
	}
	var session Session
	if s.store.Peek(r.Context(), "auth.session", cookies[0].Value, cookies[0].Value, &session) != nil || session.RequestID != requestID {
		return Session{}, ErrAuthentication
	}
	return session, nil
}
func (s *Service) VerifyCSRF(r *http.Request, session Session, token string) error {
	if r.Header.Get("Origin") != s.origin || token == "" || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(token)) != 1 {
		return ErrAuthentication
	}
	return nil
}
func (s *Service) Principal(ctx context.Context, requestID, principalRef string) (Principal, error) {
	var principal Principal
	err := s.store.Peek(ctx, "auth.principal", requestID, "server", &principal)
	if err != nil || principal.Reference != principalRef {
		return Principal{}, ErrAuthentication
	}
	return principal, nil
}
