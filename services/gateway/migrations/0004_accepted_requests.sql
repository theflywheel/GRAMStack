CREATE TABLE accepted_requests (
 request_id text PRIMARY KEY REFERENCES access_requests(request_id),
 consumer_id text NOT NULL,
 idempotency_digest text NOT NULL CHECK (idempotency_digest ~ '^sha256:[0-9a-f]{64}$'),
 body_digest text NOT NULL CHECK (body_digest ~ '^sha256:[0-9a-f]{64}$'),
 snapshot_bytes bytea NOT NULL,
 UNIQUE(consumer_id,idempotency_digest)
);
CREATE TRIGGER immutable_accepted_request BEFORE UPDATE ON accepted_requests FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
