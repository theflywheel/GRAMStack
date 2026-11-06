package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	oidcadapter "github.com/theflywheel/GRAMStack/adapters/auth/oidc"
	"golang.org/x/oauth2"
)

// applicationFlow authenticates an enrolled application's user independently of any access request.
type applicationFlow struct {
	ClientID    string
	State       string
	Challenge   string
	Transaction oidcadapter.Transaction
	StartedAt   time.Time
}
type applicationIdentity struct {
	ClientID        string
	Subject         string
	AuthenticatedAt int64
}

func (s *Service) applicationRequest(request hydraRequest) (string, bool) {
	redirects, ok := s.applicationClients[request.Client.ID]
	if !ok || !sameOrigin(request.RequestURL, s.hydra.issuer) {
		return "", false
	}
	u, err := url.Parse(request.RequestURL)
	if err != nil {
		return "", false
	}
	q := u.Query()
	for _, key := range []string{"client_id", "redirect_uri", "state", "nonce", "response_type", "scope", "code_challenge", "code_challenge_method"} {
		if len(q[key]) != 1 || q.Get(key) == "" {
			return "", false
		}
	}
	state := q.Get("state")
	if q.Get("client_id") != request.Client.ID || !slices.Contains(redirects, q.Get("redirect_uri")) || q.Get("response_type") != "code" || q.Get("scope") != "openid" || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || len(q.Get("nonce")) < 26 || len(q.Get("nonce")) > 256 || len(state) < 26 || len(state) > 256 {
		return "", false
	}
	return state, true
}
func (s *Service) applicationLogin(w http.ResponseWriter, r *http.Request, challenge string, request hydraRequest) {
	state, ok := s.applicationRequest(request)
	if !ok {
		fail(w)
		return
	}
	now := time.Now().UTC()
	binding, upstreamState := rand.Text(), rand.Text()
	value := applicationFlow{
		ClientID: request.Client.ID, State: state, Challenge: challenge, StartedAt: now,
		Transaction: oidcadapter.Transaction{ID: upstreamState, State: upstreamState, Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier()},
	}
	redirect, err := s.upstream.Begin(value.Transaction)
	if err != nil || s.store.Put(r.Context(), "application.upstream", upstreamState, binding, value, now.Add(5*time.Minute)) != nil {
		fail(w)
		return
	}
	s.setCookie(w, "application-flow", binding, now.Add(5*time.Minute))
	http.Redirect(w, r, redirect, http.StatusFound)
}
func (s *Service) completeApplication(w http.ResponseWriter, r *http.Request, state, code string) bool {
	binding := s.historyCookie(r, "application-flow")
	if binding == "" {
		return false
	}
	var value applicationFlow
	if s.store.Take(r.Context(), "application.upstream", state, binding, &value) != nil {
		return false
	}
	token, err := s.upstream.Complete(r.Context(), code, value.Transaction)
	if err != nil || token.AuthenticatedAt.Before(value.StartedAt.Truncate(time.Second)) {
		fail(w)
		return true
	}
	if _, ok := s.applicationClients[value.ClientID]; !ok {
		fail(w)
		return true
	}
	identity := applicationIdentity{ClientID: value.ClientID, Subject: s.applicationSubject(value.ClientID, token.Issuer, token.Subject), AuthenticatedAt: token.AuthenticatedAt.Unix()}
	until := time.Now().Add(5 * time.Minute)
	if token.ExpiresAt.Before(until) {
		until = token.ExpiresAt
	}
	if s.store.Put(r.Context(), "application.consent", value.State, binding, identity, until) != nil {
		fail(w)
		return true
	}
	var accepted struct {
		Redirect string `json:"redirect_to"`
	}
	if s.hydraJSON(r.Context(), "PUT", "/admin/oauth2/auth/requests/login/accept?login_challenge="+url.QueryEscape(value.Challenge), map[string]any{"subject": identity.Subject, "remember": false}, &accepted) != nil || !sameOrigin(accepted.Redirect, s.hydra.issuer) {
		fail(w)
		return true
	}
	http.Redirect(w, r, accepted.Redirect, http.StatusFound)
	return true
}
func (s *Service) applicationConsent(w http.ResponseWriter, r *http.Request, challenge string, request hydraRequest) {
	state, ok := s.applicationRequest(request)
	binding := s.historyCookie(r, "application-flow")
	if !ok || binding == "" || len(request.RequestedScope) != 1 || request.RequestedScope[0] != "openid" {
		fail(w)
		return
	}
	var identity applicationIdentity
	if s.store.Take(r.Context(), "application.consent", state, binding, &identity) != nil || identity.ClientID != request.Client.ID || identity.Subject != request.Subject || time.Now().Unix()-identity.AuthenticatedAt > 300 || identity.AuthenticatedAt > time.Now().Add(30*time.Second).Unix() {
		fail(w)
		return
	}
	var accepted struct {
		Redirect string `json:"redirect_to"`
	}
	input := map[string]any{
		"grant_scope": []string{"openid"}, "grant_access_token_audience": []string{}, "remember": false,
		"session": map[string]any{"id_token": map[string]any{"auth_time": identity.AuthenticatedAt}},
	}
	if s.hydraJSON(r.Context(), "PUT", "/admin/oauth2/auth/requests/consent/accept?consent_challenge="+url.QueryEscape(challenge), input, &accepted) != nil || !sameOrigin(accepted.Redirect, s.hydra.issuer) {
		fail(w)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName("application-flow"), Path: "/", Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, accepted.Redirect, http.StatusFound)
}

func (s *Service) applicationSubject(client, issuer, subject string) string {
	mac := hmac.New(sha256.New, s.applicationSubjectKey)
	mac.Write([]byte(strings.Join([]string{"gramstack.application.subject.v1", client, issuer, subject}, "\x00")))
	return "app_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
