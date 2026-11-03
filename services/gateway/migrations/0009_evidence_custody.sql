CREATE TABLE evidence_receipts (
 receipt_id text PRIMARY KEY,event_id text NOT NULL UNIQUE,adapter_id text NOT NULL,
 request_id text NOT NULL REFERENCES access_requests(request_id),request_digest text NOT NULL,
 consent_id text NOT NULL REFERENCES consent_state(consent_id),grant_jti text NOT NULL,
 attempt_id text NOT NULL,redemption_id text NOT NULL REFERENCES disclosure_reservations(redemption_id),
 outcome text NOT NULL CHECK(outcome IN ('release_ready','failed','delivery_unknown')),
 evidence_digest text NOT NULL,jws text NOT NULL,accepted_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX one_release_evidence ON evidence_receipts(redemption_id) WHERE outcome='release_ready';
CREATE INDEX evidence_request_receipts ON evidence_receipts(request_id,accepted_at,receipt_id);
CREATE TRIGGER immutable_evidence_receipt BEFORE UPDATE ON evidence_receipts FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TABLE evidence_anchor_state (
 receipt_id text PRIMARY KEY REFERENCES evidence_receipts(receipt_id),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','anchored')),
 anchor_reference jsonb,updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
