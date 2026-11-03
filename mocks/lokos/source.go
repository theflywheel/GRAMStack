// Package lokosmock supplies fictional native records and an independently
// configured identity-to-record verification policy. It is not the LokOS API.
package lokosmock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gowebpki/jcs"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	GateDirectory           string
	Issuer, AdapterIdentity string
	DemoEnabled             bool
	Bindings                map[string]string
}
type Source struct{ config Config }
type Principal struct {
	Methods         []string  `json:"methods,omitempty"`
	Assurance       string    `json:"assurance,omitempty"`
	ContextID       string    `json:"context_id"`
	Issuer          string    `json:"issuer"`
	Subject         string    `json:"provider_subject"`
	Transaction     string    `json:"authentication_transaction_id"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

func New(c Config) (*Source, error) {
	if !c.DemoEnabled || c.Issuer == "" || c.AdapterIdentity == "" {
		return nil, errors.New("explicit fictional source configuration required")
	}
	if len(c.Bindings) == 0 {
		return nil, errors.New("explicit fictional subject bindings required")
	}
	bindings := map[string]string{}
	for member, subject := range c.Bindings {
		if (member != "member-12345" && member != "member-67890") || subject == "" || len(subject) > 1024 {
			return nil, errors.New("invalid fictional subject binding")
		}
		bindings[member] = subject
	}
	c.Bindings = bindings
	return &Source{c}, nil
}
func TLS(ca, cert, key []byte) (*tls.Config, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("service CA required")
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}
func (s *Source) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /native/v1/verify-subject", s.verify)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /native/v1/members/{id}", s.member)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].VerifyHostname(s.config.AdapterIdentity) != nil {
			http.Error(w, "Untrusted service", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Source) waitGate(ctx context.Context) error {
	if s.config.GateDirectory == "" {
		return nil
	}
	present := func(name string) (bool, error) {
		_, err := os.Stat(filepath.Join(s.config.GateDirectory, name))
		if os.IsNotExist(err) {
			return false, nil
		}
		return err == nil, err
	}
	unavailable, err := present("unavailable")
	if err != nil || unavailable {
		return errors.New("fixture source unavailable")
	}
	enabled, err := present("enabled")
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	if err = os.WriteFile(filepath.Join(s.config.GateDirectory, "entered"), []byte("ready"), 0600); err != nil {
		return err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		released, err := present("release")
		if err != nil {
			return err
		}
		if released {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Source) verify(w http.ResponseWriter, r *http.Request) {
	if err := s.waitGate(r.Context()); err != nil {
		http.Error(w, "Fictional source unavailable", 503)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil {
		http.Error(w, "Invalid verification", 400)
		return
	}
	var input struct {
		Principal Principal `json:"principal"`
		Hint      struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"hint"`
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		http.Error(w, "Invalid verification", 400)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "Invalid verification", 400)
		return
	}
	expected := s.config.Bindings[input.Hint.Value]
	p := input.Principal
	if expected == "" || input.Hint.Type != "member_reference" || p.Issuer != s.config.Issuer || p.Subject != expected || p.ContextID == "" || p.Transaction == "" || !time.Now().Before(p.ExpiresAt) || p.AuthenticatedAt.IsZero() || time.Since(p.AuthenticatedAt) > 5*time.Minute || p.AuthenticatedAt.After(time.Now().Add(30*time.Second)) {
		http.Error(w, "Verification unsuccessful", 403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"source_mapping_ref": s.mappingReference(input.Hint.Value), "demo": true})
}
func (s *Source) mappingReference(member string) string {
	digest := sha256.Sum256([]byte(s.config.Issuer + "\x00" + s.config.Bindings[member] + "\x00" + member))
	return "native_" + hex.EncodeToString(digest[:16])
}
func (s *Source) member(w http.ResponseWriter, r *http.Request) {
	member := ""
	for id := range s.config.Bindings {
		if s.mappingReference(id) == r.PathValue("id") {
			member = id
			break
		}
	}
	switch member {
	case "member-12345":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"membership_status": "active", "savings_rupees": json.Number("12500.75"), "demo": true})
	case "member-67890":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"membership_status": "active", "savings_rupees": json.Number("8400.00"), "demo": true})
	default:
		http.Error(w, "Record unavailable", 404)
	}
}
