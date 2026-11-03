// Package consentmanager owns the human-consent lifecycle. Disclosure usage
// remains separate and must be changed transactionally with redemption.
package consentmanager

import (
	"context"
	"fmt"
	"github.com/looplab/fsm"
)

func Transition(ctx context.Context, current, event string) (string, error) {
	machine := fsm.NewFSM(current, fsm.Events{
		{Name: "approve", Src: []string{"PENDING"}, Dst: "APPROVAL_RECORDED"},
		{Name: "activate", Src: []string{"APPROVAL_RECORDED"}, Dst: "ACTIVE"},
		{Name: "reject", Src: []string{"PENDING"}, Dst: "REJECTED"},
		{Name: "withdraw", Src: []string{"APPROVAL_RECORDED", "ACTIVE"}, Dst: "WITHDRAWN"},
		{Name: "cancel", Src: []string{"PENDING", "APPROVAL_RECORDED", "ACTIVE"}, Dst: "WITHDRAWN"},
		{Name: "expire", Src: []string{"PENDING", "APPROVAL_RECORDED", "ACTIVE"}, Dst: "EXPIRED"},
	}, nil)
	if err := machine.Event(ctx, event); err != nil {
		return current, fmt.Errorf("consent transition: %w", err)
	}
	return machine.Current(), nil
}
