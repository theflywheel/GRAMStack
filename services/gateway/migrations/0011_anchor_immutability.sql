CREATE FUNCTION gramstack_immutable_verified_anchor() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='anchored' AND ROW(NEW.receipt_id,NEW.status,NEW.anchor_reference,NEW.updated_at) IS DISTINCT FROM ROW(OLD.receipt_id,OLD.status,OLD.anchor_reference,OLD.updated_at) THEN
  RAISE EXCEPTION 'immutable verified anchor';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_verified_anchor BEFORE UPDATE ON evidence_anchor_state FOR EACH ROW EXECUTE FUNCTION gramstack_immutable_verified_anchor();
