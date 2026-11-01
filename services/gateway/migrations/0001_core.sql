CREATE TABLE access_requests (
 request_id text PRIMARY KEY,
 consumer_id text NOT NULL,
 request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
 status text NOT NULL CHECK (status IN ('AUTHENTICATION_PENDING','BINDING_PENDING','CONSENT_PENDING','CONSENTED','AUTHORIZED','REDEEMING','EVIDENCE_COMMITTED','RELEASE_ATTEMPTED','COMPLETED','DELIVERY_UNKNOWN','REJECTED','DENIED','WITHDRAWN','EXPIRED','FAILED')),
 version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(request_id,request_digest)
);
CREATE INDEX access_request_deadlines ON access_requests(expires_at);
CREATE TABLE consent_state (
 consent_id text PRIMARY KEY,
 request_id text NOT NULL UNIQUE,
 request_digest text NOT NULL,
 status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPROVAL_RECORDED','ACTIVE','REJECTED','WITHDRAWN','EXPIRED')),
 status_version bigint NOT NULL DEFAULT 1 CHECK (status_version > 0),
 use_state text NOT NULL DEFAULT 'AVAILABLE' CHECK (use_state IN ('AVAILABLE','RESERVED')),
 valid_until timestamptz NOT NULL,
 FOREIGN KEY(request_id,request_digest) REFERENCES access_requests(request_id,request_digest)
);
CREATE TABLE disclosure_reservations (
 consent_id text PRIMARY KEY REFERENCES consent_state(consent_id),
 redemption_id text NOT NULL UNIQUE,
 grant_jti text NOT NULL UNIQUE,
 grant_digest text NOT NULL CHECK (grant_digest ~ '^sha256:[0-9a-f]{64}$'),
 adapter_id text NOT NULL,
 presenter_thumbprint text NOT NULL CHECK (presenter_thumbprint ~ '^[A-Za-z0-9_-]{43}$'),
 attempt_id text NOT NULL,
 reserved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(adapter_id,attempt_id)
);
CREATE TABLE outbox (
 event_id text PRIMARY KEY,
 aggregate_type text NOT NULL,
 aggregate_id text NOT NULL,
 aggregate_version bigint NOT NULL,
 event_type text NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 published_at timestamptz,
 UNIQUE(aggregate_type,aggregate_id,aggregate_version)
);
CREATE INDEX outbox_unpublished ON outbox(created_at) WHERE published_at IS NULL;
