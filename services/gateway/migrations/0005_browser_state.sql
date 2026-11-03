CREATE TABLE browser_state (
 kind text NOT NULL,
 key_digest text NOT NULL CHECK (key_digest ~ '^sha256:[0-9a-f]{64}$'),
 binding_digest text NOT NULL CHECK (binding_digest ~ '^sha256:[0-9a-f]{64}$'),
 payload bytea NOT NULL,
 expires_at timestamptz NOT NULL,
 PRIMARY KEY(kind,key_digest)
);
CREATE INDEX browser_state_expiry ON browser_state(expires_at);
