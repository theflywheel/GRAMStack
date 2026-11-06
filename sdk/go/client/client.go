// Package gramstack integrates applications with GRAMStack core 0.3.0.
package gramstack

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/gowebpki/jcs"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrInput = errors.New("invalid GRAMStack client input")
var ErrUnavailable = errors.New("GRAMStack dependency unavailable or invalid response")
var ErrConsumed = errors.New("retrieval already attempted; inspect request status before requesting new consent")
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

type Config struct {
	PublicOrigin, DataOrigin, TokenURL, ClientID, ClientSecret, DataServerName string
	CA, Certificate, Key                                                       []byte
	AllowLoopback                                                              bool
}
type Client struct {
	config          Config
	public, private *http.Client
	mu              sync.Mutex
	token           string
	tokenUntil      time.Time
}
type RemoteError struct{ Status int }

func (e *RemoteError) Error() string {
	return "GRAMStack operation failed (HTTP " + strconv.Itoa(e.Status) + ")"
}

type SubjectHint struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type Access struct {
	Mode            string `json:"mode"`
	ValidForSeconds int64  `json:"valid_for_seconds"`
}
type RecipientTerms struct {
	RetentionPolicyID string `json:"retention_policy_id"`
}
type AccessRequest struct {
	Registry       string         `json:"registry"`
	Resource       string         `json:"resource"`
	SubjectHint    SubjectHint    `json:"subject_hint"`
	Fields         []string       `json:"fields"`
	Purpose        string         `json:"purpose"`
	Access         Access         `json:"access"`
	RecipientTerms RecipientTerms `json:"recipient_terms"`
}
type Accepted struct {
	RequestID      string    `json:"request_id"`
	Status         string    `json:"status"`
	StatusURL      string    `json:"status_url"`
	InteractionURL string    `json:"interaction_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}
type Status struct {
	RequestID  string    `json:"request_id"`
	Status     string    `json:"status"`
	Version    int64     `json:"version"`
	NextAction string    `json:"next_action"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Grant is an opaque one-attempt capability owned by the client that issued it.
type Grant struct {
	ExpiresAt time.Time
	token     string
	requestID string
	client    *Client
	used      *atomic.Bool
}

func (g Grant) String() string   { return "GRAMStack retrieval grant (redacted)" }
func (g Grant) GoString() string { return g.String() }

type Result struct {
	RequestID string                     `json:"request_id"`
	Data      map[string]json.RawMessage `json:"data"`
	ReceiptID string                     `json:"receipt_id"`
}

func trustedURL(raw string, loopback bool, origin bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (origin && u.Path != "") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return loopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()
}
func New(c Config) (*Client, error) {
	if !trustedURL(c.PublicOrigin, c.AllowLoopback, true) || !trustedURL(c.TokenURL, c.AllowLoopback, false) || !idPattern.MatchString(c.ClientID) || c.ClientSecret == "" {
		return nil, errors.New("trusted platform origins, consumer credentials and data identity required")
	}
	publicRoots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if len(c.CA) > 0 && !publicRoots.AppendCertsFromPEM(c.CA) {
		return nil, ErrInput
	}
	noRedirect := func(*http.Request, []*http.Request) error { return errors.New("platform redirects forbidden") }
	public := &http.Client{Timeout: 10 * time.Second, CheckRedirect: noRedirect, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: publicRoots}}}
	var private *http.Client
	if c.DataOrigin != "" {
		if !trustedURL(c.DataOrigin, false, true) || c.DataServerName == "" {
			return nil, ErrInput
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(c.CA) {
			return nil, errors.New("platform CA required")
		}
		pair, err := tls.X509KeyPair(c.Certificate, c.Key)
		if err != nil {
			return nil, errors.New("consumer TLS keypair required")
		}
		// A fresh connection prevents net/http from replaying the one-use GET.
		private = &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect, Transport: &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: c.DataServerName, Certificates: []tls.Certificate{pair}}}}
	}
	c.CA = nil
	c.Certificate = nil
	c.Key = nil
	return &Client{config: c, public: public, private: private}, nil
}

func decode(raw []byte, out any) error {
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrInput
	}
	// Reject field-name aliases and missing required booleans/types at boundaries.
	again, err := json.Marshal(out)
	if err != nil {
		return err
	}
	again, err = jcs.Transform(again)
	if err != nil || !bytes.Equal(canonical, again) {
		return ErrInput
	}
	return nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Until(c.tokenUntil) > 10*time.Second {
		return c.token, nil
	}
	body := url.Values{"grant_type": {"client_credentials"}, "scope": {"gramstack.access"}, "audience": {"gramstack"}}
	r, err := http.NewRequestWithContext(ctx, "POST", c.config.TokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", ErrUnavailable
	}
	r.SetBasicAuth(url.QueryEscape(c.config.ClientID), url.QueryEscape(c.config.ClientSecret))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.public.Do(r)
	if err != nil {
		return "", ErrUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 || response.StatusCode != 200 {
		return "", ErrUnavailable
	}
	// Hydra may add OAuth extension fields; only required standard claims are used.
	var value struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(raw, &value) != nil || len(value.AccessToken) == 0 || len(value.AccessToken) > 32768 || !strings.EqualFold(value.TokenType, "bearer") || value.ExpiresIn < 1 || value.ExpiresIn > 86400 {
		return "", ErrUnavailable
	}
	c.token = value.AccessToken
	c.tokenUntil = time.Now().Add(time.Duration(value.ExpiresIn) * time.Second)
	return c.token, nil
}
func (c *Client) call(ctx context.Context, method, path, key string, body []byte, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, method, c.config.PublicOrigin+path, bytes.NewReader(body))
	if err != nil {
		return ErrInput
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Gramstack-Contract-Version", "0.3.0")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	err = perform(c.public, r, out)
	var remote *RemoteError
	if errors.As(err, &remote) && remote.Status == 401 {
		c.mu.Lock()
		if c.token == token {
			c.token = ""
			c.tokenUntil = time.Time{}
		}
		c.mu.Unlock()
	}
	return err
}
func perform(client *http.Client, r *http.Request, out any) error {
	response, err := client.Do(r)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &RemoteError{Status: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 || response.Header.Get("Gramstack-Contract-Version") != "0.3.0" || decode(raw, out) != nil {
		return ErrUnavailable
	}
	return nil
}

func validFields(fields []string) bool {
	if len(fields) < 1 || len(fields) > 128 {
		return false
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if !idPattern.MatchString(field) || seen[field] {
			return false
		}
		seen[field] = true
	}
	return true
}
func (c *Client) Create(ctx context.Context, key string, input AccessRequest) (Accepted, error) {
	var value Accepted
	if !idPattern.MatchString(key) || !idPattern.MatchString(input.Registry) || !idPattern.MatchString(input.Resource) || !idPattern.MatchString(input.Purpose) || !idPattern.MatchString(input.SubjectHint.Type) || input.SubjectHint.Value == "" || len(input.SubjectHint.Value) > 1024 || !validFields(input.Fields) || input.Access.Mode != "single_use" || input.Access.ValidForSeconds < 1 || input.Access.ValidForSeconds > 300 || !idPattern.MatchString(input.RecipientTerms.RetentionPolicyID) {
		return value, ErrInput
	}
	body, err := json.Marshal(input)
	if err != nil {
		return value, ErrInput
	}
	if err = c.call(ctx, "POST", "/v1/access-requests", key, body, &value); err != nil {
		return value, err
	}
	if !idPattern.MatchString(value.RequestID) || value.StatusURL != c.config.PublicOrigin+"/v1/access-requests/"+value.RequestID || value.InteractionURL != c.config.PublicOrigin+"/interactions/"+value.RequestID || value.Status != "AUTHENTICATION_PENDING" || value.ExpiresAt.IsZero() {
		return Accepted{}, ErrUnavailable
	}
	return value, nil
}
func (c *Client) readStatus(ctx context.Context, id string, cancel bool) (Status, error) {
	var value Status
	if !idPattern.MatchString(id) {
		return value, ErrInput
	}
	method, path := "GET", "/v1/access-requests/"+id
	if cancel {
		method = "POST"
		path += "/cancel"
	}
	if err := c.call(ctx, method, path, "", nil, &value); err != nil {
		return value, err
	}
	if value.RequestID != id || !slices.Contains(platformStates, value.Status) || value.Version < 1 || value.ExpiresAt.IsZero() {
		return Status{}, ErrUnavailable
	}
	return value, nil
}
func (c *Client) Status(ctx context.Context, id string) (Status, error) {
	return c.readStatus(ctx, id, false)
}
func (c *Client) Cancel(ctx context.Context, id string) (Status, error) {
	return c.readStatus(ctx, id, true)
}
func (c *Client) IssueGrant(ctx context.Context, id, key string) (*Grant, error) {
	if !idPattern.MatchString(id) || !idPattern.MatchString(key) {
		return nil, ErrInput
	}
	var value struct {
		Grant     string    `json:"grant"`
		ExpiresAt time.Time `json:"expires_at"`
		TokenType string    `json:"token_type"`
	}
	if err := c.call(ctx, "POST", "/v1/access-requests/"+id+"/grant", key, nil, &value); err != nil {
		return nil, err
	}
	if len(value.Grant) == 0 || len(value.Grant) > 32768 || value.TokenType != "Bearer" || !time.Now().Before(value.ExpiresAt) {
		return nil, ErrUnavailable
	}
	return &Grant{ExpiresAt: value.ExpiresAt, token: value.Grant, requestID: id, client: c, used: &atomic.Bool{}}, nil
}

// Retrieve attempts one disclosure. A grant handle remains spent on any transport outcome.
func (c *Client) Retrieve(ctx context.Context, grant *Grant, fields []string) (Result, error) {
	var value Result
	if c.private == nil || grant == nil || grant.client != c || grant.used == nil || !validFields(fields) || !time.Now().Before(grant.ExpiresAt) {
		return value, ErrInput
	}
	if !grant.used.CompareAndSwap(false, true) {
		return value, ErrConsumed
	}
	request, err := http.NewRequestWithContext(ctx, "GET", c.config.DataOrigin+"/v1/access-requests/"+grant.requestID+"/data", nil)
	if err != nil {
		return value, ErrInput
	}
	request.Header.Set("Authorization", "Bearer "+grant.token)
	if err = perform(c.private, request, &value); err != nil {
		return value, err
	}
	if value.RequestID != grant.requestID || !idPattern.MatchString(value.ReceiptID) || len(value.Data) != len(fields) {
		return Result{}, ErrUnavailable
	}
	for _, field := range fields {
		if _, ok := value.Data[field]; !ok {
			return Result{}, ErrUnavailable
		}
	}
	return value, nil
}
func (c *Client) HistoryURL(registry string) (string, error) {
	if !idPattern.MatchString(registry) {
		return "", ErrInput
	}
	return c.config.PublicOrigin + "/consent-history/" + registry, nil
}

// WaitForConsent polls metadata only. Callers retain control of grant issuance and retrieval.
func (c *Client) WaitForConsent(ctx context.Context, id string, interval time.Duration) (Status, error) {
	if interval < time.Second {
		return Status{}, ErrInput
	}
	for {
		state, err := c.Status(ctx, id)
		if err != nil {
			return Status{}, err
		}
		if state.Status == "CONSENTED" || state.Status == "AUTHORIZED" || slices.Contains(terminalStates, state.Status) {
			return state, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Status{}, ctx.Err()
		case <-timer.C:
		}
	}
}

var terminalStates = []string{"COMPLETED", "DELIVERY_UNKNOWN", "REJECTED", "DENIED", "WITHDRAWN", "EXPIRED", "FAILED", "CANCELLED"}
var platformStates = []string{"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED", "COMPLETED", "DELIVERY_UNKNOWN", "REJECTED", "DENIED", "WITHDRAWN", "EXPIRED", "FAILED", "CANCELLED"}
