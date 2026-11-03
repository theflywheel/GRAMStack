package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

func boundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("retrieval configuration file unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errors.New("retrieval configuration file too large")
	}
	return raw, nil
}
func configureRetrieval(mux *http.ServeMux, contexts *sdk.SQLContextStore, attempts *sdk.SQLAttemptStore, provider sdk.Provider, method string, ca, cert, key []byte, mode string) error {
	path := os.Getenv("ADAPTER_RETRIEVAL_CONFIG_FILE")
	if path == "" {
		return nil
	}
	raw, err := boundedFile(path)
	if err != nil {
		return err
	}
	var c struct {
		Issuer   string `json:"grant_issuer"`
		Audience string `json:"grant_audience"`
		Keys     []struct {
			KeyID string `json:"key_id"`
			File  string `json:"file"`
		} `json:"grant_keys"`
		Authority      string `json:"authority_origin"`
		AuthorityPeer  string `json:"authority_service_identity"`
		EvidenceKeyID  string `json:"evidence_key_id"`
		ReleaseProfile string `json:"release_profile"`
		MaxConcurrent  int    `json:"max_concurrent"`
	}
	if sdk.Decode(raw, &c) != nil {
		return errors.New("invalid retrieval configuration")
	}
	keys := map[string]*ecdsa.PublicKey{}
	for _, entry := range c.Keys {
		if _, ok := keys[entry.KeyID]; ok {
			return errors.New("duplicate grant key")
		}
		raw, err := boundedFile(entry.File)
		if err != nil {
			return err
		}
		block, rest := pem.Decode(raw)
		if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
			return errors.New("grant public key required")
		}
		value, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return errors.New("invalid grant public key")
		}
		public, ok := value.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("ECDSA grant public key required")
		}
		keys[entry.KeyID] = public
	}
	verifier, err := sdk.NewGrantVerifier(c.Issuer, c.Audience, keys, mode == "development")
	if err != nil {
		return err
	}
	raw, err = boundedFile(os.Getenv("ADAPTER_EVIDENCE_SIGNING_KEY_FILE"))
	if err != nil {
		return err
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		return errors.New("PKCS8 evidence private key required")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("invalid evidence private key")
	}
	signing, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return errors.New("ECDSA evidence key required")
	}
	serviceCert, _ := pem.Decode(cert)
	if serviceCert == nil {
		return errors.New("service certificate required")
	}
	leaf, err := x509.ParseCertificate(serviceCert.Bytes)
	if err != nil || signing.PublicKey.Equal(leaf.PublicKey) {
		return errors.New("evidence signing and service TLS keys must be distinct")
	}
	for _, grantKey := range keys {
		if signing.PublicKey.Equal(grantKey) {
			return errors.New("grant and evidence signing keys must be distinct")
		}
	}
	signer, err := sdk.NewEvidenceSigner(c.EvidenceKeyID, signing)
	if err != nil {
		return err
	}
	client, err := sdk.ServiceClient(ca, cert, key, c.AuthorityPeer)
	if err != nil {
		return err
	}
	authority, err := sdk.NewAuthorityClient(c.Authority, client)
	if err != nil {
		return err
	}
	service, err := sdk.NewRetrieval(sdk.RetrievalConfig{Registry: "lokos", AdapterID: "lokos-adapter", GatewayIdentity: "gateway.gramstack.internal", VerificationMethod: method, Provider: provider, Grants: verifier, Contexts: contexts, Attempts: attempts, Authority: authority, Custodian: authority, Signer: signer, SigningKeys: map[string]*ecdsa.PublicKey{c.EvidenceKeyID: &signing.PublicKey}, ReleaseProfile: c.ReleaseProfile, MaxConcurrent: c.MaxConcurrent})
	if err != nil {
		return err
	}
	mux.Handle("POST /internal/v1/retrievals", service.Handler())
	return nil
}
