package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/custody"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/subjectbinding"
)

type authorizationConfig struct {
	KeyID         string `json:"key_id"`
	Issuer        string `json:"issuer"`
	ListenAddress string `json:"listen_address"`
	PolicyKeys    []struct {
		Registry string `json:"registry"`
		KeyID    string `json:"key_id"`
		File     string `json:"file"`
	} `json:"policy_keys"`
	Policies []struct {
		File     string `json:"file"`
		Expected string `json:"expected_version"`
	} `json:"policies"`
	Adapters []struct {
		Registry     string `json:"registry"`
		Audience     string `json:"audience"`
		Peer         string `json:"service_identity"`
		ID           string `json:"adapter_id"`
		EvidenceKeys []struct {
			KeyID string `json:"key_id"`
			File  string `json:"file"`
		} `json:"evidence_keys"`
	} `json:"adapters"`
	Presenters map[string]string `json:"presenter_certificates"`
	Evidence   *struct {
		ReleaseProfile string `json:"release_profile"`
	} `json:"evidence"`
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("configured authorization file unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("invalid authorization file size")
	}
	return raw, nil
}
func publicKey(path string) (*ecdsa.PublicKey, error) {
	raw, err := readBounded(path, 16384)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("public policy key PEM required")
	}
	value, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid policy public key")
	}
	key, ok := value.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("ECDSA policy public key required")
	}
	return key, nil
}
func configureAuthorization(ctx context.Context, mux *http.ServeMux, pool *pgxpool.Pool, auth consumer.Authenticator, subjects *subjectbinding.Service, terms *consumer.Service, consents *cm.Service, verifier *cm.Verifier, origin string, loopback bool, c authorizationConfig) (func(), error) {
	if c.Issuer == "" {
		c.Issuer = origin
	}
	if c.ListenAddress == "" {
		return nil, errors.New("private authorization listen address required")
	}
	path := os.Getenv("GRAMSTACK_GRANT_SIGNING_KEY_FILE")
	if path == os.Getenv("GRAMSTACK_CONSENT_SIGNING_KEY_FILE") || path == os.Getenv("SERVICE_KEY_FILE") {
		return nil, errors.New("grant, consent and service keys must be separate")
	}
	key, err := signingKey(path)
	if err != nil {
		return nil, err
	}
	consentKey, err := signingKey(os.Getenv("GRAMSTACK_CONSENT_SIGNING_KEY_FILE"))
	if err != nil {
		return nil, err
	}
	if key.PublicKey.Equal(&consentKey.PublicKey) {
		return nil, errors.New("grant and consent signing keys must be distinct")
	}
	signer, err := az.NewGrantSigner(c.KeyID, key)
	if err != nil {
		return nil, err
	}
	keys := map[string]map[string]*ecdsa.PublicKey{}
	for _, entry := range c.PolicyKeys {
		if keys[entry.Registry] == nil {
			keys[entry.Registry] = map[string]*ecdsa.PublicKey{}
		}
		if keys[entry.Registry][entry.KeyID] != nil {
			return nil, errors.New("duplicate source policy key")
		}
		keys[entry.Registry][entry.KeyID], err = publicKey(entry.File)
		if err != nil {
			return nil, err
		}
	}
	ca, err := readBounded(os.Getenv("SERVICE_CA_FILE"), 65536)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("authorization service CA required")
	}
	pair, err := tls.LoadX509KeyPair(os.Getenv("SERVICE_CERT_FILE"), os.Getenv("SERVICE_KEY_FILE"))
	if err != nil {
		return nil, errors.New("authorization service TLS keypair required")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || key.PublicKey.Equal(leaf.PublicKey) {
		return nil, errors.New("grant signing and service TLS keys must be distinct")
	}
	presenters := map[string]string{}
	for client, path := range c.Presenters {
		raw, err := readBounded(path, 16384)
		if err != nil {
			return nil, err
		}
		block, rest := pem.Decode(raw)
		if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
			return nil, errors.New("presenter leaf certificate required")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid presenter certificate")
		}
		if _, err = cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
			return nil, errors.New("untrusted presenter certificate")
		}
		sum := sha256.Sum256(cert.Raw)
		presenters[client] = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	adapters := map[string]az.Adapter{}
	for _, a := range c.Adapters {
		if _, ok := adapters[a.Registry]; ok {
			return nil, errors.New("duplicate authorization registry")
		}
		adapters[a.Registry] = az.Adapter{Audience: a.Audience, PeerName: a.Peer}
	}
	service, err := az.New(az.Config{Pool: pool, Subjects: subjects, Terms: terms, Consents: verifier, Signer: signer, GrantKeys: map[string]*ecdsa.PublicKey{c.KeyID: &key.PublicKey}, PolicyKeys: keys, Adapters: adapters, Presenters: presenters, Issuer: c.Issuer, AllowLoopback: loopback})
	if err != nil {
		return nil, err
	}
	privateHandler := http.NewServeMux()
	privateHandler.Handle("/", service.InternalHandler(consents))
	if c.Evidence != nil {
		entries := []custody.Adapter{}
		for _, a := range c.Adapters {
			entry := custody.Adapter{ID: a.ID, Audience: a.Audience, PeerName: a.Peer, Keys: map[string]*ecdsa.PublicKey{}}
			for _, k := range a.EvidenceKeys {
				if _, exists := entry.Keys[k.KeyID]; exists {
					return nil, errors.New("duplicate adapter evidence key")
				}
				entry.Keys[k.KeyID], err = publicKey(k.File)
				if err != nil {
					return nil, err
				}
			}
			entries = append(entries, entry)
		}
		custodian, err := custody.New(custody.Config{Pool: pool, Scopes: service, Adapters: entries, ReleaseProfile: c.Evidence.ReleaseProfile})
		if err != nil {
			return nil, err
		}
		privateHandler.Handle("POST /internal/v1/evidence", custodian.Handler())
	}
	listener, err := net.Listen("tcp", c.ListenAddress)
	if err != nil {
		return nil, errors.New("authorization private listener unavailable")
	}
	for _, entry := range c.Policies {
		raw, err := readBounded(entry.File, 128*1024)
		if err == nil {
			err = service.InstallPolicy(ctx, strings.TrimSpace(string(raw)), entry.Expected)
		}
		if err != nil {
			listener.Close()
			return nil, err
		}
	}
	public := service.PublicHandler(auth)
	mux.Handle("POST /v1/access-requests/{id}/grant", public)
	mux.Handle("GET /.well-known/gramstack-jwks.json", public)
	server := &http.Server{Handler: privateHandler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(tls.NewListener(listener, server.TLSConfig)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Print("authorization private listener stopped")
		}
	}()
	return func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if server.Shutdown(shutdown) != nil {
			server.Close()
		}
		<-done
	}, nil
}
