ALTER TABLE rights_cases ADD COLUMN status_version bigint NOT NULL DEFAULT 1;
ALTER TABLE rights_cases ADD COLUMN exception_code text NOT NULL DEFAULT '';
CREATE TABLE rights_case_events (
 case_id text NOT NULL REFERENCES rights_cases(case_id),
 event_id text NOT NULL,
 action text NOT NULL,
 exception_code text NOT NULL,
 status text NOT NULL,
 status_version bigint NOT NULL,
 actor_thumbprint text NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(case_id,event_id),
 UNIQUE(case_id,status_version)
);
CREATE TRIGGER immutable_rights_case_event BEFORE UPDATE ON rights_case_events
 FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_consent_row();
