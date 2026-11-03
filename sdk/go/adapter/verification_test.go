package adapter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type contextMemory struct {
	mu     sync.Mutex
	values map[string]StoredContext
}

func (m *contextMemory) Find(_ context.Context, id, digest, principal string) (StoredContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.values {
		if v.RequestID == id && v.Context.RequestDigest == digest && v.Context.PrincipalContextRef == principal {
			return v, nil
		}
	}
	return StoredContext{}, ErrContextMissing
}
func (m *contextMemory) Resolve(_ context.Context, id string) (StoredContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[id]
	if !ok {
		return v, ErrContextMissing
	}
	return v, nil
}
func (m *contextMemory) Save(_ context.Context, v StoredContext) (StoredContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.values {
		if old.RequestID == v.RequestID && old.Context.RequestDigest == v.Context.RequestDigest && old.Context.PrincipalContextRef == v.Context.PrincipalContextRef {
			return old, nil
		}
	}
	m.values[v.Context.ContextID] = v
	return v, nil
}

type subjectVerifierFunc func(context.Context, PrincipalContext, Hint) (string, error)

func (f subjectVerifierFunc) VerifySubject(c context.Context, p PrincipalContext, h Hint) (string, error) {
	return f(c, p, h)
}
func verificationInput() VerificationRequest {
	now := time.Now().UTC().Truncate(time.Microsecond)
	snapshot := Snapshot{RequestID: "request-1", ConsumerID: "consumer-1", CreatedAt: now.Add(-time.Second), ExpiresAt: now.Add(4 * time.Minute), RecipientTermsDigest: "sha256:" + strings.Repeat("a", 64)}
	snapshot.Request.Registry = "lokos"
	snapshot.Request.Resource = "shg.membership"
	snapshot.Request.Fields = []string{"membership_status"}
	snapshot.Request.Purpose = "credit_eligibility"
	snapshot.Request.SubjectHint = Hint{"member_reference", "member-12345"}
	snapshot.Request.Access.Mode = "single_use"
	snapshot.Request.Access.ValidForSeconds = 300
	snapshot.Request.RecipientTerms.RetentionPolicyID = "terms-v1"
	digest, _ := SnapshotDigest(snapshot)
	return VerificationRequest{Snapshot: snapshot, RequestDigest: digest, Principal: PrincipalContext{ContextID: "principal-1", Issuer: "https://identity.example", ProviderSubject: "pairwise-sunita", AuthenticationTransactionID: "authentication-1", AuthenticatedAt: now.Add(-time.Second), ExpiresAt: now.Add(2 * time.Minute)}}
}
func TestVerificationBindingsAndIdempotence(t *testing.T) {
	store := &contextMemory{values: map[string]StoredContext{}}
	calls := 0
	service, err := NewVerification(VerificationConfig{ContractVersion: VerificationVersion, Registry: "lokos", GatewayIdentity: "gateway.example", Method: "registry.test-v1", Resource: Resource{Name: "shg.membership", Fields: []string{"membership_status", "savings_summary"}}, Store: store, Verifier: subjectVerifierFunc(func(_ context.Context, p PrincipalContext, h Hint) (string, error) {
		calls++
		if p.ProviderSubject != "pairwise-sunita" || h.Value != "member-12345" {
			return "", ErrVerification
		}
		return "native-map-1", nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	input := verificationInput()
	value, err := service.Verify(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if value.ContextID == "" || value.RequestDigest != input.RequestDigest || value.PrincipalContextRef != input.Principal.ContextID || value.SourceMappingRef != "native-map-1" || value.ExpiresAt.After(input.Principal.ExpiresAt) || value.ExpiresAt.After(input.Snapshot.ExpiresAt) {
		t.Fatal("context lost request/principal/expiry binding")
	}
	repeat, err := service.Verify(context.Background(), input)
	if err != nil || repeat != value || calls != 1 {
		t.Fatal("retry reran source verification or changed context", err, calls)
	}
	shorter := input
	shorter.Principal.ExpiresAt = time.Now().Add(time.Second)
	if _, err = service.Verify(context.Background(), shorter); !errors.Is(err, ErrVerification) {
		t.Fatal("cached context exceeded current authentication lifetime")
	}
	for _, tc := range []struct {
		name   string
		edit   func(*VerificationRequest)
		digest bool
	}{
		{"same reference with substituted person", func(v *VerificationRequest) { v.Principal.ProviderSubject = "pairwise-rani" }, false},
		{"different person", func(v *VerificationRequest) {
			v.Principal.ContextID = "principal-2"
			v.Principal.ProviderSubject = "pairwise-rani"
		}, false},
		{"wrong member", func(v *VerificationRequest) { v.Snapshot.Request.SubjectHint.Value = "member-67890" }, true},
		{"wrong digest", func(v *VerificationRequest) { v.RequestDigest = "sha256:" + strings.Repeat("b", 64) }, false},
		{"registry", func(v *VerificationRequest) { v.Snapshot.Request.Registry = "other" }, true},
		{"field", func(v *VerificationRequest) { v.Snapshot.Request.Fields = []string{"all_records"} }, true},
		{"duplicates", func(v *VerificationRequest) {
			v.Snapshot.Request.Fields = []string{"membership_status", "membership_status"}
		}, true},
		{"stale authentication", func(v *VerificationRequest) { v.Principal.AuthenticatedAt = time.Now().Add(-6 * time.Minute) }, false},
		{"expired principal", func(v *VerificationRequest) { v.Principal.ExpiresAt = time.Now().Add(-time.Second) }, false},
		{"expired request", func(v *VerificationRequest) { v.Snapshot.ExpiresAt = time.Now().Add(-time.Second) }, true},
		{"future authentication", func(v *VerificationRequest) { v.Principal.AuthenticatedAt = time.Now().Add(time.Hour) }, false},
		{"missing transaction", func(v *VerificationRequest) { v.Principal.AuthenticationTransactionID = "" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := input
			tc.edit(&v)
			if tc.digest {
				v.RequestDigest, _ = SnapshotDigest(v.Snapshot)
			}
			if _, err := service.Verify(context.Background(), v); !errors.Is(err, ErrVerification) {
				t.Fatal("invalid verification accepted", err)
			}
		})
	}
}
func TestDecodeRejectsAmbiguousInput(t *testing.T) {
	for _, raw := range []string{`{"request_digest":"one","request_digest":"two"}`, `{"unexpected":true}`, `{} {}`, `null`} {
		var value VerificationRequest
		err := Decode([]byte(raw), &value)
		if err == nil {
			t.Fatal("ambiguous transport input accepted")
		}
	}
}
