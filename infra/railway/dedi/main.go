// Run the pinned DeDi node behind a private TLS listener. Only its health
// endpoint is reachable on the separate Railway probe port.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	cert, err := tls.LoadX509KeyPair("/run/gramstack/dedi.pem", "/run/gramstack/dedi.key")
	if err != nil {
		return errors.New("DeDi TLS identity unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	upstream := &url.URL{Scheme: "http", Host: "127.0.0.1:18446"}
	if err := os.Setenv("DEDI_LISTEN", upstream.Host); err != nil {
		return err
	}
	node := exec.Command("/app/dedid", "serve")
	node.Stdout, node.Stderr = os.Stdout, os.Stderr
	if err := node.Start(); err != nil {
		return errors.New("DeDi process failed to start")
	}
	exited := make(chan error, 1)
	go func() { exited <- node.Wait() }()
	proxy := &httputil.ReverseProxy{
		Rewrite:   func(request *httputil.ProxyRequest) { request.SetURL(upstream) },
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, ResponseHeaderTimeout: 12 * time.Second},
	}
	health := http.NewServeMux()
	health.Handle("GET /healthz", proxy)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	probe := &http.Server{Addr: ":" + port, Handler: health, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 15 * time.Second}
	server := &http.Server{Addr: ":8446", Handler: proxy, TLSConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert},
	}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second}
	failed := make(chan error, 2)
	go func() { failed <- probe.ListenAndServe() }()
	go func() { failed <- server.ListenAndServeTLS("", "") }()
	nodeExited := false
	select {
	case <-ctx.Done():
	case <-failed:
		err = errors.New("DeDi listener failed")
	case <-exited:
		nodeExited = true
		err = errors.New("DeDi node exited")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	_ = probe.Shutdown(shutdown)
	if !nodeExited {
		_ = node.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(11 * time.Second):
			_ = node.Process.Kill()
			<-exited
		}
	}
	return err
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
