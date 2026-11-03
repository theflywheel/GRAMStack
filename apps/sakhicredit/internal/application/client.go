// Package application owns SakhiCredit product state. It uses the public
// GRAMStack contract and has no dependency on gateway implementation packages.
package application

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gowebpki/jcs"
)

var (
	ErrInput       = errors.New("invalid input")
	ErrNotFound    = errors.New("application not found")
	ErrConflict    = errors.New("application state conflict")
	ErrUnavailable = errors.New("platform unavailable")
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	memberPattern  = regexp.MustCompile(`^member-[0-9]{1,32}$`)
	amountPattern  = regexp.MustCompile(`^(0|[1-9][0-9]{0,12})(\.[0-9]{1,2})?$`)
)

type ClientConfig struct {
	PublicOrigin, DataOrigin, TokenURL, ClientID, ClientSecret, DataServerName string
	CA, Certificate, Key                                                       []byte
	AllowLoopback                                                              bool
}
type Client struct {
	config          ClientConfig
	public, private *http.Client
	mu              sync.Mutex
	token           string
	tokenUntil      time.Time
}
type Input struct {
	MemberReference string `json:"member_reference"`
	IncludeSavings  bool   `json:"include_savings"`
}

func (in Input) validate() error {
	if !memberPattern.MatchString(in.MemberReference) {
		return ErrInput
	}
	return nil
}
func (in Input) fields() []string {
	fields := []string{"membership_status"}
	if in.IncludeSavings {
		fields = append(fields, "savings_summary")
	}
	return fields
}

type Accepted struct {
	RequestID      string    `json:"request_id"`
	Status         string    `json:"status"`
	StatusURL      string    `json:"status_url"`
	InteractionURL string    `json:"interaction_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}
type PlatformStatus struct {
	RequestID  string    `json:"request_id"`
	Status     string    `json:"status"`
	Version    int64     `json:"version"`
	NextAction string    `json:"next_action"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type grantResponse struct {
	Grant     string    `json:"grant"`
	ExpiresAt time.Time `json:"expires_at"`
	TokenType string    `json:"token_type"`
}
type Result struct {
	RequestID string                     `json:"request_id"`
	Data      map[string]json.RawMessage `json:"data"`
	ReceiptID string                     `json:"receipt_id"`
}
type RemoteError struct{ Status int }

func (e *RemoteError) Error() string {
	return "platform rejected operation (HTTP " + strconv.Itoa(e.Status) + ")"
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
func NewClient(c ClientConfig) (*Client, error) {
	if !trustedURL(c.PublicOrigin, c.AllowLoopback, true) || !trustedURL(c.DataOrigin, false, true) || !trustedURL(c.TokenURL, c.AllowLoopback, false) || !idPattern.MatchString(c.ClientID) || c.ClientSecret == "" || c.DataServerName == "" {
		return nil, errors.New("trusted platform origins, consumer credentials and data identity required")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(c.CA) {
		return nil, errors.New("platform CA required")
	}
	pair, err := tls.X509KeyPair(c.Certificate, c.Key)
	if err != nil {
		return nil, errors.New("consumer TLS keypair required")
	}
	publicRoots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	publicRoots.AppendCertsFromPEM(c.CA)
	noRedirect := func(*http.Request, []*http.Request) error { return errors.New("platform redirects forbidden") }
	public := &http.Client{Timeout: 10 * time.Second, CheckRedirect: noRedirect, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: publicRoots}}}
	// Disable connection reuse so net/http cannot transparently retry a one-use GET.
	private := &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect, Transport: &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: c.DataServerName, Certificates: []tls.Certificate{pair}}}}
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
	r.SetBasicAuth(c.config.ClientID, c.config.ClientSecret)
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
		return &RemoteError{response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 || response.Header.Get("Gramstack-Contract-Version") != "0.3.0" || decode(raw, out) != nil {
		return ErrUnavailable
	}
	return nil
}
func (c *Client) Create(ctx context.Context, id string, in Input) (Accepted, error) {
	var value Accepted
	if !idPattern.MatchString(id) || in.validate() != nil {
		return value, ErrInput
	}
	body, _ := json.Marshal(map[string]any{"registry": "lokos", "resource": "shg.membership", "subject_hint": map[string]string{"type": "member_reference", "value": in.MemberReference}, "fields": in.fields(), "purpose": "credit_eligibility", "access": map[string]any{"mode": "single_use", "valid_for_seconds": 300}, "recipient_terms": map[string]string{"retention_policy_id": "terms-v1"}})
	err := c.call(ctx, "POST", "/v1/access-requests", "sakhi-create-"+id, body, &value)
	if err != nil {
		return value, err
	}
	if !idPattern.MatchString(value.RequestID) || value.StatusURL != c.config.PublicOrigin+"/v1/access-requests/"+value.RequestID || value.InteractionURL != c.config.PublicOrigin+"/interactions/"+value.RequestID || value.Status != "AUTHENTICATION_PENDING" || value.ExpiresAt.IsZero() {
		return Accepted{}, ErrUnavailable
	}
	return value, nil
}
func (c *Client) Status(ctx context.Context, id string, cancel bool) (PlatformStatus, error) {
	var value PlatformStatus
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
	if value.RequestID != id || !slices.Contains(platformStates, value.Status) || value.ExpiresAt.IsZero() {
		return PlatformStatus{}, ErrUnavailable
	}
	return value, nil
}
func (c *Client) grant(ctx context.Context, id, applicationID string) (string, error) {
	var value grantResponse
	if err := c.call(ctx, "POST", "/v1/access-requests/"+id+"/grant", "sakhi-grant-"+applicationID, nil, &value); err != nil {
		return "", err
	}
	if len(value.Grant) == 0 || len(value.Grant) > 32768 || value.TokenType != "Bearer" || !time.Now().Before(value.ExpiresAt) {
		return "", ErrUnavailable
	}
	return value.Grant, nil
}
func (c *Client) fetch(ctx context.Context, id, grant string, in Input) (Result, error) {
	var result Result
	r, err := http.NewRequestWithContext(ctx, "GET", c.config.DataOrigin+"/v1/access-requests/"+id+"/data", nil)
	if err != nil {
		return result, ErrInput
	}
	r.Header.Set("Authorization", "Bearer "+grant)
	if err := perform(c.private, r, &result); err != nil {
		return Result{}, err
	}
	if err := result.validate(id, in); err != nil {
		return Result{}, err
	}
	return result, nil
}
func (result Result) validate(id string, in Input) error {
	if result.RequestID != id || !idPattern.MatchString(result.ReceiptID) || len(result.Data) != len(in.fields()) {
		return ErrUnavailable
	}
	var membership string
	if json.Unmarshal(result.Data["membership_status"], &membership) != nil || !slices.Contains([]string{"active", "inactive", "suspended"}, membership) {
		return ErrUnavailable
	}
	if in.IncludeSavings {
		var savings struct {
			Amount   json.Number `json:"amount"`
			Currency string      `json:"currency"`
		}
		if decode(result.Data["savings_summary"], &savings) != nil || savings.Currency != "INR" {
			return ErrUnavailable
		}
		amount, err := savings.Amount.Float64()
		if err != nil || amount < 0 {
			return ErrUnavailable
		}
	}
	return nil
}

var platformStates = []string{"AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED", "COMPLETED", "DELIVERY_UNKNOWN", "REJECTED", "DENIED", "WITHDRAWN", "EXPIRED", "FAILED", "CANCELLED"}
