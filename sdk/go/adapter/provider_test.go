package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type source struct {
	calls int
	data  Data
}

func (*source) Resource() Resource {
	return Resource{Name: "resource", Version: "1", Fields: []string{"allowed", "other"}}
}
func (s *source) Fetch(context.Context, Subject, []string) (Data, error) {
	s.calls++
	return s.data, nil
}
func TestFieldBoundary(t *testing.T) {
	for _, fields := range [][]string{nil, {"unknown"}, {"allowed", "allowed"}} {
		s := &source{}
		_, err := ReadSelected(context.Background(), s, Subject{"subject"}, fields)
		if !errors.Is(err, ErrFields) || s.calls != 0 {
			t.Fatalf("%v: %v calls=%d", fields, err, s.calls)
		}
	}
	s := &source{data: Data{"allowed": json.RawMessage(`true`), "secret": json.RawMessage(`"private"`)}}
	got, err := ReadSelected(context.Background(), s, Subject{"subject"}, []string{"allowed"})
	if err != nil || len(got) != 1 || string(got["allowed"]) != "true" {
		t.Fatalf("%v %v", got, err)
	}
	got["allowed"][0] = 'x'
	if string(s.data["allowed"]) != "true" {
		t.Fatal("response aliases source buffer")
	}
	if _, err := ReadSelected(context.Background(), s, Subject{"subject"}, []string{"other"}); err == nil {
		t.Fatal("missing source field accepted")
	}
	if _, err := ReadSelected(context.Background(), s, Subject{}, []string{"allowed"}); err == nil {
		t.Fatal("empty subject accepted")
	}
}

func TestCancellationBeforeSourceRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &source{}
	_, err := ReadSelected(ctx, s, Subject{Reference: "verified"}, []string{"allowed"})
	if !errors.Is(err, context.Canceled) || s.calls != 0 {
		t.Fatalf("%v source calls=%d", err, s.calls)
	}
}
