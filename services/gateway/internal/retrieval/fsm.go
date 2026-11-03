package retrieval

import (
	"context"
	"github.com/looplab/fsm"
)

func transition(ctx context.Context, state, event string) (string, error) {
	machine := fsm.NewFSM(state, fsm.Events{
		{Name: "release_started", Src: []string{"DISPATCHED"}, Dst: "RELEASE_ATTEMPTED"},
		{Name: "response_written", Src: []string{"RELEASE_ATTEMPTED"}, Dst: "COMPLETED"},
		{Name: "delivery_uncertain", Src: []string{"DISPATCHED", "RELEASE_ATTEMPTED"}, Dst: "DELIVERY_UNKNOWN"},
		{Name: "failed", Src: []string{"DISPATCHED"}, Dst: "FAILED"},
	}, nil)
	if err := machine.Event(ctx, event); err != nil {
		return "", ErrConflict
	}
	return machine.Current(), nil
}
