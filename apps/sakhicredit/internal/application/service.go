package application

import (
	"context"
	"errors"
	"time"
)

type Service struct {
	store  *Store
	client *Client
}

func NewService(store *Store, client *Client) (*Service, error) {
	if store == nil || client == nil {
		return nil, errors.New("product store and platform client required")
	}
	return &Service{store, client}, nil
}
func (s *Service) Get(ctx context.Context, owner, id string) (Application, error) {
	r, err := s.store.get(ctx, owner, id)
	if err != nil {
		return Application{}, err
	}
	return s.store.view(r)
}
func (s *Service) Create(ctx context.Context, owner, key string, input Input) (Application, error) {
	r, err := s.store.create(ctx, owner, key, input)
	if err != nil {
		return Application{}, err
	}
	return s.connect(ctx, r)
}
func (s *Service) connect(ctx context.Context, r record) (Application, error) {
	if r.State != "CONNECTING" {
		return s.store.view(r)
	}
	input, err := s.store.input(r)
	if err != nil {
		return Application{}, err
	}
	accepted, err := s.client.Create(ctx, r.ID, input)
	if err != nil {
		var remote *RemoteError
		if errors.As(err, &remote) && (remote.Status == 400 || remote.Status == 403 || remote.Status == 409) {
			if failure := s.store.claim(ctx, r.owner, r.ID, "BLOCKED"); failure != nil {
				return Application{}, failure
			}
			app, failure := s.Get(ctx, r.owner, r.ID)
			if failure != nil {
				return Application{}, failure
			}
			return app, err
		}
		return r.Application, err
	}
	if err = s.store.attach(ctx, r.owner, r.ID, accepted); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, r.owner, r.ID)
}
func (s *Service) Refresh(ctx context.Context, owner, id string) (Application, error) {
	r, err := s.store.get(ctx, owner, id)
	if err != nil {
		return Application{}, err
	}
	if r.State == "CONNECTING" {
		return s.connect(ctx, r)
	}
	if r.State == "CANCELLING" {
		return s.Cancel(ctx, owner, id)
	}
	if r.State != "WAITING" && r.State != "READY" {
		return s.store.view(r)
	}
	status, err := s.client.Status(ctx, r.RequestID, false)
	if err != nil {
		return r.Application, err
	}
	if err = s.store.observe(ctx, owner, id, status); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, owner, id)
}
func (s *Service) Retrieve(ctx context.Context, owner, id string) (Application, error) {
	r, err := s.store.get(ctx, owner, id)
	if err != nil {
		return Application{}, err
	}
	if r.State == "COMPLETE" {
		return s.store.view(r)
	}
	if r.State != "READY" {
		return r.Application, ErrConflict
	}
	input, err := s.store.input(r)
	if err != nil {
		return Application{}, err
	}
	// Grant retries are idempotent and precede the local one-use fetch claim.
	grant, err := s.client.grant(ctx, r.RequestID, r.ID)
	if err != nil {
		var remote *RemoteError
		if errors.As(err, &remote) && (remote.Status == 403 || remote.Status == 410) {
			target := "BLOCKED"
			if remote.Status == 410 {
				target = "EXPIRED"
			}
			if failure := s.store.claim(ctx, owner, id, target); failure != nil {
				return Application{}, failure
			}
			app, failure := s.Get(ctx, owner, id)
			if failure != nil {
				return Application{}, failure
			}
			return app, err
		}
		return r.Application, err
	}
	if err = s.store.claim(ctx, owner, id, "FETCHING"); err != nil {
		return r.Application, err
	}
	result, fetchErr := s.client.fetch(ctx, r.RequestID, grant, input)
	// Complete a durable checkpoint even if the browser disconnected meanwhile.
	checkpoint, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var value *Result
	if fetchErr == nil {
		value = &result
	}
	if err = s.store.finish(checkpoint, owner, id, value); err != nil {
		return Application{}, ErrUnavailable
	}
	app, err := s.Get(checkpoint, owner, id)
	if err != nil {
		return Application{}, err
	}
	// A failed/ambiguous fetch is terminal for this application, never retried.
	return app, nil
}
func (s *Service) Cancel(ctx context.Context, owner, id string) (Application, error) {
	r, err := s.store.get(ctx, owner, id)
	if err != nil {
		return Application{}, err
	}
	if r.State == "CANCELLED" {
		return r.Application, nil
	}
	if r.State != "CANCELLING" {
		if err = s.store.claim(ctx, owner, id, "CANCELLING"); err != nil {
			return r.Application, err
		}
		r.State = "CANCELLING" // Report the committed fence even when the upstream call fails.
	}
	status, err := s.client.Status(ctx, r.RequestID, true)
	var remote *RemoteError
	if errors.As(err, &remote) && remote.Status == 409 {
		// Resolve rejection/expiry or a passed admission cutoff without fetching.
		status, err = s.client.Status(ctx, r.RequestID, false)
	}
	if err != nil {
		return r.Application, err
	}
	if err = s.store.observe(ctx, owner, id, status); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, owner, id)
}
