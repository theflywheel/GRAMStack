package consentmanager_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

func TestPresentedDecisionBindingAndReadOnlyRecord(t *testing.T) {
	f := setup(t)
	config := f.config
	config.RequirePresentation = true
	service, err := cm.New(config)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := service.Create(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	digest := cm.Digest([]byte("isolated-session-one"))
	presentation, err := service.Present(context.Background(), notice.ConsentRequestID, f.principal, digest, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := service.Present(context.Background(), notice.ConsentRequestID, f.principal, digest, time.Now().Add(time.Minute))
	if err != nil || repeat != presentation {
		t.Fatal("reloading changed session-bound presentation", err)
	}
	record, err := service.Record(context.Background(), notice.ConsentRequestID, f.principal)
	if err != nil || (record.Notice.NoticeDigest != notice.NoticeDigest || record.Notice.Text != notice.Text || record.Notice.NoticeVersion != notice.NoticeVersion || record.Notice.Language != notice.Language || !record.Notice.ExpiresAt.Equal(notice.ExpiresAt)) || record.Decision != nil || record.SignedArtefact != "" || record.Consent.Status != "PENDING" {
		t.Fatal("read-only record created authority or changed exact notice", err)
	}
	if _, err = service.Record(context.Background(), notice.ConsentRequestID, "other-principal"); !errors.Is(err, cm.ErrForbidden) {
		t.Fatal("receipt ownership bypassed")
	}
	approve := decision(notice, "approve")
	approve.PresentationID = presentation.ID
	approve.SessionDigest = digest
	approve.ExplicitConfirmation = true
	for _, tc := range []struct {
		name string
		edit func(*cm.Decision)
	}{
		{"missing presentation", func(d *cm.Decision) { d.PresentationID = "" }},
		{"session substitution", func(d *cm.Decision) { d.SessionDigest = cm.Digest([]byte("another-session")) }},
		{"implicit approval", func(d *cm.Decision) { d.ExplicitConfirmation = false }},
		{"notice substitution", func(d *cm.Decision) { d.NoticeDigest = cm.Digest([]byte("other notice")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := approve
			tc.edit(&d)
			if _, err := service.Decide(context.Background(), notice.ConsentRequestID, f.principal, d); !errors.Is(err, cm.ErrBinding) {
				t.Fatal("unbound decision accepted", err)
			}
		})
	}
	state, err := service.Decide(context.Background(), notice.ConsentRequestID, f.principal, approve)
	if err != nil || state.Status != "APPROVAL_RECORDED" {
		t.Fatal("valid explicit decision rejected", err)
	}
	again, err := service.Decide(context.Background(), notice.ConsentRequestID, f.principal, approve)
	if err != nil || again.ConsentID != state.ConsentID || again.Status != state.Status || again.StatusVersion != state.StatusVersion || again.Use != state.Use || !again.ValidUntil.Equal(state.ValidUntil) {
		t.Fatal("identical decision replay changed authority", err)
	}
	record, err = service.Record(context.Background(), notice.ConsentRequestID, f.principal)
	if err != nil || record.Decision == nil || record.Decision.Presentation == nil || record.Decision.Presentation.ID != presentation.ID || !record.Decision.ExplicitConfirmation || record.SignedArtefact != "" {
		t.Fatal("receipt lost decision provenance or activated implicitly", err)
	}
	var count int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM consent_decision_presentations WHERE consent_id=$1`, notice.ConsentRequestID).Scan(&count); err != nil || count != 1 {
		t.Fatal("decision presentation duplicated", err, count)
	}
	if _, err = f.pool.Exec(context.Background(), `UPDATE consent_presentations SET language='hi' WHERE presentation_id=$1`, presentation.ID); err == nil {
		t.Fatal("rendered notice evidence was mutable")
	}
}
func TestPresentationExpiresAndRefusesUnsupportedSession(t *testing.T) {
	f := setup(t)
	config := f.config
	config.RequirePresentation = true
	service, err := cm.New(config)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := service.Create(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Present(context.Background(), notice.ConsentRequestID, "other", cm.Digest([]byte("session")), time.Now().Add(time.Minute)); !errors.Is(err, cm.ErrForbidden) {
		t.Fatal("wrong principal rendered a notice")
	}
	if _, err = service.Present(context.Background(), notice.ConsentRequestID, f.principal, cm.Digest([]byte("session")), time.Now().Add(-time.Second)); !errors.Is(err, cm.ErrExpired) {
		t.Fatal("expired session created usable presentation")
	}
}

func TestPresentedDecisionRollbackAndExpiry(t *testing.T) {
	f := setup(t)
	config := f.config
	config.RequirePresentation = true
	service, err := cm.New(config)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := service.Create(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	digest := cm.Digest([]byte("rollback-session"))
	presentation, err := service.Present(context.Background(), notice.ConsentRequestID, f.principal, digest, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	approve := decision(notice, "approve")
	approve.PresentationID, approve.SessionDigest, approve.ExplicitConfirmation = presentation.ID, digest, true
	// A conflicting outbox version forces a rollback after both evidence inserts.
	if _, err = f.pool.Exec(context.Background(), `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES($1,'consent',$2,2,'test.injected','{}')`, rand.Text(), notice.ConsentRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Decide(context.Background(), notice.ConsentRequestID, f.principal, approve); err == nil {
		t.Fatal("decision ignored outbox failure")
	}
	record, err := service.Record(context.Background(), notice.ConsentRequestID, f.principal)
	if err != nil || record.Consent.Status != "PENDING" || record.Decision != nil {
		t.Fatal("failed decision left authority or provenance", err)
	}
	var links int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM consent_decision_presentations WHERE consent_id=$1`, notice.ConsentRequestID).Scan(&links); err != nil || links != 0 {
		t.Fatal("presentation link escaped rollback", err)
	}
	if _, err = f.pool.Exec(context.Background(), `DELETE FROM outbox WHERE aggregate_id=$1`, notice.ConsentRequestID); err != nil {
		t.Fatal(err)
	}
	// Advance database time past a genuinely stored presentation deadline.
	expired, err := service.Present(context.Background(), notice.ConsentRequestID, f.principal, cm.Digest([]byte("short-session")), time.Now().Add(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(context.Background(), `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())))+0.001)`, expired.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	approve.PresentationID, approve.SessionDigest = expired.ID, cm.Digest([]byte("short-session"))
	if _, err = service.Decide(context.Background(), notice.ConsentRequestID, f.principal, approve); !errors.Is(err, cm.ErrBinding) {
		t.Fatal("expired presentation authorised decision", err)
	}
	approve.PresentationID, approve.SessionDigest = presentation.ID, digest
	if _, err = service.Decide(context.Background(), notice.ConsentRequestID, f.principal, approve); err != nil {
		t.Fatal("valid retry could not recover after rollback", err)
	}
}
