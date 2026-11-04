package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		switch os.Args[1] {
		case "anchor":
			err = anchor(ctx, os.Args[2:], os.Stdout)
		case "verify-anchors":
			err = verifyAnchors(ctx, os.Args[2:], os.Stdout)
		case "sign-policy":
			err = signPolicy(ctx, os.Args[2:], os.Stdout)
		case "migrate":
			err = migrate(ctx, os.Args[2:], os.Stdout)
		default:
			err = errors.New("usage: gramstack [anchor flags | verify-anchors | migrate | sign-policy flags]")
		}
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	closeConsumers, err := configureConsumers(ctx, mux)
	if err != nil {
		log.Fatal(err)
	}
	defer closeConsumers()
	server := &http.Server{Addr: net.JoinHostPort(os.Getenv("BIND_HOST"), port), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Print(err)
		}
	}()
	log.Printf("listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
