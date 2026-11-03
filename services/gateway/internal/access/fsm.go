// Package access defines request-lifecycle decisions. Persistence must commit
// each decision with an expected-version check; the FSM is not a database lock.
package access

import (
	"context"
	"fmt"
	"github.com/looplab/fsm"
)

var events = fsm.Events{
	{Name: "authenticated", Src: []string{"AUTHENTICATION_PENDING"}, Dst: "BINDING_PENDING"},
	{Name: "subject_verified", Src: []string{"BINDING_PENDING"}, Dst: "CONSENT_PENDING"},
	{Name: "consent_activated", Src: []string{"CONSENT_PENDING"}, Dst: "CONSENTED"},
	{Name: "grant_issued", Src: []string{"CONSENTED"}, Dst: "AUTHORIZED"},
	{Name: "retrieval_started", Src: []string{"AUTHORIZED"}, Dst: "REDEEMING"},
	{Name: "evidence_committed", Src: []string{"REDEEMING"}, Dst: "EVIDENCE_COMMITTED"},
	{Name: "release_started", Src: []string{"EVIDENCE_COMMITTED"}, Dst: "RELEASE_ATTEMPTED"},
	{Name: "response_written", Src: []string{"RELEASE_ATTEMPTED"}, Dst: "COMPLETED"},
	{Name: "delivery_uncertain", Src: []string{"REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED"}, Dst: "DELIVERY_UNKNOWN"},
	{Name: "rejected", Src: []string{"CONSENT_PENDING"}, Dst: "REJECTED"},
	{Name: "denied", Src: []string{"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING"}, Dst: "DENIED"},
	{Name: "withdrawn", Src: []string{"CONSENT_PENDING", "CONSENTED", "AUTHORIZED"}, Dst: "WITHDRAWN"},
	{Name: "cancelled", Src: []string{"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED"}, Dst: "CANCELLED"},
	{Name: "expired", Src: []string{"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED"}, Dst: "EXPIRED"},
	{Name: "failed", Src: []string{"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "REDEEMING", "EVIDENCE_COMMITTED"}, Dst: "FAILED"},
}

// Transition evaluates an event without mutating a persisted request.
func Transition(ctx context.Context, current, event string) (string, error) {
	machine := fsm.NewFSM(current, events, nil)
	if err := machine.Event(ctx, event); err != nil {
		return current, fmt.Errorf("request transition: %w", err)
	}
	return machine.Current(), nil
}
