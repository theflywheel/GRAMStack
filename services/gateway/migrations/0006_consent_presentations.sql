CREATE TABLE consent_presentations (
 presentation_id text PRIMARY KEY,
 consent_id text NOT NULL REFERENCES consent_requests(consent_id),
 principal_ref text NOT NULL,
 session_digest text NOT NULL,
 notice_digest text NOT NULL,
 notice_version text NOT NULL,
 language text NOT NULL,
 channel text NOT NULL CHECK(channel='WEB'),
 rendered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 UNIQUE(consent_id,session_digest,notice_digest)
);
CREATE TABLE consent_decision_presentations (
 consent_id text PRIMARY KEY REFERENCES consent_decisions(consent_id),
 presentation_id text NOT NULL REFERENCES consent_presentations(presentation_id),
 explicit_confirmation boolean NOT NULL
);
CREATE TRIGGER immutable_consent_presentation BEFORE UPDATE ON consent_presentations FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TRIGGER immutable_decision_presentation BEFORE UPDATE ON consent_decision_presentations FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
