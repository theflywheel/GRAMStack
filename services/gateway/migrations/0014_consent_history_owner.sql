CREATE TABLE consent_history_owners (
 request_id text PRIMARY KEY REFERENCES access_requests(request_id) ON DELETE CASCADE,
 owner_digest text NOT NULL CHECK(owner_digest ~ '^sha256:[0-9a-f]{64}$'),
 principal_ref text NOT NULL
);
CREATE INDEX consent_history_owner_requests ON consent_history_owners(owner_digest,request_id);
CREATE TRIGGER immutable_consent_history_owner BEFORE UPDATE ON consent_history_owners
 FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
