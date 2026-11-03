package main

import (
	"context"
	"encoding/json"
	source "github.com/theflywheel/GRAMStack/mocks/lokos"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mode := os.Getenv("GRAMSTACK_ENV")
	if mode != "development" && mode != "demo" {
		log.Fatal("fictional source requires development/demo mode")
	}
	raw, err := os.ReadFile(os.Getenv("OIDC_SUBJECT_BINDINGS_FILE"))
	if err != nil {
		log.Fatal("fictional subject binding file required")
	}
	var bindings map[string]string
	if len(raw) > 16384 || json.Unmarshal(raw, &bindings) != nil {
		log.Fatal("invalid fictional subject bindings")
	}
	native, err := source.New(source.Config{Issuer: os.Getenv("OIDC_ISSUER"), AdapterIdentity: "lokos-adapter.gramstack.internal", DemoEnabled: true, GateDirectory: os.Getenv("MOCK_VERIFICATION_GATE_DIRECTORY"), ReadGateDirectory: os.Getenv("MOCK_READ_GATE_DIRECTORY"), Bindings: bindings})
	if err != nil {
		log.Fatal(err)
	}
	ca, err := os.ReadFile(os.Getenv("SERVICE_CA_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	certFile, keyFile := os.Getenv("SERVICE_CERT_FILE"), os.Getenv("SERVICE_KEY_FILE")
	cert, err := os.ReadFile(certFile)
	if err != nil {
		log.Fatal(err)
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		log.Fatal(err)
	}
	tls, err := source.TLS(ca, cert, key)
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("SOURCE_TLS_PORT")
	if port == "" {
		port = "8444"
	}
	server := &http.Server{Addr: ":" + port, Handler: native.Handler(), TLSConfig: tls, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
