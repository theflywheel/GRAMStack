CREATE TABLE disclosure_handling_reports (
 receipt_id text NOT NULL REFERENCES evidence_receipts(receipt_id),
 report_id text NOT NULL,
 producer_id text NOT NULL,
 action text NOT NULL CHECK (action IN ('retained','deleted','retention_exception')),
 exception_code text NOT NULL CHECK (
   (action = 'retention_exception' AND exception_code IN ('legal_hold','statutory_retention'))
   OR (action <> 'retention_exception' AND exception_code = '')
 ),
 occurred_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 terms_version text NOT NULL,
 terms_digest text NOT NULL,
 PRIMARY KEY (receipt_id,report_id)
);
CREATE TRIGGER immutable_handling_report BEFORE UPDATE ON disclosure_handling_reports
 FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
