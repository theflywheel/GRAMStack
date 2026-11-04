package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gowebpki/jcs"
	"github.com/jackc/pgx/v5/pgxpool"
	oidcadapter "github.com/theflywheel/GRAMStack/adapters/auth/oidc"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/authn"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/subjectbinding"
	"github.com/theflywheel/GRAMStack/services/gateway/migrations"
)

// Consumer APIs are explicitly configured; the default shell exposes no protected
// endpoints. Migrations remain a separate operator command, never startup DDL.
func configureConsumers(ctx context.Context, mux *http.ServeMux) (func(), error) {
	path := os.Getenv("GRAMSTACK_CONFIG_FILE")
	if path == "" {
		registerReadiness(mux, nil)
		return func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read gateway configuration")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 131073))
	if err != nil || len(raw) > 131072 {
		return nil, errors.New("invalid gateway configuration size")
	}
	raw, err = jcs.Transform(raw)
	if err != nil {
		return nil, errors.New("invalid gateway configuration JSON")
	}
	var config struct {
		PublicOrigin          string                       `json:"public_origin"`
		HydraIntrospectionURL string                       `json:"hydra_introspection_url"`
		HydraIssuer           string                       `json:"hydra_issuer"`
		Audience              string                       `json:"audience"`
		Scope                 string                       `json:"scope"`
		AllowLoopbackHTTP     bool                         `json:"allow_loopback_http"`
		Consumers             map[string][]consumer.Policy `json:"consumers"`
		Registries            []subjectbinding.Registry    `json:"registries"`
		Consent               *consentConfig               `json:"consent"`
		Authorization         *authorizationConfig         `json:"authorization"`
		Authentication        *struct {
			ClientID     string              `json:"client_id"`
			HydraAdmin   string              `json:"hydra_admin_url"`
			Provider     oidcadapter.Profile `json:"provider"`
			DemoUpstream bool                `json:"demo_upstream"`
		} `json:"authentication"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&config); err != nil {
		return nil, errors.New("invalid gateway configuration fields")
	}
	if config.AllowLoopbackHTTP && os.Getenv("GRAMSTACK_ENV") != "development" {
		return nil, errors.New("loopback HTTP is development-only")
	}
	if c := config.Authentication; c != nil && c.DemoUpstream && os.Getenv("GRAMSTACK_ENV") != "development" && os.Getenv("GRAMSTACK_ENV") != "demo" {
		return nil, errors.New("fictional identity provider requires explicit development/demo environment")
	}
	if config.Authorization != nil && config.Consent == nil {
		return nil, errors.New("authorization requires configured consent authority")
	}
	if config.Consent != nil && (config.Authentication == nil || len(config.Registries) == 0) {
		return nil, errors.New("consent portal requires authentication and registry verification")
	}
	for _, registry := range config.Registries {
		if registry.Demo && os.Getenv("GRAMSTACK_ENV") != "development" && os.Getenv("GRAMSTACK_ENV") != "demo" {
			return nil, errors.New("demo registry verification is forbidden in production")
		}
	}
	auth, err := consumer.NewHydra(config.HydraIntrospectionURL, config.HydraIssuer, config.Audience, config.Scope, config.AllowLoopbackHTTP)
	if err != nil {
		return nil, err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required for consumer APIs")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = pool.Ping(probe); err != nil {
		pool.Close()
		return nil, errors.New("database unavailable")
	}
	if err = migrations.Check(probe, pool); err != nil {
		pool.Close()
		return nil, errors.New("gateway migrations required or changed")
	}
	service, err := consumer.New(consumer.Config{Pool: pool, Auth: auth, PublicOrigin: config.PublicOrigin, AllowLoopback: config.AllowLoopbackHTTP, Consumers: config.Consumers})
	if err != nil {
		pool.Close()
		return nil, err
	}
	closeWorkers := func() {}
	handler := service.Handler()
	mux.Handle("/v1/access-requests", handler)
	mux.Handle("/v1/access-requests/", handler)
	if c := config.Authentication; c != nil {
		key, err := base64.StdEncoding.DecodeString(os.Getenv("GRAMSTACK_AUTH_KEY"))
		if err != nil {
			pool.Close()
			return nil, errors.New("invalid authentication encryption key")
		}
		store, err := authn.NewStore(pool, key)
		if err != nil {
			pool.Close()
			return nil, err
		}
		credentials := oidcadapter.Credentials{ClientSecret: os.Getenv("GRAMSTACK_UPSTREAM_CLIENT_SECRET")}
		if path := os.Getenv("GRAMSTACK_UPSTREAM_PRIVATE_KEY_FILE"); path != "" {
			file, err := os.Open(path)
			if err != nil {
				pool.Close()
				return nil, errors.New("cannot read upstream client assertion key")
			}
			credentials.PrivateKeyPEM, err = io.ReadAll(io.LimitReader(file, 16385))
			closeErr := file.Close()
			if err != nil || closeErr != nil || len(credentials.PrivateKeyPEM) > 16384 {
				pool.Close()
				return nil, errors.New("invalid upstream assertion key size")
			}
		}
		federation, err := authn.New(ctx, authn.Config{Pool: pool, Store: store, Origin: config.PublicOrigin, HydraAdmin: c.HydraAdmin, AllowLoopback: config.AllowLoopbackHTTP, VerificationEnabled: len(config.Registries) > 0, ConsentPortalEnabled: config.Consent != nil, Hydra: authn.ProviderConfig{Issuer: config.HydraIssuer, ClientID: c.ClientID, ClientSecret: os.Getenv("GRAMSTACK_PORTAL_CLIENT_SECRET")}, UpstreamProfile: c.Provider, UpstreamCredentials: credentials, AllowDemoUpstream: c.DemoUpstream})
		if err != nil {
			pool.Close()
			return nil, err
		}
		if len(config.Registries) > 0 {
			ca, err := os.ReadFile(os.Getenv("SERVICE_CA_FILE"))
			if err != nil {
				pool.Close()
				return nil, errors.New("registry CA required")
			}
			certificate, err := os.ReadFile(os.Getenv("SERVICE_CERT_FILE"))
			if err != nil {
				pool.Close()
				return nil, err
			}
			key, err := os.ReadFile(os.Getenv("SERVICE_KEY_FILE"))
			if err != nil {
				pool.Close()
				return nil, err
			}
			mode := os.Getenv("GRAMSTACK_ENV")
			binding, err := subjectbinding.New(subjectbinding.Config{ConsentPortalEnabled: config.Consent != nil, Pool: pool, Auth: federation, Store: store, Registries: config.Registries, CA: ca, Certificate: certificate, Key: key, AllowDemo: mode == "development" || mode == "demo"})
			if err != nil {
				pool.Close()
				return nil, err
			}
			mux.Handle("POST /interactions/{id}/verify", binding.Handler())
			if config.Consent != nil {
				if closeWorkers, err = configureConsentPortal(ctx, mux, pool, federation, binding, service, config.Registries, config.PublicOrigin, config.AllowLoopbackHTTP, *config.Consent, config.Authorization, auth); err != nil {
					pool.Close()
					return nil, err
				}
			}
		}
		authHandler := federation.Handler()
		mux.Handle("/auth/", authHandler)
		mux.Handle("/interactions/", authHandler)
	}
	registerReadiness(mux, func(ctx context.Context) error { return migrations.Check(ctx, pool) })
	return func() { closeWorkers(); pool.Close() }, nil
}

func registerReadiness(mux *http.ServeMux, check func(context.Context) error) {
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if check == nil || check(ctx) != nil {
			http.Error(w, "application dependencies unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ready\n"))
	})
}
