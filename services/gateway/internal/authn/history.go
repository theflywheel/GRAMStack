package authn

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	oidcadapter "github.com/theflywheel/GRAMStack/adapters/auth/oidc"
	"golang.org/x/oauth2"
)

func (s *Store) historyDigest(client, registry string, principal Principal) string {
	mac := hmac.New(sha256.New, s.historyKey)
	fmt.Fprintf(mac, "%q:%q:%q:%q", client, registry, principal.Issuer, principal.Subject)
	return "sha256:" + hex.EncodeToString(mac.Sum(nil))
}

// bindHistory keeps ownership stable even after encrypted authentication
// transactions and sessions expire. Only opaque, scoped values survive.
func (s *Service) bindHistory(ctx context.Context, tx pgx.Tx, requestID, registry string, principal Principal) error {
	owner := s.store.historyDigest(s.historyClientID, registry, principal)
	if _, err := tx.Exec(ctx, `INSERT INTO consent_history_owners(request_id,owner_digest,principal_ref)
		VALUES($1,$2,$3) ON CONFLICT(request_id) DO NOTHING`, requestID, owner, principal.Reference); err != nil {
		return err
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT owner_digest=$2 AND principal_ref=$3 FROM consent_history_owners WHERE request_id=$1`,
		requestID, owner, principal.Reference).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return ErrAuthentication
	}
	return nil
}

type historyFlow struct {
	Transaction oidcadapter.Transaction
	Registry    string
	StartedAt   time.Time
}

type HistorySession struct {
	OwnerDigest string
	Registry    string
	CSRF        string
}

var historyRegistry = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func (s *Service) startHistory(w http.ResponseWriter, r *http.Request) {
	registry := r.PathValue("registry")
	if !s.consentPortalEnabled || !historyRegistry.MatchString(registry) {
		http.NotFound(w, r)
		return
	}
	now := time.Now().UTC()
	state, binding := rand.Text(), rand.Text()
	flow := historyFlow{
		Transaction: oidcadapter.Transaction{ID: state, State: state, Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier()},
		Registry:    registry, StartedAt: now,
	}
	redirect, err := s.upstream.Begin(flow.Transaction)
	deadline := now.Add(5 * time.Minute)
	if err != nil || s.store.Put(r.Context(), "history.flow", state, binding, flow, deadline) != nil {
		fail(w)
		return
	}
	s.setCookie(w, "history-flow", binding, deadline)
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Service) completeHistory(w http.ResponseWriter, r *http.Request, state, code string) bool {
	if !s.consentPortalEnabled {
		return false
	}
	binding := s.historyCookie(r, "history-flow")
	if binding == "" {
		return false
	}
	var flow historyFlow
	if s.store.Take(r.Context(), "history.flow", state, binding, &flow) != nil {
		return false
	}
	token, err := s.upstream.Complete(r.Context(), code, flow.Transaction)
	if err != nil || token.AuthenticatedAt.Before(flow.StartedAt.Truncate(time.Second)) {
		fail(w)
		return true
	}
	until := time.Now().Add(5 * time.Minute)
	if token.ExpiresAt.Before(until) {
		until = token.ExpiresAt
	}
	sessionID := rand.Text()
	session := HistorySession{
		OwnerDigest: s.store.historyDigest(s.historyClientID, flow.Registry, Principal{Issuer: token.Issuer, Subject: token.Subject}),
		Registry:    flow.Registry,
		CSRF:        rand.Text(),
	}
	if s.store.Put(r.Context(), "history.session", sessionID, sessionID, session, until) != nil {
		fail(w)
		return true
	}
	if prior := s.historyCookie(r, "history"); prior != "" {
		var previous HistorySession
		_ = s.store.Take(r.Context(), "history.session", prior, prior, &previous)
	}
	s.setCookie(w, "history", sessionID, until)
	http.SetCookie(w, &http.Cookie{Name: s.cookieName("history-flow"), Path: "/", Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, s.origin+"/consent-history/"+url.PathEscape(flow.Registry), http.StatusSeeOther)
	return true
}

func (s *Service) historyCookie(r *http.Request, kind string) string {
	cookies := r.CookiesNamed(s.cookieName(kind))
	if len(cookies) != 1 || len(cookies[0].Value) != 26 {
		return ""
	}
	return cookies[0].Value
}

// HistoryAccess authorizes owner operations for exactly one registry.
func (s *Service) HistoryAccess(r *http.Request, registry string) (HistorySession, error) {
	id := s.historyCookie(r, "history")
	var session HistorySession
	if !s.consentPortalEnabled || id == "" ||
		s.store.Peek(r.Context(), "history.session", id, id, &session) != nil ||
		session.Registry != registry || session.OwnerDigest == "" {
		return HistorySession{}, ErrAuthentication
	}
	return session, nil
}
