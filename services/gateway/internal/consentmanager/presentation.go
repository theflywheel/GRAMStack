package consentmanager

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Presentation struct {
	ID            string    `json:"presentation_id"`
	NoticeDigest  string    `json:"notice_digest"`
	NoticeVersion string    `json:"notice_version"`
	Language      string    `json:"language"`
	Channel       string    `json:"channel"`
	RenderedAt    time.Time `json:"rendered_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// Present records a server-rendered notice, not a claim that a person read it.
// The caller supplies a digest of its authenticated session's CSRF binding.
func (s *Service) Present(ctx context.Context, id, principal, sessionDigest string, sessionExpires time.Time) (Presentation, error) {
	if !digestPattern.MatchString(sessionDigest) {
		return Presentation{}, ErrBinding
	}
	var result Presentation
	err := pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		value, err := load(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if principal == "" || principal != value.principalRef {
			return ErrForbidden
		}
		until := earlier(sessionExpires, earlier(value.notice.ExpiresAt, value.subjectExpiresAt))
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if !now.Before(until) {
			return ErrExpired
		}
		if value.rawStatus != "PENDING" {
			return ErrNotReady
		}
		_, err = tx.Exec(ctx, `INSERT INTO consent_presentations(presentation_id,consent_id,principal_ref,session_digest,notice_digest,notice_version,language,channel,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,'WEB',$8) ON CONFLICT(consent_id,session_digest,notice_digest) DO NOTHING`, rand.Text(), id, principal, sessionDigest, value.notice.NoticeDigest, value.notice.NoticeVersion, value.notice.Language, until)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT presentation_id,notice_digest,notice_version,language,channel,rendered_at,expires_at FROM consent_presentations WHERE consent_id=$1 AND principal_ref=$2 AND session_digest=$3 AND notice_digest=$4 AND expires_at>clock_timestamp()`, id, principal, sessionDigest, value.notice.NoticeDigest).Scan(&result.ID, &result.NoticeDigest, &result.NoticeVersion, &result.Language, &result.Channel, &result.RenderedAt, &result.ExpiresAt)
	})
	return result, err
}

type DecisionRecord struct {
	Action               string        `json:"decision"`
	DecidedAt            time.Time     `json:"decided_at"`
	ExplicitConfirmation bool          `json:"explicit_confirmation"`
	Presentation         *Presentation `json:"presentation,omitempty"`
}
type HandlingReport struct {
	Action        string
	ExceptionCode string
	Producer      string
	OccurredAt    time.Time
}

type Record struct {
	HandlingReports       []HandlingReport `json:"-"`
	MoreHandlingReports   bool             `json:"-"`
	RequestStatus         string           `json:"request_status"`
	GeneratedAt           time.Time        `json:"generated_at"`
	SchemaVersion         string           `json:"schema_version"`
	RequestID             string           `json:"request_id"`
	Notice                Notice           `json:"notice"`
	Consent               Status           `json:"consent"`
	Decision              *DecisionRecord  `json:"decision,omitempty"`
	SignedArtefact        string           `json:"signed_artefact,omitempty"`
	AdmissionCutoffPassed bool             `json:"admission_cutoff_passed"`
}

// Record returns read-only provenance. It never signs, activates or renews consent.
func (s *Service) Record(ctx context.Context, id, principal string) (Record, error) {
	tx, err := s.config.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	value, err := load(ctx, tx, id, false)
	if err != nil {
		return Record{}, err
	}
	if principal == "" || principal != value.principalRef {
		return Record{}, ErrForbidden
	}
	result := Record{SchemaVersion: "gramstack.consent-record.v1", RequestID: value.requestID, Notice: value.notice, Consent: value.status, AdmissionCutoffPassed: value.status.Use == "RESERVED"}
	var decision DecisionRecord
	err = tx.QueryRow(ctx, `SELECT action,decided_at FROM consent_decisions WHERE consent_id=$1`, id).Scan(&decision.Action, &decision.DecidedAt)
	if err == nil {
		result.Decision = &decision
		var presentation Presentation
		err = tx.QueryRow(ctx, `SELECT p.presentation_id,p.notice_digest,p.notice_version,p.language,p.channel,p.rendered_at,p.expires_at,d.explicit_confirmation FROM consent_decision_presentations d JOIN consent_presentations p USING(presentation_id) WHERE d.consent_id=$1`, id).Scan(&presentation.ID, &presentation.NoticeDigest, &presentation.NoticeVersion, &presentation.Language, &presentation.Channel, &presentation.RenderedAt, &presentation.ExpiresAt, &decision.ExplicitConfirmation)
		if err == nil {
			decision.Presentation = &presentation
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return Record{}, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Record{}, err
	}
	err = tx.QueryRow(ctx, `SELECT status,clock_timestamp() FROM access_requests WHERE request_id=$1`, value.requestID).Scan(&result.RequestStatus, &result.GeneratedAt)
	if err != nil {
		return Record{}, err
	}
	err = tx.QueryRow(ctx, `SELECT jws FROM consent_artefacts WHERE consent_id=$1`, id).Scan(&result.SignedArtefact)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Record{}, err
	}
	if result.RequestStatus == "COMPLETED" || result.RequestStatus == "DELIVERY_UNKNOWN" {
		rows, err := tx.Query(ctx, `SELECT h.action,h.exception_code,h.producer_id,h.occurred_at
			FROM gateway_retrieval_attempts g JOIN disclosure_handling_reports h ON h.receipt_id=g.receipt_id
			WHERE g.request_id=$1 ORDER BY h.recorded_at DESC,h.report_id DESC LIMIT 11`, value.requestID)
		if err != nil {
			return Record{}, err
		}
		reports, err := pgx.CollectRows(rows, pgx.RowToStructByPos[HandlingReport])
		if err != nil {
			return Record{}, err
		}
		if len(reports) > 10 {
			result.MoreHandlingReports = true
			reports = reports[:10]
		}
		result.HandlingReports = reports
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	return result, nil
}
