package adapter_test

import (
	"net/http"
	"testing"

	"github.com/theflywheel/GRAMStack/sdk/go/adapter"
	"github.com/theflywheel/GRAMStack/sdk/go/adapter/conformance"
)

func TestAuthorityClientPinsContractAndRejectsRedirects(t *testing.T) {
	conformance.CheckAuthorityClient(t, func(origin string, client *http.Client) (conformance.AuthorityClient, error) {
		return adapter.NewAuthorityClient(origin, client)
	})
}
