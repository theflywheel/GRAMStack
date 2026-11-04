package lokos

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
	"github.com/theflywheel/GRAMStack/sdk/go/adapter/conformance"
)

func TestLiveDiscoveryConformance(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_DISCOVERY") != "1" {
		t.Skip("requires a running LokOS adapter")
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GRAMSTACK_TEST_CERTS"), "adapter-evidence.pub"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("missing evidence public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	public, ok := key.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("expected ECDSA evidence key")
	}
	conformance.CheckDiscovery(t, os.Getenv("GRAMSTACK_TEST_ADAPTER_PUBLIC"), sdk.Manifest{
		AdapterID: "lokos-adapter", Registry: "lokos", Audience: "lokos-adapter",
		ContractVersion: sdk.CoreVersion, Authentication: "mtls",
		SigningKeyIDs:       []string{"demo-adapter-key"},
		VerificationMethods: []string{"demo.registry-map-v1"},
		Capabilities:        []string{"atomic-redemption", "evidence-before-release"},
		Resources:           []sdk.Resource{{Name: "shg.membership", Version: "1", Fields: []string{"membership_status", "savings_summary"}}},
	}, map[string]*ecdsa.PublicKey{"demo-adapter-key": public})
}
