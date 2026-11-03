CREATE TABLE gateway_retrieval_attempts (
 request_id text PRIMARY KEY REFERENCES access_requests(request_id),attempt_id text NOT NULL UNIQUE,
 consumer_id text NOT NULL,grant_digest text NOT NULL,presenter_thumbprint text NOT NULL,
 state text NOT NULL DEFAULT 'DISPATCHED' CHECK(state IN ('DISPATCHED','RELEASE_ATTEMPTED','COMPLETED','DELIVERY_UNKNOWN','FAILED')),
 version bigint NOT NULL DEFAULT 1,receipt_id text REFERENCES evidence_receipts(receipt_id),
 deadline timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX gateway_abandoned_retrievals ON gateway_retrieval_attempts(deadline,request_id) WHERE state IN ('DISPATCHED','RELEASE_ATTEMPTED');
CREATE FUNCTION gramstack_immutable_dispatch_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.request_id,NEW.attempt_id,NEW.consumer_id,NEW.grant_digest,NEW.presenter_thumbprint,NEW.deadline,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.request_id,OLD.attempt_id,OLD.consumer_id,OLD.grant_digest,OLD.presenter_thumbprint,OLD.deadline,OLD.created_at) THEN
  RAISE EXCEPTION 'immutable dispatch binding';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_gateway_dispatch_binding BEFORE UPDATE ON gateway_retrieval_attempts FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_dispatch_binding();
