package consentportal

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type rightsCase struct {
	ID, Type, Role, Responsible, Status, Exception string
	CreatedAt, DueAt                               time.Time
}

var rightsPage = template.Must(template.New("rights").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="robots" content="noindex,nofollow">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Rights and grievances</title><link rel="stylesheet" href="/assets/consent.css"></head>
<body><main><div class="eyebrow">GRAMSTACK · PRIVATE RECORDS</div><h1>Rights and grievances</h1>
<a href="/consent-history/{{.Registry}}">Back to consent history</a>
<section class="card"><h2>Open a case</h2>
<p>Source corrections go to the registry. Requests about a recipient’s copy and grievances go to the recipient. The case records your request for follow-up.</p>
<form action="/consent-history/{{.Registry}}/{{.Request}}/rights" method="post">
<input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="case_id" value="{{.CaseID}}">
<label for="case-type">Request type</label>
<select id="case-type" name="case_type" required>
<option value="access">Access recipient-held data</option><option value="correction">Correct a source record</option>
<option value="erasure">Request erasure of the recipient’s copy</option><option value="grievance">Raise a grievance</option>
</select><p><button class="button primary">Submit case</button></p>
</form></section>
<section class="card"><h2>Case status</h2>
{{range .Cases}}<dl><div><dt>Case {{.ID}}</dt><dd>{{.Type}} · {{.Status}}</dd></div>
<div><dt>Responsible {{.Role}}</dt><dd>{{.Responsible}}</dd></div>
{{if eq .Status "resolved"}}<div><dt>Outcome</dt><dd>The responsible organisation reported resolution.</dd></div>{{end}}
{{if .Exception}}<div><dt>Reported exception</dt><dd>{{if eq .Exception "legal_hold"}}Legal hold{{else if eq .Exception "statutory_retention"}}Statutory retention{{else}}Insufficient information{{end}}</dd></div>{{end}}
<div><dt>Submitted</dt><dd>{{.CreatedAt.UTC.Format "2 Jan 2006, 15:04 UTC"}}</dd></div>
<div><dt>Demo follow-up target</dt><dd>{{.DueAt.UTC.Format "2 Jan 2006, 15:04 UTC"}}</dd></div></dl>
{{else}}<p>No cases for this consent record.</p>{{end}}</section>
</main></body></html>`))

func (s *Service) rights(w http.ResponseWriter, r *http.Request) {
	registry, id := r.PathValue("registry"), r.PathValue("id")
	if !historyRegistryPath.MatchString(registry) || !historyRequestID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	session, err := s.config.Auth.HistoryAccess(r, registry)
	if err != nil {
		http.Redirect(w, r, s.config.Origin+"/consent-history/"+registry, http.StatusSeeOther)
		return
	}
	var owned bool
	err = s.config.Pool.QueryRow(r.Context(), `SELECT EXISTS(
		SELECT 1 FROM consent_history_owners h JOIN consent_state c USING(request_id)
		JOIN consent_requests cr USING(consent_id)
		WHERE h.request_id=$1 AND h.owner_digest=$2 AND h.principal_ref=cr.principal_context_ref)`,
		id, session.OwnerDigest).Scan(&owned)
	if err != nil {
		http.Error(w, "Case status is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	if !owned {
		http.NotFound(w, r)
		return
	}
	rows, err := s.config.Pool.Query(r.Context(), `SELECT case_id,case_type,responsible_role,responsible_id,status,exception_code,created_at,due_at
		FROM rights_cases WHERE request_id=$1 ORDER BY created_at DESC,case_id DESC LIMIT 25`, id)
	var cases []rightsCase
	if err == nil {
		cases, err = pgx.CollectRows(rows, pgx.RowToStructByPos[rightsCase])
	}
	if err != nil {
		http.Error(w, "Case status is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = rightsPage.Execute(w, struct {
		Registry, Request, CSRF, CaseID string
		Cases                           []rightsCase
	}{registry, id, session.CSRF, rand.Text(), cases})
}

func (s *Service) createRightsCase(w http.ResponseWriter, r *http.Request) {
	registry, id := r.PathValue("registry"), r.PathValue("id")
	if !historyRegistryPath.MatchString(registry) || !historyRequestID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	session, err := s.config.Auth.HistoryAccess(r, registry)
	if err != nil || r.Header.Get("Origin") != s.config.Origin ||
		form(w, r, []string{"csrf", "case_id", "case_type"}, nil) != nil ||
		session.CSRF == "" || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
		http.Error(w, "Case submission requires an authenticated history form.", http.StatusForbidden)
		return
	}
	caseID, kind := r.PostForm.Get("case_id"), r.PostForm.Get("case_type")
	role := "recipient"
	switch kind {
	case "correction":
		role = "registry"
	case "access", "erasure", "grievance":
	default:
		http.Error(w, "Unsupported request type.", http.StatusBadRequest)
		return
	}
	if !historyRequestID.MatchString(caseID) {
		http.Error(w, "Invalid case reference.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var consent, recipient string
	err = s.config.Pool.QueryRow(ctx, `SELECT c.consent_id,a.consumer_id
		FROM consent_history_owners h JOIN access_requests a USING(request_id)
		JOIN consent_state c USING(request_id) JOIN consent_requests cr USING(consent_id)
		WHERE h.request_id=$1 AND h.owner_digest=$2 AND h.principal_ref=cr.principal_context_ref`, id, session.OwnerDigest).Scan(&consent, &recipient)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Case submission is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	responsible := recipient
	if role == "registry" {
		responsible = registry
	}
	var matches bool
	err = pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO rights_cases(case_id,request_id,consent_id,receipt_id,case_type,responsible_role,responsible_id)
			VALUES($1,$2,$3,(SELECT receipt_id FROM gateway_retrieval_attempts WHERE request_id=$2 AND state IN ('COMPLETED','DELIVERY_UNKNOWN')),$4,$5,$6)
			ON CONFLICT(case_id) DO NOTHING`, caseID, id, consent, kind, role, responsible)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT request_id=$2 AND case_type=$3 FROM rights_cases WHERE case_id=$1`, caseID, id, kind).Scan(&matches)
	})
	if err != nil {
		http.Error(w, "Case submission is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	if !matches {
		http.Error(w, "Case reference is already in use.", http.StatusConflict)
		return
	}
	http.Redirect(w, r, s.config.Origin+"/consent-history/"+registry+"/"+id+"/rights", http.StatusSeeOther)
}
