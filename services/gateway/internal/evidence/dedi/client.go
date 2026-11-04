// Package dedi anchors protected commitments using the signed publication and
// inclusion-proof protocol implemented by theflywheel/DeDi-node.
package dedi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

var component = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var commitment = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

const responseLimit = 1 << 20

type Config struct {
	BaseURL, Namespace, Registry         string
	PublisherKeyID                       string
	PublisherKey                         ed25519.PrivateKey
	AdminUser, AdminPassword             string
	CheckpointOrigin, CheckpointVerifier string
	AllowInsecureLoopback                bool // Test/development only; never permits remote plaintext HTTP.
}
type Client struct {
	proofVerifier *Verifier
	config        Config
	origin        *url.URL
	http          *http.Client
}

// VerificationConfig contains public trust configuration only.
type VerificationConfig struct {
	Namespace, Registry, PublisherKeyID, CheckpointOrigin, CheckpointVerifier string
}

// Verifier checks retained commitments offline without publication credentials.
type Verifier struct {
	config   VerificationConfig
	verifier note.Verifier
}

func NewVerifier(config VerificationConfig) (*Verifier, error) {
	for _, part := range []string{config.Namespace, config.Registry, config.PublisherKeyID} {
		if !component.MatchString(part) {
			return nil, errors.New("invalid DeDi namespace, registry or publisher key ID")
		}
	}
	if config.CheckpointOrigin == "" {
		return nil, errors.New("DeDi checkpoint origin required")
	}
	verifier, err := note.NewVerifier(config.CheckpointVerifier)
	if err != nil {
		return nil, fmt.Errorf("DeDi checkpoint verifier: %w", err)
	}
	return &Verifier{config: config, verifier: verifier}, nil
}

type Anchor struct {
	RecordID   string          `json:"record_id"`
	TreeSize   int64           `json:"tree_size"`
	LeafIndex  int64           `json:"leaf_index"`
	Checkpoint string          `json:"checkpoint"`
	Inclusion  json.RawMessage `json:"inclusion_proof"`
}

func New(config Config) (*Client, error) {
	base, err := url.Parse(config.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("DeDi base URL must be an origin without credentials, path, query or fragment")
	}
	if base.Scheme != "https" {
		ip := net.ParseIP(base.Hostname())
		if base.Scheme != "http" || !config.AllowInsecureLoopback || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("DeDi requires HTTPS; explicit loopback HTTP is development-only")
		}
	}
	if len(config.PublisherKey) != ed25519.PrivateKeySize || config.AdminUser == "" || config.AdminPassword == "" {
		return nil, errors.New("DeDi publisher and admin configuration required")
	}
	verifier, err := NewVerifier(VerificationConfig{
		Namespace: config.Namespace, Registry: config.Registry, PublisherKeyID: config.PublisherKeyID,
		CheckpointOrigin: config.CheckpointOrigin, CheckpointVerifier: config.CheckpointVerifier,
	})
	if err != nil {
		return nil, err
	}
	config.PublisherKey = bytes.Clone(config.PublisherKey)
	return &Client{proofVerifier: verifier, config: config, origin: base, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("DeDi redirects are forbidden") }}}, nil
}

// Provision is an explicit operator operation, never run implicitly on a read.
// It installs the namespace and commitment registry using create-only writes.
func (c *Client) Provision(ctx context.Context) error {
	for _, path := range []string{"/admin/namespaces/" + c.config.Namespace, "/admin/namespaces/" + c.config.Namespace + "/registries/" + c.config.Registry} {
		status, _, err := c.write(ctx, http.MethodPut, path, []byte(`{"description":"GRAMStack protected evidence commitments"}`))
		if err != nil {
			return err
		}
		if status != http.StatusOK && status != http.StatusCreated && status != http.StatusPreconditionFailed {
			return fmt.Errorf("DeDi provisioning returned HTTP %d", status)
		}
	}
	return nil
}

// Publish is idempotent by event ID and content. It returns only after verifying
// the actual commitment's inclusion under the configured node checkpoint key.
// No plaintext receipt, source data, subject ID or signing secret is published.
func (c *Client) Publish(ctx context.Context, eventID, digest string) (Anchor, error) {
	if eventID == "" || len(eventID) > 256 || !commitment.MatchString(digest) {
		return Anchor{}, errors.New("opaque event ID and SHA-256 evidence commitment required")
	}
	id := sha256.Sum256([]byte(eventID))
	record := hex.EncodeToString(id[:])
	payload, _ := json.Marshal(struct {
		Schema string `json:"schema"`
		Digest string `json:"evidence_digest"`
	}{"gramstack.evidence.v1", digest})
	path := "/admin/namespaces/" + c.config.Namespace + "/registries/" + c.config.Registry + "/records/" + record + "/publish"
	status, _, err := c.write(ctx, http.MethodPost, path, payload)
	if err != nil {
		return Anchor{}, err
	}
	if status != http.StatusOK && status != http.StatusCreated && status != http.StatusPreconditionFailed {
		return Anchor{}, fmt.Errorf("DeDi publication returned HTTP %d", status)
	}
	// A 412 is a retry or a conflict; reconcile using a verified read, never an overwrite.
	readPath := "/dedi/lookup/" + c.config.Namespace + "/" + c.config.Registry + "/" + record + "?proof=inclusion"
	status, body, err := c.request(ctx, http.MethodGet, readPath, nil, false)
	if err != nil {
		return Anchor{}, err
	}
	if status != http.StatusOK {
		return Anchor{}, fmt.Errorf("DeDi proof lookup returned HTTP %d", status)
	}
	return c.proofVerifier.verify(body, record, payload)
}

func (c *Client) write(ctx context.Context, method, path string, payload []byte) (int, []byte, error) {
	body := append([]byte(`{"payload":`), payload...)
	body = append(body, '}')
	return c.request(ctx, method, path, body, true)
}
func (c *Client) request(ctx context.Context, method, path string, body []byte, signed bool) (int, []byte, error) {
	target := *c.origin
	relative, err := url.Parse(path)
	if err != nil {
		return 0, nil, err
	}
	target.Path = relative.Path
	target.RawQuery = relative.RawQuery
	req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if signed {
		timestamp := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		digest := sha256.Sum256(body)
		preimage := strings.Join([]string{"dedi/v1/publish", method, req.URL.RequestURI(), base64.StdEncoding.EncodeToString(digest[:]), "", "*", timestamp}, "\n")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-None-Match", "*")
		req.Header.Set("DeDi-Key-Id", c.config.PublisherKeyID)
		req.Header.Set("DeDi-Timestamp", timestamp)
		req.Header.Set("DeDi-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(c.config.PublisherKey, []byte(preimage))))
		req.SetBasicAuth(c.config.AdminUser, c.config.AdminPassword)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return 0, nil, errors.New("DeDi transport failed; operation outcome may require reconciliation")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return 0, nil, errors.New("DeDi response read failed")
	}
	if len(raw) > responseLimit {
		return 0, nil, errors.New("DeDi response exceeds size limit")
	}
	return response.StatusCode, raw, nil
}

type proof struct {
	LeafIndex  int64    `json:"leaf_index"`
	TreeSize   int64    `json:"tree_size"`
	Checkpoint string   `json:"checkpoint"`
	Path       []string `json:"path"`
	Leaf       struct {
		EntryType  string `json:"entry_type"`
		Namespace  string `json:"namespace"`
		Registry   string `json:"registry"`
		RecordName string `json:"record_name"`
		VersionNum int32  `json:"version_num"`
		Digest     string `json:"digest"`
		CreatedBy  string `json:"created_by"`
		CreatedAt  string `json:"created_at"`
	} `json:"leaf"`
}

func (c *Verifier) verify(body []byte, record string, payload []byte) (Anchor, error) {
	var envelope struct {
		Data struct {
			Details json.RawMessage `json:"details"`
			State   string          `json:"state"`
		} `json:"data"`
		Proof *proof `json:"proof"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Anchor{}, errors.New("malformed DeDi proof response")
	}
	p := envelope.Proof
	if p == nil {
		return Anchor{}, errors.New("DeDi inclusion proof missing")
	}
	expected := sha256.Sum256(payload)
	leaf := p.Leaf
	var compact bytes.Buffer
	if err := json.Compact(&compact, envelope.Data.Details); err != nil || !bytes.Equal(compact.Bytes(), payload) {
		return Anchor{}, errors.New("DeDi record commitment conflicts with the requested evidence")
	}
	if envelope.Data.State != "live" || leaf.EntryType != "record" || leaf.Namespace != c.config.Namespace || leaf.Registry != c.config.Registry || leaf.RecordName != record || leaf.VersionNum != 1 || leaf.CreatedBy != "publisher:"+c.config.PublisherKeyID || leaf.Digest != hex.EncodeToString(expected[:]) {
		return Anchor{}, errors.New("DeDi proof is not bound to the expected immutable publisher record")
	}
	checkpoint, err := note.Open([]byte(p.Checkpoint), note.VerifierList(c.verifier))
	if err != nil {
		return Anchor{}, errors.New("DeDi checkpoint signature does not match pinned verifier")
	}
	lines := strings.Split(checkpoint.Text, "\n")
	if len(lines) != 4 || lines[3] != "" || lines[0] != c.config.CheckpointOrigin {
		return Anchor{}, errors.New("unexpected DeDi checkpoint origin or format")
	}
	size, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil || size <= 0 || size != p.TreeSize || p.LeafIndex < 0 || p.LeafIndex >= size || len(p.Path) > 64 {
		return Anchor{}, errors.New("invalid DeDi tree bounds")
	}
	rootRaw, err := base64.StdEncoding.DecodeString(lines[2])
	if err != nil || len(rootRaw) != 32 {
		return Anchor{}, errors.New("invalid DeDi Merkle root")
	}
	var root tlog.Hash
	copy(root[:], rootRaw)
	created, err := time.Parse(time.RFC3339Nano, leaf.CreatedAt)
	if err != nil {
		return Anchor{}, errors.New("invalid DeDi leaf timestamp")
	}
	preimage, _ := json.Marshal([]any{"dedi/v1/leaf", leaf.EntryType, leaf.Namespace, leaf.Registry, leaf.RecordName, leaf.VersionNum, leaf.Digest, leaf.CreatedBy, created.UTC().Format(time.RFC3339Nano)})
	path := make(tlog.RecordProof, len(p.Path))
	for i, encoded := range p.Path {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != 32 {
			return Anchor{}, errors.New("invalid DeDi audit path hash")
		}
		copy(path[i][:], raw)
	}
	if err := tlog.CheckRecord(path, size, root, p.LeafIndex, tlog.RecordHash(preimage)); err != nil {
		return Anchor{}, errors.New("DeDi Merkle inclusion verification failed")
	}
	rawProof, err := json.Marshal(p)
	if err != nil {
		return Anchor{}, err
	}
	return Anchor{RecordID: record, TreeSize: size, LeafIndex: p.LeafIndex, Checkpoint: p.Checkpoint, Inclusion: rawProof}, nil
}

// VerifyAnchor checks retained evidence against the client's pinned trust.
func (c *Client) VerifyAnchor(eventID, digest string, anchor Anchor) error {
	return c.proofVerifier.VerifyAnchor(eventID, digest, anchor)
}

// VerifyAnchor revalidates retained inclusion evidence offline against the same
// pinned node, publisher and exact protected commitment used for publication.
func (c *Verifier) VerifyAnchor(eventID, digest string, a Anchor) error {
	if eventID == "" || len(eventID) > 256 || !commitment.MatchString(digest) || len(a.Inclusion) == 0 || len(a.Inclusion) > responseLimit {
		return errors.New("invalid retained DeDi anchor")
	}
	id := sha256.Sum256([]byte(eventID))
	record := hex.EncodeToString(id[:])
	payload, _ := json.Marshal(struct {
		Schema string `json:"schema"`
		Digest string `json:"evidence_digest"`
	}{"gramstack.evidence.v1", digest})
	body, err := json.Marshal(map[string]any{"data": map[string]any{"details": json.RawMessage(payload), "state": "live"}, "proof": a.Inclusion})
	if err != nil {
		return err
	}
	verified, err := c.verify(body, record, payload)
	if err != nil {
		return err
	}
	if verified.RecordID != a.RecordID || verified.TreeSize != a.TreeSize || verified.LeafIndex != a.LeafIndex || verified.Checkpoint != a.Checkpoint {
		return errors.New("retained DeDi reference disagrees with proof")
	}
	return nil
}
