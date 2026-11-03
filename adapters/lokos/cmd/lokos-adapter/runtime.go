package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	lokos "github.com/theflywheel/GRAMStack/adapters/lokos"
	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

type adapterRuntime struct {
	db        *sql.DB
	server    *http.Server
	cert, key string
}

func configureAdapter(ctx context.Context, migrate bool) (*adapterRuntime, error) {
	if os.Getenv("ADAPTER_STATE_DATABASE_URL") == "" {
		if migrate {
			return nil, errors.New("adapter database configuration required")
		}
		return nil, nil
	}
	demo := os.Getenv("ADAPTER_DEMO_VERIFIER") == "true"
	mode := os.Getenv("GRAMSTACK_ENV")
	if demo && mode != "demo" && mode != "development" {
		return nil, errors.New("demo verifier is not permitted in production")
	}
	ca, err := os.ReadFile(os.Getenv("SERVICE_CA_FILE"))
	if err != nil {
		return nil, errors.New("service CA required")
	}
	certPath, keyPath := os.Getenv("SERVICE_CERT_FILE"), os.Getenv("SERVICE_KEY_FILE")
	cert, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := sdk.ServiceTLS(ca, cert, key)
	if err != nil {
		return nil, err
	}
	sourceClient, err := sdk.ServiceClient(ca, cert, key, os.Getenv("LOKOS_SOURCE_SERVER_NAME"))
	if err != nil {
		return nil, err
	}
	stateKey, err := base64.StdEncoding.DecodeString(os.Getenv("ADAPTER_STATE_KEY"))
	if err != nil {
		return nil, errors.New("adapter state key required")
	}
	db, err := sql.Open("pgx", os.Getenv("ADAPTER_STATE_DATABASE_URL"))
	if err != nil {
		return nil, errors.New("invalid adapter database")
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("adapter database unavailable")
	}
	store, err := sdk.NewSQLContextStore(db, stateKey)
	if err != nil {
		db.Close()
		return nil, err
	}
	attempts, err := sdk.NewSQLAttemptStore(db, "lokos-adapter", stateKey)
	if err != nil {
		db.Close()
		return nil, err
	}
	if migrate {
		err = store.Migrate(ctx)
		if err == nil {
			err = attempts.Migrate(ctx)
		}
		db.Close()
		return nil, err
	}
	sourceOrigin := os.Getenv("LOKOS_SOURCE_URL")
	sourceURL, parseErr := url.Parse(sourceOrigin)
	if parseErr != nil || sourceURL.Scheme != "https" || sourceURL.Host == "" || sourceURL.User != nil || sourceURL.RawQuery != "" || sourceURL.Fragment != "" || (sourceURL.Path != "" && sourceURL.Path != "/") {
		db.Close()
		return nil, errors.New("trusted HTTPS source origin required")
	}
	source := lokos.HTTPSource{Origin: sourceOrigin, Client: sourceClient, AllowDemo: demo}
	provider, err := lokos.New(source)
	if err != nil {
		db.Close()
		return nil, err
	}
	method := "registry.issuer-map-v1"
	if demo {
		method = "demo.registry-map-v1"
	}
	service, err := sdk.NewVerification(sdk.VerificationConfig{ContractVersion: sdk.VerificationVersion, Registry: "lokos", GatewayIdentity: "gateway.gramstack.internal", Method: method, Resource: provider.Resource(), Verifier: source, Store: store})
	if err != nil {
		db.Close()
		return nil, err
	}
	port := os.Getenv("ADAPTER_TLS_PORT")
	if port == "" {
		port = "8443"
	}
	return &adapterRuntime{db: db, server: &http.Server{Addr: ":" + port, Handler: service.Handler(), TLSConfig: tlsConfig, ReadHeaderTimeout: 5e9, ReadTimeout: 15e9, WriteTimeout: 15e9, IdleTimeout: 60e9}, cert: certPath, key: keyPath}, nil
}
