CREATE TABLE rights_cases (
 case_id text PRIMARY KEY,
 request_id text NOT NULL REFERENCES consent_history_owners(request_id),
 consent_id text NOT NULL REFERENCES consent_state(consent_id),
 receipt_id text REFERENCES evidence_receipts(receipt_id),
 case_type text NOT NULL CHECK (case_type IN ('access','correction','erasure','grievance')),
 responsible_role text NOT NULL CHECK (responsible_role IN ('registry','recipient')),
 responsible_id text NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','acknowledged','resolved','exception')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 due_at timestamptz NOT NULL DEFAULT (clock_timestamp() + interval '7 days')
);
CREATE INDEX rights_cases_request ON rights_cases(request_id,created_at DESC,case_id DESC);
CREATE INDEX rights_cases_route ON rights_cases(responsible_role,responsible_id,status,created_at);
