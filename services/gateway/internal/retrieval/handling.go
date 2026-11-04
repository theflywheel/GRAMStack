package retrieval

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type handlingInput struct {
	Action        string    `json:"action"`
	ExceptionCode string    `json:"exception_code,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func (v handlingInput) valid() bool {
	switch v.Action {
	case "retained", "deleted":
		return v.ExceptionCode == ""
	case "retention_exception":
		return v.ExceptionCode == "legal_hold" || v.ExceptionCode == "statutory_retention"
	default:
		return false
	}
}

func (s *Service) HandlingHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		receipt, report := r.PathValue("receipt"), r.PathValue("report")
		if !identifier.MatchString(receipt) || !identifier.MatchString(report) || r.URL.RawQuery != "" || r.URL.ForceQuery {
			failure(w, 404, "DISCLOSURE_NOT_FOUND")
			return
		}
		ctx := r.Context()
		var owner string
		var consentPayload []byte
		var acceptedAt, now time.Time
		err := s.pool.QueryRow(ctx, `SELECT g.consumer_id,c.payload_bytes,e.accepted_at,clock_timestamp()
			FROM gateway_retrieval_attempts g JOIN evidence_receipts e
			  ON e.receipt_id=g.receipt_id AND e.request_id=g.request_id AND e.attempt_id=g.attempt_id
			JOIN consent_artefacts c ON c.consent_id=e.consent_id
			WHERE e.receipt_id=$1 AND e.outcome='release_ready' AND g.state IN ('COMPLETED','DELIVERY_UNKNOWN')`, receipt).
			Scan(&owner, &consentPayload, &acceptedAt, &now)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !s.authority.IsRecipient(r, owner)) {
			failure(w, 404, "DISCLOSURE_NOT_FOUND")
			return
		}
		var consent cm.Artefact
		if err != nil || decode(consentPayload, &consent) != nil || consent.Consumer != owner {
			failure(w, 503, "HANDLING_REPORT_UNAVAILABLE")
			return
		}

		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		raw, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 2048))
		var input handlingInput
		if mediaErr != nil || media != "application/json" || readErr != nil || decode(raw, &input) != nil ||
			!input.valid() || input.OccurredAt.Before(acceptedAt) || input.OccurredAt.After(now) {
			failure(w, 400, "INVALID_HANDLING_REPORT")
			return
		}
		input.OccurredAt = input.OccurredAt.UTC().Truncate(time.Microsecond)
		var previous handlingInput
		var recorded time.Time
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO disclosure_handling_reports
				(receipt_id,report_id,producer_id,action,exception_code,occurred_at,terms_version,terms_digest)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(receipt_id,report_id) DO NOTHING`,
				receipt, report, owner, input.Action, input.ExceptionCode, input.OccurredAt, consent.RecipientTermsVersion, consent.RecipientTermsDigest)
			if err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT action,exception_code,occurred_at,recorded_at
				FROM disclosure_handling_reports WHERE receipt_id=$1 AND report_id=$2`, receipt, report).
				Scan(&previous.Action, &previous.ExceptionCode, &previous.OccurredAt, &recorded)
		})
		if err != nil {
			failure(w, 503, "HANDLING_REPORT_UNAVAILABLE")
			return
		}
		if previous.Action != input.Action || previous.ExceptionCode != input.ExceptionCode || !previous.OccurredAt.Equal(input.OccurredAt) {
			failure(w, 409, "HANDLING_REPORT_CONFLICT")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			handlingInput
			ReceiptID    string    `json:"receipt_id"`
			ReportID     string    `json:"report_id"`
			ProducerID   string    `json:"producer_id"`
			Verification string    `json:"verification"`
			TermsVersion string    `json:"terms_version"`
			TermsDigest  string    `json:"terms_digest"`
			RecordedAt   time.Time `json:"recorded_at"`
		}{previous, receipt, report, owner, "reported", consent.RecipientTermsVersion, consent.RecipientTermsDigest, recorded.UTC()})
	})
}
