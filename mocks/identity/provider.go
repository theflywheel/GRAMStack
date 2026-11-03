// Package identity is a fictional OIDC test provider. It does not authenticate
// real citizens and must never be represented as a production identity authority.
package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURI string
	FormReturnOrigins                           []string
	DemoEnabled                                 bool
}
type authRequest struct {
	State, Nonce, Challenge, CSRF string
	Expires                       time.Time
}
type code struct {
	Request authRequest
	Subject string
	Expires time.Time
}
type Provider struct {
	config   Config
	key      *rsa.PrivateKey
	mu       sync.Mutex
	requests map[string]authRequest
	codes    map[string]code
}

func New(c Config) (*Provider, error) {
	if !c.DemoEnabled || c.ClientID == "" || len(c.ClientSecret) < 16 {
		return nil, errors.New("explicit demo mode and synthetic client credentials required")
	}
	for _, raw := range []string{c.Issuer, c.RedirectURI} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid demo endpoints")
		}
	}
	for _, raw := range c.FormReturnOrigins {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(raw, "; \t\r\n'\"*") {
			return nil, errors.New("explicit form return origins must be exact HTTP(S) origins")
		}
	}
	c.FormReturnOrigins = append([]string(nil), c.FormReturnOrigins...)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Provider{c, key, sync.Mutex{}, map[string]authRequest{}, map[string]code{}}, nil
}
func (p *Provider) Handler() http.Handler {
	callback, _ := url.Parse(p.config.RedirectURI)
	// Chrome applies form-action to the registered OAuth return redirect too.
	formTargets := "'self' " + callback.Scheme + "://" + callback.Host
	for _, origin := range p.config.FormReturnOrigins {
		formTargets += " " + origin
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /authorize", p.choose)
	mux.HandleFunc("POST /token", p.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action "+formTargets+"; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]any{"gramstack_demo": true, "issuer": p.config.Issuer, "authorization_endpoint": p.config.Issuer + "/authorize", "token_endpoint": p.config.Issuer + "/token", "jwks_uri": p.config.Issuer + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "code_challenge_methods_supported": []string{"S256"}})
}
func (p *Provider) jwks(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "demo-rsa-1", Algorithm: "RS256", Use: "sig"}}})
}

var screen = template.Must(template.New("identity").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Demo identity provider</title></head><body><main><p>GRAMStack · fictional identity provider</p><h1>Choose a demo identity</h1><p>This service uses fictional users. It does not perform Aadhaar, eSignet or government identity verification.</p><form method="post" action="/authorize"><input type="hidden" name="ticket" value="{{.Ticket}}"><input type="hidden" name="csrf" value="{{.CSRF}}"><button name="identity" value="sunita">Continue as Sunita Devi</button><button name="identity" value="rani">Continue as Rani — different member</button></form></main></body></html>`))

func (p *Provider) prune() {
	now := time.Now()
	for key, item := range p.requests {
		if !now.Before(item.Expires) {
			delete(p.requests, key)
		}
	}
	for key, item := range p.codes {
		if !now.Before(item.Expires) {
			delete(p.codes, key)
		}
	}
}
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != p.config.ClientID || q.Get("redirect_uri") != p.config.RedirectURI || q.Get("response_type") != "code" || q.Get("scope") != "openid" || q.Get("state") == "" || q.Get("nonce") == "" || len(q.Get("code_challenge")) != 43 || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "Invalid demo authorisation request", 400)
		return
	}
	for _, key := range []string{"client_id", "redirect_uri", "response_type", "scope", "state", "nonce", "code_challenge", "code_challenge_method"} {
		if len(q[key]) != 1 || len(q.Get(key)) > 2048 {
			http.Error(w, "Invalid demo authorisation request", 400)
			return
		}
	}
	ticket, csrf := rand.Text(), rand.Text()
	request := authRequest{q.Get("state"), q.Get("nonce"), q.Get("code_challenge"), csrf, time.Now().Add(2 * time.Minute)}
	p.mu.Lock()
	p.prune()
	if len(p.requests)+len(p.codes) >= 1000 {
		p.mu.Unlock()
		http.Error(w, "Demo capacity reached", 503)
		return
	}
	p.requests[ticket] = request
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "demo_idp_csrf", Value: csrf, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(p.config.Issuer, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 120})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = screen.Execute(w, struct{ Ticket, CSRF string }{ticket, csrf})
}
func (p *Provider) choose(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid demo selection", 400)
		return
	}
	p.mu.Lock()
	request, ok := p.requests[r.PostForm.Get("ticket")]
	cookie, err := r.Cookie("demo_idp_csrf")
	if !ok || !time.Now().Before(request.Expires) || err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(request.CSRF)) != 1 || r.PostForm.Get("csrf") != request.CSRF || (r.PostForm.Get("identity") != "sunita" && r.PostForm.Get("identity") != "rani") {
		p.mu.Unlock()
		http.Error(w, "Invalid demo selection", 403)
		return
	}
	delete(p.requests, r.PostForm.Get("ticket"))
	value := rand.Text()
	p.codes[value] = code{request, "demo-" + r.PostForm.Get("identity"), time.Now().Add(time.Minute)}
	p.mu.Unlock()
	target, _ := url.Parse(p.config.RedirectURI)
	q := target.Query()
	q.Set("code", value)
	q.Set("state", request.State)
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	client, secret, ok := r.BasicAuth()
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if !ok || client != p.config.ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(p.config.ClientSecret)) != 1 || r.ParseForm() != nil || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != p.config.RedirectURI {
		jsonResponse(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	stored, found := p.codes[r.PostForm.Get("code")]
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !found || !time.Now().Before(stored.Expires) || subtle.ConstantTimeCompare([]byte(challenge), []byte(stored.Request.Challenge)) != 1 {
		p.mu.Unlock()
		jsonResponse(w, 400, map[string]string{"error": "invalid_grant"})
		return
	}
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	now := time.Now().Unix()
	claims := map[string]any{"iss": p.config.Issuer, "sub": stored.Subject, "aud": p.config.ClientID, "iat": now, "nbf": now, "exp": now + 180, "auth_time": now, "nonce": stored.Request.Nonce, "amr": []string{"demo-selection"}, "demo_identity": true}
	raw, _ := json.Marshal(claims)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "demo-rsa-1"))
	if err != nil {
		http.Error(w, "Signing unavailable", 503)
		return
	}
	signed, err := signer.Sign(raw)
	if err != nil {
		http.Error(w, "Signing unavailable", 503)
		return
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		http.Error(w, "Signing unavailable", 503)
		return
	}
	jsonResponse(w, 200, map[string]any{"access_token": rand.Text(), "token_type": "Bearer", "expires_in": 180, "id_token": compact})
}
