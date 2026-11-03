package consentmanager

import (
	"context"
	"testing"
)

func TestConsentLifecycle(t *testing.T) {
	state := "PENDING"
	for _, step := range []struct{ event, want string }{{"approve", "APPROVAL_RECORDED"}, {"activate", "ACTIVE"}, {"withdraw", "WITHDRAWN"}} {
		next, err := Transition(context.Background(), state, step.event)
		if err != nil || next != step.want {
			t.Fatalf("%s: %s %v", step.event, next, err)
		}
		state = next
	}
	for _, state := range []string{"PENDING", "REJECTED", "WITHDRAWN", "EXPIRED", "EXHAUSTED", "UNKNOWN"} {
		if _, err := Transition(context.Background(), state, "activate"); err == nil {
			t.Fatalf("activated from %s", state)
		}
	}
	for _, state := range []string{"REJECTED", "WITHDRAWN", "EXPIRED"} {
		for _, event := range []string{"approve", "activate", "reject", "withdraw", "expire"} {
			got, err := Transition(context.Background(), state, event)
			if err == nil || got != state {
				t.Fatalf("terminal state changed: %s %s", state, event)
			}
		}
	}
}

func TestWithdrawalAndCancellationBeforeActivation(t *testing.T) {
	for _, state := range []string{"APPROVAL_RECORDED", "ACTIVE"} {
		next, err := Transition(context.Background(), state, "withdraw")
		if err != nil || next != "WITHDRAWN" {
			t.Fatalf("%s: %s %v", state, next, err)
		}
	}
	for _, state := range []string{"PENDING", "APPROVAL_RECORDED", "ACTIVE"} {
		next, err := Transition(context.Background(), state, "cancel")
		if err != nil || next != "WITHDRAWN" {
			t.Fatalf("%s: %s %v", state, next, err)
		}
		if _, err = Transition(context.Background(), next, "activate"); err == nil {
			t.Fatal("cancelled consent activated")
		}
	}
}
