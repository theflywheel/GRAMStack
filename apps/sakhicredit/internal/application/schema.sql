CREATE TABLE sakhi_sessions (
 owner_hash text PRIMARY KEY, expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '2 hours'
);
CREATE TABLE sakhi_applications (
 application_id text PRIMARY KEY, owner_hash text NOT NULL REFERENCES sakhi_sessions(owner_hash), create_key text NOT NULL,
 input_digest text NOT NULL, input_cipher bytea NOT NULL,
 state text NOT NULL CHECK(state IN ('CONNECTING','WAITING','READY','FETCHING','COMPLETE','UNKNOWN','BLOCKED','EXPIRED','CANCELLING','CANCELLED')),
 version bigint NOT NULL DEFAULT 1, request_id text UNIQUE, interaction_url text, platform_status text NOT NULL DEFAULT '', platform_version bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '1 hour',
 request_expires_at timestamptz, fetch_deadline timestamptz, result_cipher bytea,
 UNIQUE(owner_hash,create_key), CHECK((state='COMPLETE')=(result_cipher IS NOT NULL)), CHECK(state NOT IN ('FETCHING','COMPLETE') OR fetch_deadline IS NOT NULL)
);
CREATE INDEX sakhi_application_expiry ON sakhi_applications(expires_at);
CREATE INDEX sakhi_fetch_recovery ON sakhi_applications(fetch_deadline) WHERE state='FETCHING';
CREATE FUNCTION sakhi_immutable_application() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.application_id,NEW.owner_hash,NEW.create_key,NEW.input_digest,NEW.input_cipher,NEW.created_at,NEW.expires_at) IS DISTINCT FROM ROW(OLD.application_id,OLD.owner_hash,OLD.create_key,OLD.input_digest,OLD.input_cipher,OLD.created_at,OLD.expires_at)
 OR (OLD.request_id IS NOT NULL AND ROW(NEW.request_id,NEW.interaction_url,NEW.request_expires_at) IS DISTINCT FROM ROW(OLD.request_id,OLD.interaction_url,OLD.request_expires_at))
 OR (OLD.fetch_deadline IS NOT NULL AND NEW.fetch_deadline IS DISTINCT FROM OLD.fetch_deadline)
 OR (OLD.state IN ('COMPLETE','UNKNOWN','BLOCKED','EXPIRED','CANCELLED') AND NEW IS DISTINCT FROM OLD) THEN
  RAISE EXCEPTION 'immutable application authority or outcome';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_sakhi_application BEFORE UPDATE ON sakhi_applications FOR EACH ROW EXECUTE FUNCTION sakhi_immutable_application();
