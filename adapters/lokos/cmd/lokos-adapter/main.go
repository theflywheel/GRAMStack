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
	if len(os.Args) > 1 && os.Args[1] != "migrate" {
		log.Fatal("usage: lokos-adapter [migrate]")
	}
	setup, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	runtime, err := configureAdapter(setup, len(os.Args) > 1)
	cancelSetup()
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 {
		return
	}
	if runtime != nil {
		defer runtime.db.Close()
		go func() {
			if err := runtime.server.ListenAndServeTLS(runtime.cert, runtime.key); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal("adapter TLS listener failed")
			}
		}()
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	if runtime != nil && runtime.metadata != nil {
		mux.Handle("GET /.well-known/gramstack-adapter", runtime.metadata)
		mux.Handle("GET /.well-known/gramstack-adapter/jwks.json", runtime.metadata)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if runtime == nil || runtime.ready(ctx) != nil {
			http.Error(w, "application dependencies unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ready\n"))
	})
	server := &http.Server{Addr: net.JoinHostPort(os.Getenv("BIND_HOST"), port), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if runtime != nil {
			_ = runtime.server.Shutdown(shutdown)
		}
		if err := server.Shutdown(shutdown); err != nil {
			log.Print(err)
		}
	}()
	log.Printf("listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
