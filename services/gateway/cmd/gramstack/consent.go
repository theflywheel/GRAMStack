package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/authn"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consentportal"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consentworker"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/subjectbinding"
)

type consentConfig struct {
	WorkersEnabled bool   `json:"workers_enabled"`
	KeyID          string `json:"key_id"`
	Issuer         string `json:"issuer"`
	StatusOrigin   string `json:"status_origin"`
}

func consentKey(path string) (*ecdsa.PrivateKey, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("consent signing key file required")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errors.New("invalid consent signing key size")
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("invalid consent signing key PEM")
	}
	var key *ecdsa.PrivateKey
	if block.Type == "EC PRIVATE KEY" {
		key, err = x509.ParseECPrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = parsed.(*ecdsa.PrivateKey)
	} else {
		return nil, errors.New("P-256 consent signing key required")
	}
	if err != nil || key == nil {
		return nil, errors.New("invalid consent signing key")
	}
	return key, nil
}
func configureConsentPortal(ctx context.Context, mux *http.ServeMux, pool *pgxpool.Pool, auth *authn.Service, bindings *subjectbinding.Service, terms *consumer.Service, origin string, loopback bool, c consentConfig) (func(), error) {
	key, err := consentKey(os.Getenv("GRAMSTACK_CONSENT_SIGNING_KEY_FILE"))
	if err != nil {
		return nil, err
	}
	signer, err := cm.NewES256Signer(c.KeyID, key)
	if err != nil {
		return nil, err
	}
	if c.Issuer == "" {
		c.Issuer = origin
	}
	if c.StatusOrigin == "" {
		c.StatusOrigin = origin
	}
	verifier, err := cm.NewVerifierWithOptions(c.Issuer, c.StatusOrigin, map[string]*ecdsa.PublicKey{c.KeyID: &key.PublicKey}, cm.VerifierOptions{AllowLoopback: loopback})
	if err != nil {
		return nil, err
	}
	service, err := cm.New(cm.Config{Pool: pool, Subjects: bindings, Terms: terms, Signer: signer, Verifier: verifier, InteractionOrigin: origin, AllowLoopback: loopback, RequirePresentation: true})
	if err != nil {
		return nil, err
	}
	portal, err := consentportal.New(consentportal.Config{Pool: pool, Auth: auth, Subjects: bindings, Consents: service, Origin: origin})
	if err != nil {
		return nil, err
	}
	handler := portal.Handler()
	for _, pattern := range []string{"GET /interactions/{id}/consent", "POST /interactions/{id}/decision", "GET /interactions/{id}/receipt", "POST /consents/{id}/withdrawal", "GET /assets/consent.css"} {
		mux.Handle(pattern, handler)
	}
	if !c.WorkersEnabled {
		return func() {}, nil
	}
	worker, err := consentworker.New(pool, service)
	if err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(workerCtx) }()
	return func() { cancel(); <-done }, nil
}
