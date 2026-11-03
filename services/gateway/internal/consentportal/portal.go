// Package consentportal exposes the authenticated first-party human decision UI.
package consentportal

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/authn"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/enforcement"
)

type SubjectReferences interface {
	ForRequest(context.Context, string, string) (string, error)
}
type Config struct {
	Pool     *pgxpool.Pool
	Auth     *authn.Service
	Consents *cm.Service
	Subjects SubjectReferences
	Origin   string
}
type Service struct{ config Config }

func New(c Config) (*Service, error) {
	if c.Pool == nil || c.Auth == nil || c.Consents == nil || c.Subjects == nil || c.Origin == "" {
		return nil, errors.New("consent portal dependencies required")
	}
	c.Origin = strings.TrimSuffix(c.Origin, "/")
	return &Service{c}, nil
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /interactions/{id}/consent", s.notice)
	mux.HandleFunc("POST /interactions/{id}/decision", s.decide)
	mux.HandleFunc("GET /interactions/{id}/receipt", s.receipt)
	mux.HandleFunc("POST /consents/{id}/withdrawal", s.withdraw)
	mux.HandleFunc("GET /assets/consent.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(styles))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Gramstack-Consent-Portal-Version", "0.1.0")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}
func (s *Service) session(w http.ResponseWriter, r *http.Request, id string, redirect bool) (authn.Session, bool) {
	session, err := s.config.Auth.Session(r, id)
	if err != nil {
		if redirect {
			http.Redirect(w, r, s.config.Origin+"/interactions/"+url.PathEscape(id), http.StatusSeeOther)
		} else {
			s.fail(w, cm.ErrForbidden)
		}
		return session, false
	}
	return session, true
}
func (s *Service) consentID(ctx context.Context, requestID string) (string, error) {
	var id string
	err := s.config.Pool.QueryRow(ctx, `SELECT consent_id FROM consent_state WHERE request_id=$1`, requestID).Scan(&id)
	return id, err
}
func (s *Service) record(ctx context.Context, requestID, principal string) (cm.Record, error) {
	id, err := s.consentID(ctx, requestID)
	if err != nil {
		return cm.Record{}, err
	}
	return s.config.Consents.Record(ctx, id, principal)
}
func (s *Service) notice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, ok := s.session(w, r, id, true)
	if !ok {
		return
	}
	if languages, exists := r.URL.Query()["lang"]; exists && (len(languages) != 1 || languages[0] != "en") {
		s.fail(w, errLanguage)
		return
	}
	consentID, err := s.consentID(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		var snapshot cm.Snapshot
		var raw []byte
		var digest, reference string
		err = s.config.Pool.QueryRow(r.Context(), `SELECT a.snapshot_bytes,r.request_digest FROM accepted_requests a JOIN access_requests r USING(request_id) WHERE r.request_id=$1`, id).Scan(&raw, &digest)
		if err != nil || json.Unmarshal(raw, &snapshot) != nil {
			s.fail(w, cm.ErrUnavailable)
			return
		}
		if _, err = s.config.Auth.Principal(r.Context(), id, session.PrincipalRef); err != nil {
			s.fail(w, cm.ErrForbidden)
			return
		}
		// The trusted verifier reference is server state; the browser cannot supply it.
		reference, err = s.config.Subjects.ForRequest(r.Context(), id, session.PrincipalRef)
		if err != nil {
			s.fail(w, err)
			return
		}
		notice, err := s.config.Consents.Create(r.Context(), cm.CreateRequest{Snapshot: snapshot, RequestDigest: digest, SubjectContextRef: reference, Language: "en"})
		if err != nil {
			s.fail(w, err)
			return
		}
		consentID = notice.ConsentRequestID
	} else if err != nil {
		s.fail(w, cm.ErrUnavailable)
		return
	}
	record, err := s.config.Consents.Record(r.Context(), consentID, session.PrincipalRef)
	if err != nil {
		s.fail(w, err)
		return
	}
	if record.Consent.Status != "PENDING" {
		http.Redirect(w, r, s.receiptURL(id), http.StatusSeeOther)
		return
	}
	presentation, err := s.config.Consents.Present(r.Context(), consentID, session.PrincipalRef, sessionDigest(session), session.ExpiresAt)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, view{Title: "Review this data request", CSRF: session.CSRF, RequestID: id, Record: record, Presentation: presentation, Review: true})
}
func sessionDigest(session authn.Session) string {
	return cm.Digest([]byte("consent.presentation.v1\x00" + session.RequestID + "\x00" + session.CSRF))
}
func form(w http.ResponseWriter, r *http.Request, required, optional []string) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		return cm.ErrBinding
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil {
		return cm.ErrBinding
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		if len(r.PostForm[key]) != 1 || r.PostForm.Get(key) == "" {
			return cm.ErrBinding
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			return cm.ErrBinding
		}
	}
	return nil
}
func (s *Service) decide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, ok := s.session(w, r, id, false)
	if !ok {
		return
	}
	if err := form(w, r, []string{"csrf", "decision", "request_digest", "notice_digest", "notice_version", "language", "presentation_id"}, []string{"confirmation"}); err != nil {
		s.fail(w, err)
		return
	}
	if s.config.Auth.VerifyCSRF(r, session, r.PostForm.Get("csrf")) != nil {
		s.fail(w, cm.ErrForbidden)
		return
	}
	action := r.PostForm.Get("decision")
	if action != "approve" && action != "reject" {
		s.fail(w, cm.ErrBinding)
		return
	}
	if action == "approve" && r.PostForm.Get("confirmation") != "yes" {
		s.fail(w, errConfirmation)
		return
	}
	consentID, err := s.consentID(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), session.ExpiresAt)
	defer cancel()
	_, err = s.config.Consents.Decide(ctx, consentID, session.PrincipalRef, cm.Decision{Action: action, RequestDigest: r.PostForm.Get("request_digest"), NoticeDigest: r.PostForm.Get("notice_digest"), NoticeVersion: r.PostForm.Get("notice_version"), Language: r.PostForm.Get("language"), PresentationID: r.PostForm.Get("presentation_id"), SessionDigest: sessionDigest(session), ExplicitConfirmation: action == "approve"})
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, s.receiptURL(id), http.StatusSeeOther)
}
func (s *Service) receiptURL(id string) string {
	return s.config.Origin + "/interactions/" + url.PathEscape(id) + "/receipt"
}
func (s *Service) receipt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, ok := s.session(w, r, id, true)
	if !ok {
		return
	}
	record, err := s.record(r.Context(), id, session.PrincipalRef)
	if err != nil {
		s.fail(w, err)
		return
	}
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="consent-record.json"`)
		json.NewEncoder(w).Encode(record)
		return
	}
	s.render(w, view{Title: "Your permission record", RequestID: id, CSRF: session.CSRF, Record: record})
}
func (s *Service) withdraw(w http.ResponseWriter, r *http.Request) {
	consentID := r.PathValue("id")
	var requestID string
	if err := s.config.Pool.QueryRow(r.Context(), `SELECT request_id FROM consent_state WHERE consent_id=$1`, consentID).Scan(&requestID); err != nil {
		s.fail(w, cm.ErrForbidden)
		return
	}
	session, ok := s.session(w, r, requestID, false)
	if !ok {
		return
	}
	if form(w, r, []string{"csrf"}, nil) != nil || s.config.Auth.VerifyCSRF(r, session, r.PostForm.Get("csrf")) != nil {
		s.fail(w, cm.ErrForbidden)
		return
	}
	if _, err := s.config.Consents.GetNotice(r.Context(), consentID, session.PrincipalRef); err != nil {
		s.fail(w, err)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), session.ExpiresAt)
	defer cancel()
	if _, err := (enforcement.Repository{Pool: s.config.Pool}).Withdraw(ctx, consentID, 0); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, s.receiptURL(requestID), http.StatusSeeOther)
}

var errLanguage = errors.New("supported notice language required")
var errConfirmation = errors.New("explicit approval confirmation required")

func (s *Service) fail(w http.ResponseWriter, err error) {
	code, title, message := http.StatusServiceUnavailable, "This page is temporarily unavailable", "We could not confirm this action. Refresh your permission record before trying again."
	switch {
	case errors.Is(err, errLanguage):
		code, title, message = 400, "English is the supported language", "Other notice languages are not supported in this version. No translated notice or permission was created."
	case errors.Is(err, errConfirmation):
		code, title, message = 400, "Confirm your choice", "Read the exact notice and tick the confirmation before approving. You can reject without ticking it."
	case errors.Is(err, cm.ErrExpired):
		code, title, message = 410, "This request has expired", "No new permission can be recorded for this notice. Ask the requesting organisation for a new request."
	case errors.Is(err, cm.ErrConflict), errors.Is(err, cm.ErrNotReady), errors.Is(err, enforcement.ErrVersion):
		code, title, message = 409, "This permission has changed", "Reload your permission record to see its current state. No new decision was recorded."
	case errors.Is(err, cm.ErrForbidden), errors.Is(err, cm.ErrBinding), errors.Is(err, pgx.ErrNoRows), errors.Is(err, authn.ErrAuthentication):
		code, title, message = 403, "We could not verify this action", "Return to the requesting application and reopen your authenticated interaction. No permission was added."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	s.render(w, view{Title: title, Message: message, Error: true})
}

type view struct {
	Title, Message, RequestID, CSRF string
	Record                          cm.Record
	Presentation                    cm.Presentation
	Review, Error                   bool
}

func (s *Service) render(w http.ResponseWriter, value view) {
	var buffer bytes.Buffer
	if err := page.Execute(&buffer, value); err != nil {
		http.Error(w, "Page unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buffer.Bytes())
}

//go:embed page.html
var pageText string

//go:embed style.css
var styles string
var page = template.Must(template.New("consent").Funcs(template.FuncMap{"state": stateLabel, "description": stateDescription, "canWithdraw": func(status string) bool {
	return status == "APPROVAL_RECORDED" || status == "ACTIVE" || status == "EXHAUSTED"
}, "time": func(value time.Time) string { return value.UTC().Format("2 Jan 2006, 15:04 UTC") }}).Parse(pageText))

func stateLabel(status string) string {
	switch status {
	case "PENDING":
		return "Awaiting your choice"
	case "APPROVAL_RECORDED":
		return "Approval recorded"
	case "ACTIVE":
		return "Permission active"
	case "REJECTED":
		return "Request rejected"
	case "WITHDRAWN":
		return "Permission stopped"
	case "EXPIRED":
		return "Permission expired"
	case "EXHAUSTED":
		return "Retrieval admitted"
	default:
		return "Permission record"
	}
}
func stateDescription(status string) string {
	switch status {
	case "APPROVAL_RECORDED":
		return "Your approval has been saved. Permission is not active until signing and verification complete."
	case "ACTIVE":
		return "This permission can admit one retrieval within its validity window. Current policy checks still apply."
	case "REJECTED":
		return "No disclosure is authorised by this decision. A different scope requires a new request."
	case "WITHDRAWN":
		return "This permission cannot admit a new retrieval. An already admitted disclosure cannot be recalled."
	case "EXPIRED":
		return "The validity window has ended. This record does not authorise a new disclosure."
	case "EXHAUSTED":
		return "A retrieval was admitted. This record does not establish successful delivery and cannot authorise a second retrieval."
	default:
		return "Review the exact request before making your choice."
	}
}
