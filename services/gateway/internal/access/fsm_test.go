package access

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestGraphMatchesSpec(t *testing.T) {
	raw, err := os.ReadFile("testdata/lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Transitions map[string][]string `json:"transitions"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	for from, destinations := range spec.Transitions {
		observed := []string{}
		for _, event := range events {
			next, err := Transition(context.Background(), from, event.Name)
			allowed := slices.Contains(event.Src, from)
			if (err == nil) != allowed {
				t.Fatalf("%s + %s: %v", from, event.Name, err)
			}
			if allowed {
				observed = append(observed, next)
			} else if next != from {
				t.Fatal("rejected transition changed state")
			}
		}
		slices.Sort(observed)
		slices.Sort(destinations)
		if !slices.Equal(observed, destinations) {
			t.Fatalf("%s: got %v want %v", from, observed, destinations)
		}
	}
}

func TestCanceledTransition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state, err := Transition(ctx, "AUTHENTICATION_PENDING", "authenticated")
	if err == nil || state != "AUTHENTICATION_PENDING" {
		t.Fatalf("state=%s error=%v", state, err)
	}
}
