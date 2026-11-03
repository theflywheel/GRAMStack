CREATE TABLE registry_policy_versions (
 registry text NOT NULL, consumer_id text NOT NULL, resource text NOT NULL, purpose text NOT NULL,
 version text NOT NULL, policy_digest text NOT NULL, jws text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(registry,consumer_id,resource,purpose,version)
);
CREATE TABLE registry_policy_heads (
 registry text NOT NULL, consumer_id text NOT NULL, resource text NOT NULL, purpose text NOT NULL,
 version text NOT NULL, enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY(registry,consumer_id,resource,purpose),
 FOREIGN KEY(registry,consumer_id,resource,purpose,version) REFERENCES registry_policy_versions(registry,consumer_id,resource,purpose,version)
);
CREATE TABLE retrieval_grants (
 request_id text PRIMARY KEY REFERENCES access_requests(request_id),
 consent_id text NOT NULL REFERENCES consent_state(consent_id),
 consent_version bigint NOT NULL,
 jti text NOT NULL UNIQUE,
 policy_digest text NOT NULL,
 subject_digest text NOT NULL,
 jws text NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE grant_idempotency (
 consumer_id text NOT NULL,
 key_digest text NOT NULL,
 request_id text NOT NULL REFERENCES retrieval_grants(request_id),
 PRIMARY KEY(consumer_id,key_digest)
);
CREATE TRIGGER immutable_policy_version BEFORE UPDATE ON registry_policy_versions FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TRIGGER immutable_retrieval_grant BEFORE UPDATE ON retrieval_grants FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
CREATE TRIGGER immutable_grant_idempotency BEFORE UPDATE ON grant_idempotency FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
