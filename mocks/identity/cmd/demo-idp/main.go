package main

import (
	"context"
	identity "github.com/theflywheel/GRAMStack/mocks/identity"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := os.Getenv("GRAMSTACK_ENV")
	if mode != "development" && mode != "demo" {
		log.Fatal("fictional identity provider requires development or demo mode")
	}
	var formReturns []string
	if raw := os.Getenv("OIDC_FORM_RETURN_ORIGINS"); raw != "" {
		formReturns = strings.Split(raw, ",")
	}
	provider, err := identity.New(identity.Config{Issuer: os.Getenv("OIDC_ISSUER"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), RedirectURI: os.Getenv("OIDC_REDIRECT_URI"), DemoEnabled: true, FormReturnOrigins: formReturns})
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8091"
	}
	server := &http.Server{Addr: ":" + port, Handler: provider.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
