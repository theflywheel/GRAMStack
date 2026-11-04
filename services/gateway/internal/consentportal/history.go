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
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

var historyRequestID = regexp.MustCompile(`^[A-Z2-7]{26}$`)

type historyRow struct {
	RequestID        string
	Consumer         string
	Consent          string
	Use              string
	Request          string
	CreatedAt        time.Time
	Delivery         string
	DeliveredAt      *time.Time
	Fields           []string
	ReceiptID        string
	AnchorStatus     string
	UsedAt           *time.Time
	AdmissionEventID string
}

var historyPage = template.Must(template.New("history").Funcs(template.FuncMap{"consentLabel": func(status, use string) string {
	status = cm.DisplayStatus(status, use)
	if status == "EXHAUSTED" {
		return "Single use spent"
	}
	return stateLabel(status)
}}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="robots" content="noindex,nofollow">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Your consent history</title><link rel="stylesheet" href="/assets/consent.css"></head>
<body><main><div class="eyebrow">GRAMSTACK · PRIVATE RECORDS</div><h1>Your consent history</h1>
<p>Registry {{.Registry}}</p>
{{if .SignIn}}<p>Sign in to see the consent and disclosure records linked to this identity.</p>
<a class="button secondary" href="/auth/history/{{.Registry}}">Sign in to view history</a>
{{else}}
{{range .Rows}}<section class="card"><h2>{{.Consumer}}</h2>
<p>{{consentLabel .Consent .Use}} · Request {{.Request}}</p>
<p class="muted">Request reference <code>{{.RequestID}}</code></p>
<p class="muted">Request created {{.CreatedAt.UTC.Format "2 Jan 2006, 15:04 UTC"}}</p>
<h3>Access audit</h3>
{{if .UsedAt}}
<dl>
<div><dt>Approval used to authorize retrieval</dt><dd>{{.UsedAt.UTC.Format "2 Jan 2006, 15:04:05 UTC"}}</dd></div>
<div><dt>Authorization event reference</dt><dd><code>{{.AdmissionEventID}}</code></dd></div>
</dl>
{{end}}
{{if .DeliveredAt}}
<dl>
<div><dt>Recipient application</dt><dd>{{.Consumer}}</dd></div>
<div><dt>Fields shared</dt><dd>{{range $i, $field := .Fields}}{{if $i}}, {{end}}<code>{{$field}}</code>{{end}}</dd></div>
<div><dt>Response written by GRAMStack</dt><dd>{{.DeliveredAt.UTC.Format "2 Jan 2006, 15:04:05 UTC"}}</dd></div>
<div><dt>Signed access receipt</dt><dd><code>{{.ReceiptID}}</code>{{if eq .AnchorStatus "anchored"}} · DeDi commitment verified{{else if eq .AnchorStatus "pending"}} · DeDi anchoring pending{{end}}</dd></div>
</dl>
<p><strong>1 of 1 authorized retrievals used.</strong> This consent cannot authorize another retrieval through GRAMStack.</p>
{{else if or (eq .Delivery "DELIVERY_UNKNOWN") (eq .Delivery "RELEASE_ATTEMPTED")}}
<p>Delivery is unconfirmed. GRAMStack will not retry this retrieval automatically.</p>
{{else}}<p>No completed disclosure is recorded for this request.</p>{{end}}
<a href="/consent-history/{{$.Registry}}/{{.RequestID}}/receipt">Download consent record</a>
<p><a href="/consent-history/{{$.Registry}}/{{.RequestID}}/rights">Rights and grievances</a></p></section>
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
	session, err := s.config.Auth.HistoryAccess(r, registry)
	view := struct {
		Registry string
		SignIn   bool
		Rows     []historyRow
		Next     string
	}{Registry: registry, SignIn: err != nil}
	if err == nil {
		rows, err := s.config.Pool.Query(r.Context(), `SELECT a.request_id,a.consumer_id,c.status,c.use_state,a.status,a.created_at,
				COALESCE(g.state,''),CASE WHEN g.state='COMPLETED' AND e.receipt_id IS NOT NULL THEN g.updated_at END,
				CASE WHEN e.receipt_id IS NOT NULL THEN ARRAY(SELECT jsonb_array_elements_text(
					convert_from(ar.snapshot_bytes,'UTF8')::jsonb #> '{request,fields}')) END,
				COALESCE(e.receipt_id,''),COALESCE(anchor.status,''),
				CASE WHEN admitted.event_id IS NOT NULL THEN r.reserved_at END,COALESCE(admitted.event_id,'')
			FROM consent_history_owners h JOIN access_requests a USING(request_id)
			JOIN consent_state c USING(request_id) JOIN consent_requests cr USING(consent_id)
			LEFT JOIN gateway_retrieval_attempts g ON g.request_id=a.request_id AND g.consumer_id=a.consumer_id
			LEFT JOIN evidence_receipts e ON e.receipt_id=g.receipt_id AND e.request_id=a.request_id
				AND e.consent_id=c.consent_id AND e.attempt_id=g.attempt_id AND e.outcome='release_ready'
			LEFT JOIN disclosure_reservations r ON r.consent_id=c.consent_id AND r.attempt_id=g.attempt_id
				AND r.grant_digest=g.grant_digest AND r.presenter_thumbprint=g.presenter_thumbprint
			LEFT JOIN LATERAL (SELECT min(o.event_id) AS event_id FROM outbox o
				WHERE r.consent_id IS NOT NULL AND o.aggregate_type='request' AND o.aggregate_id=a.request_id
				AND o.event_type='request.transitioned' AND o.payload->>'status'='REDEEMING'
				HAVING count(*)=1 AND bool_and(o.payload->>'request_id'=a.request_id
					AND o.payload->>'request_digest'=a.request_digest AND o.payload->>'event_id'=o.event_id
					AND o.created_at>=r.reserved_at)) admitted ON true
			LEFT JOIN accepted_requests ar ON ar.request_id=e.request_id
			LEFT JOIN evidence_anchor_state anchor ON anchor.receipt_id=e.receipt_id
			WHERE h.owner_digest=$1 AND h.principal_ref=cr.principal_context_ref AND h.request_id>$2
			ORDER BY h.request_id LIMIT 26`, session.OwnerDigest, after)
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
	session, err := s.config.Auth.HistoryAccess(r, registry)
	if err != nil {
		http.Redirect(w, r, s.config.Origin+"/consent-history/"+url.PathEscape(registry), http.StatusSeeOther)
		return
	}
	var principal string
	err = s.config.Pool.QueryRow(r.Context(), `SELECT h.principal_ref
		FROM consent_history_owners h JOIN consent_state c USING(request_id)
		JOIN consent_requests cr USING(consent_id)
		WHERE h.request_id=$1 AND h.owner_digest=$2 AND h.principal_ref=cr.principal_context_ref`, id, session.OwnerDigest).Scan(&principal)
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
