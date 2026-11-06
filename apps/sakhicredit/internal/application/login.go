package application

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	gramstack "github.com/theflywheel/GRAMStack/sdk/go/client"
)

// SaveLogin and TakeLogin implement the SDK's durable, encrypted one-use login store.
func (s *Store) SaveLogin(ctx context.Context, state, binding string, value gramstack.LoginTransaction) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return ErrInput
	}
	stateHash, bindingHash := ownerOf(state), ownerOf(binding)
	_, err = s.pool.Exec(ctx, `INSERT INTO sakhi_login_flows(state_hash,binding_hash,payload,expires_at) VALUES($1,$2,$3,$4)`, stateHash, bindingHash, s.seal(stateHash, bindingHash, "login-flow", raw), value.ExpiresAt)
	return err
}
func (s *Store) TakeLogin(ctx context.Context, state, binding string) (gramstack.LoginTransaction, error) {
	var value gramstack.LoginTransaction
	stateHash, bindingHash := ownerOf(state), ownerOf(binding)
	var encrypted []byte
	if err := s.pool.QueryRow(ctx, `DELETE FROM sakhi_login_flows WHERE state_hash=$1 AND binding_hash=$2 AND expires_at>clock_timestamp() RETURNING payload`, stateHash, bindingHash).Scan(&encrypted); err != nil {
		return value, gramstack.ErrAuthentication
	}
	raw, err := s.open(stateHash, bindingHash, "login-flow", encrypted)
	if err != nil || json.Unmarshal(raw, &value) != nil {
		return value, gramstack.ErrAuthentication
	}
	return value, nil
}
func (s *Store) identitySession(ctx context.Context, identity gramstack.Identity, priorSession, priorIdentity string) (string, string, time.Time, error) {
	token, session := rand.Text(), rand.Text()
	until := time.Now().Add(time.Hour)
	if identity.ExpiresAt.Before(until) {
		until = identity.ExpiresAt
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", "", until, ErrUnavailable
	}
	hash, owner := ownerOf(token), ownerOf(session)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE sakhi_sessions SET expires_at=clock_timestamp() WHERE owner_hash=$1`, ownerOf(priorSession)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sakhi_identity_sessions WHERE token_hash=$1`, ownerOf(priorIdentity)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sakhi_sessions(owner_hash,expires_at) VALUES($1,$2)`, owner, until); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO sakhi_identity_sessions(token_hash,owner_hash,payload,expires_at) SELECT $1,$2,$3,$4 WHERE $4>clock_timestamp()`, hash, owner, s.seal(hash, owner, "identity-session", raw), until)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrUnavailable
		}
		return nil
	})
	return token, session, until, err
}
func (s *Store) identity(ctx context.Context, token, owner string) (gramstack.Identity, error) {
	var value gramstack.Identity
	if len(token) != 26 {
		return value, ErrNotFound
	}
	var encrypted []byte
	hash := ownerOf(token)
	err := s.pool.QueryRow(ctx, `SELECT payload FROM sakhi_identity_sessions WHERE token_hash=$1 AND owner_hash=$2 AND expires_at>clock_timestamp()`, hash, owner).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, ErrUnavailable
	}
	raw, err := s.open(hash, owner, "identity-session", encrypted)
	if err != nil || json.Unmarshal(raw, &value) != nil {
		return value, ErrUnavailable
	}
	return value, nil
}
func registerLogin(mux *http.ServeMux, service *Service, auth *gramstack.AuthClient, origin, productCookie string, authenticate func(http.ResponseWriter, *http.Request) (string, bool)) {
	secure := strings.HasPrefix(origin, "https://")
	cookie := "__Host-sakhi-identity"
	if !secure {
		cookie = "sakhi_identity_dev"
	}
	sessionToken := func(r *http.Request) string {
		cookies := r.CookiesNamed(cookie)
		if len(cookies) != 1 {
			return ""
		}
		return cookies[0].Value
	}
	mux.HandleFunc("GET /api/identity", func(w http.ResponseWriter, r *http.Request) {
		if auth == nil || service == nil {
			writeJSON(w, 200, map[string]bool{"enabled": false, "authenticated": false})
			return
		}
		browser, err := r.Cookie(productCookie)
		if err != nil {
			writeJSON(w, 200, map[string]bool{"enabled": true, "authenticated": false})
			return
		}
		owner, _, err := service.store.Session(r.Context(), browser.Value)
		if err != nil {
			writeJSON(w, 200, map[string]bool{"enabled": true, "authenticated": false})
			return
		}
		identity, err := service.store.identity(r.Context(), sessionToken(r), owner)
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, 200, map[string]bool{"enabled": true, "authenticated": false})
			return
		}
		if err != nil {
			problem(w, ErrUnavailable, Application{})
			return
		}
		writeJSON(w, 200, map[string]any{"enabled": true, "authenticated": true, "identity": identity})
	})
	mux.HandleFunc("GET /auth/gramstack/login", func(w http.ResponseWriter, r *http.Request) {
		if auth == nil {
			http.NotFound(w, r)
			return
		}
		auth.Login(w, r)
	})
	mux.HandleFunc("GET /auth/gramstack/callback", func(w http.ResponseWriter, r *http.Request) {
		if auth == nil || service == nil {
			http.NotFound(w, r)
			return
		}
		identity, err := auth.Callback(w, r)
		if err != nil {
			http.Error(w, "Sign-in could not be completed. Please start again.", 403)
			return
		}
		priorBrowser, _ := r.Cookie(productCookie)
		prior := ""
		if priorBrowser != nil {
			prior = priorBrowser.Value
		}
		token, browserToken, until, err := service.store.identitySession(r.Context(), identity, prior, sessionToken(r))
		if err != nil {
			problem(w, ErrUnavailable, Application{})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookie, Value: token, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(until).Seconds())})
		http.SetCookie(w, &http.Cookie{Name: productCookie, Value: browserToken, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(until).Seconds())})
		http.Redirect(w, r, "/?signed_in=1", http.StatusSeeOther)
	})
	mux.HandleFunc("POST /auth/gramstack/logout", func(w http.ResponseWriter, r *http.Request) {
		if auth == nil || service == nil {
			http.NotFound(w, r)
			return
		}
		owner, ok := authenticate(w, r)
		if !ok {
			return
		}
		if _, err := service.store.pool.Exec(r.Context(), `DELETE FROM sakhi_identity_sessions WHERE token_hash=$1`, ownerOf(sessionToken(r))); err != nil {
			problem(w, ErrUnavailable, Application{})
			return
		}
		if _, err := service.store.pool.Exec(r.Context(), `UPDATE sakhi_sessions SET expires_at=clock_timestamp() WHERE owner_hash=$1`, owner); err != nil {
			problem(w, ErrUnavailable, Application{})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: productCookie, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		http.SetCookie(w, &http.Cookie{Name: cookie, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		writeJSON(w, 200, map[string]bool{"authenticated": false})
	})
}
