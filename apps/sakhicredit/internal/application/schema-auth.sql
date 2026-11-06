CREATE TABLE sakhi_login_flows (
 state_hash text PRIMARY KEY, binding_hash text NOT NULL, payload bytea NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX sakhi_login_flow_expiry ON sakhi_login_flows(expires_at);
CREATE TABLE sakhi_identity_sessions (
 token_hash text PRIMARY KEY, owner_hash text NOT NULL REFERENCES sakhi_sessions(owner_hash), payload bytea NOT NULL, expires_at timestamptz NOT NULL
);
CREATE INDEX sakhi_identity_session_expiry ON sakhi_identity_sessions(expires_at);
