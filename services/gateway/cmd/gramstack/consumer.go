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
	"github.com/theflywheel/GRAMStack/services/gateway/internal/authn"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/consumer"
)

// Consumer APIs are explicitly configured; the default shell exposes no protected
// endpoints. Migrations remain a separate operator command, never startup DDL.
func configureConsumers(ctx context.Context, mux *http.ServeMux) (func(), error) {
	path := os.Getenv("GRAMSTACK_CONFIG_FILE")
	if path == "" {
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
		Authentication        *struct {
			ClientID         string `json:"client_id"`
			HydraAdmin       string `json:"hydra_admin_url"`
			UpstreamIssuer   string `json:"upstream_issuer"`
			UpstreamClientID string `json:"upstream_client_id"`
			DemoUpstream     bool   `json:"demo_upstream"`
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
	service, err := consumer.New(consumer.Config{Pool: pool, Auth: auth, PublicOrigin: config.PublicOrigin, AllowLoopback: config.AllowLoopbackHTTP, Consumers: config.Consumers})
	if err != nil {
		pool.Close()
		return nil, err
	}
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
		federation, err := authn.New(ctx, authn.Config{Pool: pool, Store: store, Origin: config.PublicOrigin, HydraAdmin: c.HydraAdmin, AllowLoopback: config.AllowLoopbackHTTP, Hydra: authn.ProviderConfig{Issuer: config.HydraIssuer, ClientID: c.ClientID, ClientSecret: os.Getenv("GRAMSTACK_PORTAL_CLIENT_SECRET")}, Upstream: authn.ProviderConfig{Issuer: c.UpstreamIssuer, ClientID: c.UpstreamClientID, ClientSecret: os.Getenv("GRAMSTACK_UPSTREAM_CLIENT_SECRET"), AllowDemo: c.DemoUpstream}})
		if err != nil {
			pool.Close()
			return nil, err
		}
		authHandler := federation.Handler()
		mux.Handle("/auth/", authHandler)
		mux.Handle("/interactions/", authHandler)
	}
	return pool.Close, nil
}
