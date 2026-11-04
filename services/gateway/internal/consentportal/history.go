package consentportal

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var historyRequestID = regexp.MustCompile(`^[A-Z2-7]{26}$`)

type historyRow struct {
	RequestID string
	Consumer  string
	Consent   string
	Request   string
	CreatedAt time.Time
}

var historyPage = template.Must(template.New("history").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="robots" content="noindex,nofollow">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Your consent history</title><link rel="stylesheet" href="/assets/consent.css"></head>
<body><main><div class="eyebrow">GRAMSTACK · PRIVATE RECORDS</div><h1>Your consent history</h1>
<p>Registry {{.Registry}}</p>
{{if .SignIn}}<p>Sign in to see the consent and disclosure records linked to this identity.</p>
<a class="button secondary" href="/auth/history/{{.Registry}}">Sign in to view history</a>
{{else}}
{{range .Rows}}<section class="card"><h2>{{.Consumer}}</h2>
<p>Consent {{.Consent}} · Request {{.Request}}</p>
<p>{{.CreatedAt.UTC.Format "2 Jan 2006, 15:04 UTC"}}</p>
<a href="/consent-history/{{$.Registry}}/{{.RequestID}}/receipt">Download consent record</a></section>
{{else}}<p>No consent records are available for this identity.</p>{{end}}
{{if .Next}}<a href="/consent-history/{{.Registry}}?after={{.Next}}">Next page</a>{{end}}
{{end}}</main></body></html>`))

func (s *Service) history(w http.ResponseWriter, r *http.Request) {
	registry := r.PathValue("registry")
	if !historyRegistryPath.MatchString(registry) {
		http.NotFound(w, r)
		return
	}
	after := r.URL.Query().Get("after")
	if after != "" && !historyRequestID.MatchString(after) {
		http.Error(w, "Invalid history cursor.", http.StatusBadRequest)
		return
	}
	owner, err := s.config.Auth.HistoryOwner(r, registry)
	view := struct {
		Registry string
		SignIn   bool
		Rows     []historyRow
		Next     string
	}{Registry: registry, SignIn: err != nil}
	if err == nil {
		rows, err := s.config.Pool.Query(r.Context(), `SELECT a.request_id,a.consumer_id,c.status,a.status,a.created_at
			FROM consent_history_owners h JOIN access_requests a USING(request_id)
			JOIN consent_state c USING(request_id) JOIN consent_requests cr USING(consent_id)
			WHERE h.owner_digest=$1 AND h.principal_ref=cr.principal_context_ref AND h.request_id>$2
			ORDER BY h.request_id LIMIT 26`, owner, after)
		if err == nil {
			view.Rows, err = pgx.CollectRows(rows, pgx.RowToStructByPos[historyRow])
		}
		if err != nil {
			http.Error(w, "Consent history is temporarily unavailable.", http.StatusServiceUnavailable)
			return
		}
		if len(view.Rows) > 25 {
			view.Rows = view.Rows[:25]
			view.Next = view.Rows[24].RequestID
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = historyPage.Execute(w, view)
}

var historyRegistryPath = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func (s *Service) historyReceipt(w http.ResponseWriter, r *http.Request) {
	registry, id := r.PathValue("registry"), r.PathValue("id")
	if !historyRegistryPath.MatchString(registry) || !historyRequestID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	owner, err := s.config.Auth.HistoryOwner(r, registry)
	if err != nil {
		http.Redirect(w, r, s.config.Origin+"/consent-history/"+url.PathEscape(registry), http.StatusSeeOther)
		return
	}
	var principal string
	err = s.config.Pool.QueryRow(r.Context(), `SELECT h.principal_ref
		FROM consent_history_owners h JOIN consent_state c USING(request_id)
		JOIN consent_requests cr USING(consent_id)
		WHERE h.request_id=$1 AND h.owner_digest=$2 AND h.principal_ref=cr.principal_context_ref`, id, owner).Scan(&principal)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Consent history is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	record, err := s.record(r.Context(), id, principal)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="consent-record.json"`)
	_ = json.NewEncoder(w).Encode(record)
}
