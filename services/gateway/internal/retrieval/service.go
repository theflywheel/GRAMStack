// Package retrieval owns one dispatch and one consumer response for a disclosure.
// Registry source access stays behind the independently hosted adapter contract.
package retrieval

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gowebpki/jcs"
	"github.com/jackc/pgx/v5/pgxpool"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

var ErrConflict = errors.New("disclosure dispatch already exists or stopped")
var ErrDenied = errors.New("disclosure request binding denied")
var ErrDependency = errors.New("protected retrieval unavailable")
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

type Endpoint struct{ Registry, AdapterID, Audience, Origin, ServerName string }
type ReceiptVerifier interface {
	VerifyReceipt(context.Context, string, string, string, az.Grant) error
}
type Config struct {
	Pool                 *pgxpool.Pool
	Authority            *az.Service
	Receipts             ReceiptVerifier
	Endpoints            []Endpoint
	CA, Certificate, Key []byte
	MaxConcurrent        int
}
type endpoint struct {
	descriptor Endpoint
	client     *http.Client
}
type Service struct {
	pool      *pgxpool.Pool
	authority *az.Service
	receipts  ReceiptVerifier
	endpoints map[string]endpoint
	slots     chan struct{}
}
type request struct {
	AttemptID string       `json:"attempt_id"`
	Snapshot  cm.Snapshot  `json:"snapshot"`
	Digest    string       `json:"request_digest"`
	Grant     string       `json:"grant"`
	Presenter az.Presenter `json:"presenter"`
}
type response struct {
	RequestID string                     `json:"request_id"`
	Data      map[string]json.RawMessage `json:"data"`
	ReceiptID string                     `json:"receipt_id"`
}

func New(c Config) (*Service, error) {
	if c.Pool == nil || c.Authority == nil || c.Receipts == nil || len(c.Endpoints) == 0 || c.MaxConcurrent < 1 || c.MaxConcurrent > 64 {
		return nil, errors.New("retrieval dependencies and bounded concurrency required")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(c.CA) {
		return nil, ErrDenied
	}
	pair, err := tls.X509KeyPair(c.Certificate, c.Key)
	if err != nil {
		return nil, ErrDenied
	}
	s := &Service{c.Pool, c.Authority, c.Receipts, map[string]endpoint{}, make(chan struct{}, c.MaxConcurrent)}
	for _, e := range c.Endpoints {
		u, err := url.Parse(e.Origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !identifier.MatchString(e.Registry) || !identifier.MatchString(e.AdapterID) || !identifier.MatchString(e.Audience) || e.ServerName == "" {
			return nil, ErrDenied
		}
		if _, ok := s.endpoints[e.Registry]; ok {
			return nil, ErrDenied
		}
		e.Origin = strings.TrimSuffix(e.Origin, "/")
		client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}, ServerName: e.ServerName}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		s.endpoints[e.Registry] = endpoint{e, client}
	}
	return s, nil
}
func decode(raw []byte, target any) error {
	if _, err := jcs.Transform(raw); err != nil {
		return ErrDependency
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return ErrDependency
	}
	return nil
}
func (s *Service) fetch(ctx context.Context, e endpoint, input request) (response, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return response{}, ErrDependency
	}
	r, err := http.NewRequestWithContext(ctx, "POST", e.descriptor.Origin+"/internal/v1/retrievals", bytes.NewReader(raw))
	if err != nil {
		return response{}, ErrDependency
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Gramstack-Contract-Version", "0.3.0")
	result, err := e.client.Do(r)
	if err != nil {
		return response{}, ErrDependency
	}
	defer result.Body.Close()
	media, _, mediaErr := mime.ParseMediaType(result.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" || result.Header.Get("Gramstack-Contract-Version") != "0.3.0" || result.Header.Get("Cache-Control") != "no-store" {
		return response{}, ErrDependency
	}
	if result.StatusCode != 200 {
		switch result.StatusCode {
		case 403:
			return response{}, ErrDenied
		case 409:
			return response{}, ErrConflict
		case 410:
			return response{}, az.ErrExpired
		default:
			return response{}, ErrDependency
		}
	}
	raw, err = io.ReadAll(io.LimitReader(result.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return response{}, ErrDependency
	}
	var out response
	if decode(raw, &out) != nil || out.RequestID != input.Snapshot.RequestID || !identifier.MatchString(out.ReceiptID) || len(out.Data) != len(input.Snapshot.Request.Fields) {
		return response{}, ErrDependency
	}
	for _, field := range input.Snapshot.Request.Fields {
		value, ok := out.Data[field]
		if !ok || !json.Valid(value) {
			return response{}, ErrDependency
		}
	}
	return out, nil
}
