package conformance

import (
	"testing"

	"github.com/theflywheel/GRAMStack/sdk/go/adapter/internal/retrievalcheck"
)

// RetrievalFixture describes a freshly configured handler and synthetic
// requests with independently observed native read and custody counts.
type RetrievalFixture = retrievalcheck.Fixture

// CheckRetrieval exercises grant and service-identity rejection, exact
// projection with custody, and refusal to replay under either attempt ID.
func CheckRetrieval(t *testing.T, fixture RetrievalFixture) {
	t.Helper()
	retrievalcheck.Run(t, fixture)
}
