-- Acknowledgement belongs to one subscriber; it must not consume another's event.
CREATE TABLE outbox_deliveries (
 subscriber text NOT NULL,
 event_id text NOT NULL REFERENCES outbox(event_id),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts>=0),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 lease_token text,
 lease_until timestamptz,
 acknowledged_at timestamptz,
 PRIMARY KEY(subscriber,event_id),
 CHECK((lease_token IS NULL)=(lease_until IS NULL))
);
CREATE INDEX outbox_delivery_due ON outbox_deliveries(subscriber,available_at) WHERE acknowledged_at IS NULL;
CREATE INDEX outbox_dispatch_events ON outbox(event_type,created_at,event_id);
CREATE INDEX consent_due ON consent_state(valid_until) WHERE status IN ('PENDING','APPROVAL_RECORDED','ACTIVE');
