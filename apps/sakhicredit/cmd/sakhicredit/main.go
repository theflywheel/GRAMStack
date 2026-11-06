package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/apps/sakhicredit/internal/application"
	gramstack "github.com/theflywheel/GRAMStack/sdk/go/client"
)

type config struct {
	LoginIssuer       string `json:"login_issuer,omitempty"`
	LoginClientID     string `json:"login_client_id,omitempty"`
	PublicOrigin      string `json:"public_origin"`
	GatewayOrigin     string `json:"gateway_origin"`
	GatewayDataOrigin string `json:"gateway_data_origin"`
	TokenURL          string `json:"hydra_token_url"`
	ClientID          string `json:"client_id"`
	DataServerName    string `json:"data_server_name"`
	AllowLoopback     bool   `json:"allow_loopback_http"`
}

func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("configured product file unavailable")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errors.New("invalid product file")
	}
	return raw, nil
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var login *gramstack.AuthClient
	var service *application.Service
	var store *application.Store
	var pool *pgxpool.Pool
	var c config
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "migrate" {
			return errors.New("usage: sakhicredit [migrate]")
		}
		if os.Getenv("SAKHI_DATABASE_URL") == "" {
			return errors.New("product database URL required")
		}
		p, err := pgxpool.New(ctx, os.Getenv("SAKHI_DATABASE_URL"))
		if err != nil {
			return errors.New("product database configuration invalid")
		}
		defer p.Close()
		if err := application.Migrate(ctx, p); err != nil {
			return errors.New("product migration failed")
		}
		log.Print("SakhiCredit migrations applied")
		return nil
	}
	if path := os.Getenv("SAKHI_CONFIG_FILE"); path != "" {
		raw, err := readFile(path)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF {
			return errors.New("invalid product configuration")
		}
		if c.PublicOrigin == "" {
			return errors.New("product public origin required")
		}
		if c.AllowLoopback && os.Getenv("SAKHI_ENV") != "development" {
			return errors.New("loopback mode requires explicit development environment")
		}
		if os.Getenv("SAKHI_DATABASE_URL") == "" {
			return errors.New("product database URL required")
		}
		poolConfig, err := pgxpool.ParseConfig(os.Getenv("SAKHI_DATABASE_URL"))
		if err != nil {
			return errors.New("product database configuration invalid")
		}
		poolConfig.MaxConns = 8
		poolConfig.ConnConfig.ConnectTimeout = 10 * time.Second
		pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			return errors.New("product database unavailable")
		}
		defer pool.Close()
		storageKey, err := readFile(os.Getenv("SAKHI_STORAGE_KEY_FILE"))
		if err != nil {
			return err
		}
		store, err = application.NewStore(pool, storageKey)
		if err != nil {
			return err
		}
		readyCtx, cancelReady := context.WithTimeout(ctx, 10*time.Second)
		readyErr := store.Ready(readyCtx)
		cancelReady()
		if readyErr != nil {
			return errors.New("product database/migration unavailable")
		}
		ca, err := readFile(os.Getenv("SAKHI_SERVICE_CA_FILE"))
		if err != nil {
			return err
		}
		cert, err := readFile(os.Getenv("SAKHI_CONSUMER_CERT_FILE"))
		if err != nil {
			return err
		}
		key, err := readFile(os.Getenv("SAKHI_CONSUMER_KEY_FILE"))
		if err != nil {
			return err
		}
		client, err := application.NewClient(application.ClientConfig{PublicOrigin: c.GatewayOrigin, DataOrigin: c.GatewayDataOrigin, TokenURL: c.TokenURL, ClientID: c.ClientID, ClientSecret: os.Getenv("SAKHI_CLIENT_SECRET"), DataServerName: c.DataServerName, CA: ca, Certificate: cert, Key: key, AllowLoopback: c.AllowLoopback})
		if err != nil {
			return err
		}
		if c.LoginClientID != "" || c.LoginIssuer != "" {
			login, err = gramstack.NewAuth(ctx, gramstack.AuthConfig{Issuer: c.LoginIssuer, ClientID: c.LoginClientID, ClientSecret: os.Getenv("SAKHI_LOGIN_CLIENT_SECRET"), RedirectURL: c.PublicOrigin + "/auth/gramstack/callback", CA: ca, AllowLoopback: c.AllowLoopback}, store)
			if err != nil {
				return errors.New("GRAMStack application login configuration unavailable")
			}
		}
		service, err = application.NewService(store, client)
		if err != nil {
			return err
		}
	}
	if c.PublicOrigin == "" {
		c.PublicOrigin = "http://127.0.0.1"
		c.AllowLoopback = true
	}
	handler, err := application.Handler(service, c.PublicOrigin, c.AllowLoopback, login)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", handler)
	if directory := os.Getenv("GRAMSTACK_SWAGGER_DIR"); directory != "" {
		mux.Handle("GET /swagger/", http.StripPrefix("/swagger/", http.FileServer(http.Dir(directory))))
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	server := &http.Server{Addr: net.JoinHostPort(os.Getenv("BIND_HOST"), port), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if store == nil {
			return
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if err := store.Maintain(workerCtx); err != nil && workerCtx.Err() == nil {
					log.Print("product maintenance deferred")
				}
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- server.ListenAndServe() }()
	select {
	case err := <-exited:
		cancelWorker()
		<-workerDone
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("product HTTP listener failed")
		}
		return nil
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if server.Shutdown(shutdown) != nil {
		server.Close()
	}
	cancelWorker()
	<-workerDone
	return nil
}
func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
