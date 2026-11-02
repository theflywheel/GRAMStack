CREATE TABLE consent_requests (
 consent_id text PRIMARY KEY REFERENCES consent_state(consent_id),
 scope_bytes bytea NOT NULL,
 subject_context_ref text NOT NULL,
 subject_context_digest text NOT NULL CHECK (subject_context_digest ~ '^sha256:[0-9a-f]{64}$'),
 principal_context_ref text NOT NULL,
 subject_expires_at timestamptz NOT NULL,
 recipient_terms_text text NOT NULL
);
CREATE TABLE consent_notices (
 consent_id text PRIMARY KEY REFERENCES consent_requests(consent_id),
 version text NOT NULL,
 language text NOT NULL,
 notice_text text NOT NULL,
 notice_digest text NOT NULL CHECK (notice_digest ~ '^sha256:[0-9a-f]{64}$'),
 expires_at timestamptz NOT NULL
);
CREATE TABLE consent_decisions (
 consent_id text PRIMARY KEY REFERENCES consent_requests(consent_id),
 action text NOT NULL CHECK(action IN ('approve','reject')),
 principal_context_ref text NOT NULL,
 request_digest text NOT NULL,
 notice_digest text NOT NULL,
 notice_version text NOT NULL,
 language text NOT NULL,
 decided_at timestamptz NOT NULL
);
CREATE TABLE consent_artefacts (
 consent_id text PRIMARY KEY REFERENCES consent_decisions(consent_id),
 payload_bytes bytea NOT NULL,
 jws text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION gramstack_immutable_consent_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'consent provenance rows are immutable';
END;
$$;
CREATE TRIGGER immutable_consent_request BEFORE UPDATE ON consent_requests FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TRIGGER immutable_consent_notice BEFORE UPDATE ON consent_notices FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TRIGGER immutable_consent_decision BEFORE UPDATE ON consent_decisions FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TRIGGER immutable_consent_artefact BEFORE UPDATE ON consent_artefacts FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
