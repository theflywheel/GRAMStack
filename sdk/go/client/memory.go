package gramstack

import (
	"context"
	"crypto/subtle"
	"sync"
	"time"
)

type storedLogin struct {
	binding     string
	transaction LoginTransaction
}

// MemoryLoginStore is an explicit single-process development store.
// Production applications supply shared encrypted TTL storage through LoginStore.
type MemoryLoginStore struct {
	mu      sync.Mutex
	entries map[string]storedLogin
}

func NewMemoryLoginStore() *MemoryLoginStore {
	return &MemoryLoginStore{entries: make(map[string]storedLogin)}
}
func (s *MemoryLoginStore) SaveLogin(ctx context.Context, state, binding string, value LoginTransaction) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.entries {
		if time.Now().After(entry.transaction.ExpiresAt) {
			delete(s.entries, key)
		}
	}
	if len(s.entries) >= 4096 {
		return ErrAuthentication
	}
	if _, exists := s.entries[state]; exists {
		return ErrAuthentication
	}
	s.entries[state] = storedLogin{binding, value}
	return nil
}
func (s *MemoryLoginStore) TakeLogin(ctx context.Context, state, binding string) (LoginTransaction, error) {
	if ctx.Err() != nil {
		return LoginTransaction{}, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.entries[state]
	if !ok || subtle.ConstantTimeCompare([]byte(value.binding), []byte(binding)) != 1 || time.Now().After(value.transaction.ExpiresAt) {
		return LoginTransaction{}, ErrAuthentication
	}
	delete(s.entries, state)
	return value.transaction, nil
}
