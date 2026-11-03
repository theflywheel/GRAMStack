package authn

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/discovery"
)

func TestActualAdapterDiscovery(t *testing.T) {
	if os.Getenv("GRAMSTACK_TEST_DISCOVERY") != "1" {
		t.Skip("requires configured public adapter metadata")
	}
	base := os.Getenv("GRAMSTACK_TEST_ADAPTER_PUBLIC")
	client := &http.Client{Timeout: 5 * time.Second}
	before := sourceReads(t)
	request, _ := http.NewRequest("GET", base+discovery.ManifestPath, nil)
	response, body := liveResponse(t, client, request)
	var manifest discovery.Manifest
	if response.StatusCode != 200 || json.Unmarshal([]byte(body), &manifest) != nil || manifest.AdapterID != "lokos-adapter" || manifest.Registry != "lokos" || manifest.Audience != "lokos-adapter" || manifest.ContractVersion != "0.3.0" || manifest.Authentication != "mtls" || !slices.Contains(manifest.VerificationMethods, "demo.registry-map-v1") || len(manifest.Capabilities) != 2 || len(manifest.Resources) != 1 || manifest.Resources[0].Version != "1" {
		t.Fatal("public configured capabilities missing", response.StatusCode)
	}
	if response.Header.Get(discovery.VersionHeader) != discovery.Version || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("discovery version/cache contract missing")
	}
	request, _ = http.NewRequest("GET", base+discovery.KeysPath, nil)
	response, body = liveResponse(t, client, request)
	var set jose.JSONWebKeySet
	if response.StatusCode != 200 || json.Unmarshal([]byte(body), &set) != nil || len(set.Keys) != 1 || set.Keys[0].KeyID != "demo-adapter-key" || !set.Keys[0].IsPublic() {
		t.Fatal("public key discovery failed", response.StatusCode)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("GRAMSTACK_TEST_CERTS"), "adapter-evidence.pub"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("approved key missing")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := set.Keys[0].Key.(*ecdsa.PublicKey)
	if !ok || !key.Equal(parsed) {
		t.Fatal("public key differs from approved descriptor")
	}
	for _, private := range []string{"12500.75", "member-12345", "9000000001", `"d"`, "authority_origin"} {
		if strings.Contains(body, private) {
			t.Fatal("public discovery leaked private configuration/data")
		}
	}
	request, _ = http.NewRequest("POST", base+"/internal/v1/retrievals", strings.NewReader("{}"))
	if response, _ = liveResponse(t, client, request); response.StatusCode != 404 {
		t.Fatal("public discovery listener exposed protected retrieval", response.StatusCode)
	}
	if sourceReads(t) != before {
		t.Fatal("discovery read native resource data")
	}
	t.Log("PASS: anonymous public manifest and public keys match the configured SDK, approved trust and resource contract; protected operations remain private")
}
