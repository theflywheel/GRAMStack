package authn

import (
	"strings"
	"testing"
)

func TestHistoryDigestScopes(t *testing.T) {
	pool, key := database(t)
	store, err := NewStore(pool, key)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{Issuer: "https://identity.example", Subject: "private-subject", Reference: "request-one"}
	original := store.historyDigest("client-one", "registry-one", principal)
	principal.Reference = "request-two"
	restarted, err := NewStore(pool, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(original, principal.Subject) || restarted.historyDigest("client-one", "registry-one", principal) != original ||
		restarted.historyDigest("client-two", "registry-one", principal) == original ||
		restarted.historyDigest("client-one", "registry-two", principal) == original {
		t.Fatal("history lookup must be opaque, stable across requests and isolated per provider client and registry")
	}
	principal.Issuer = "https://other.example"
	if restarted.historyDigest("client-one", "registry-one", principal) == original {
		t.Fatal("matching subjects from different issuers shared history")
	}
}
