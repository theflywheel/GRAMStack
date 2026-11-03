package discovery

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"mime"
	"net/http"
	"slices"
	"time"

	"github.com/gowebpki/jcs"
)

type Verifier struct{ client *http.Client }

func New(ca []byte) (*Verifier, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if len(ca) > 0 && !roots.AppendCertsFromPEM(ca) {
		return nil, ErrDescriptor
	}
	return &Verifier{&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (v *Verifier) read(ctx context.Context, origin, path string, target any) error {
	r, err := http.NewRequestWithContext(ctx, "GET", origin+path, nil)
	if err != nil {
		return ErrMetadata
	}
	r.Header.Set("Accept", "application/json")
	result, err := v.client.Do(r)
	if err != nil {
		return ErrMetadata
	}
	defer result.Body.Close()
	media, _, err := mime.ParseMediaType(result.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || result.StatusCode != 200 || result.Header.Get(VersionHeader) != Version || result.Header.Get("Gramstack-Contract-Version") != "0.3.0" || result.Header.Get("Cache-Control") != "no-store" {
		return ErrMetadata
	}
	raw, err := io.ReadAll(io.LimitReader(result.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return ErrMetadata
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return ErrMetadata
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrMetadata
	}
	normalized, err := json.Marshal(target)
	if err != nil {
		return ErrMetadata
	}
	normalized, err = jcs.Transform(normalized)
	if err != nil || !bytes.Equal(canonical, normalized) {
		return ErrMetadata
	}
	return nil
}
func (v *Verifier) Check(ctx context.Context, a *Approved) error {
	if a == nil {
		return ErrDescriptor
	}
	d := a.descriptor
	var manifest Manifest
	if err := v.read(ctx, d.Origin, ManifestPath, &manifest); err != nil {
		return err
	}
	if manifest.AdapterID != d.AdapterID || manifest.Registry != d.Registry || manifest.Audience != d.Audience || manifest.ContractVersion != "0.3.0" || manifest.Authentication != "mtls" || !validIDs(manifest.SigningKeyIDs, 16) || !validIDs(manifest.VerificationMethods, 16) || !slices.Contains(manifest.VerificationMethods, d.VerificationMethod) || !validResources(manifest.Resources) || len(manifest.Capabilities) != 2 || !slices.Contains(manifest.Capabilities, "atomic-redemption") || !slices.Contains(manifest.Capabilities, "evidence-before-release") {
		return ErrMetadata
	}
	for _, id := range manifest.SigningKeyIDs {
		if d.Keys[id] == nil {
			return ErrMetadata
		}
	}
	for _, required := range d.Resources {
		matched := false
		for _, r := range manifest.Resources {
			if r.Name == required.Name {
				if r.Version != required.Version {
					return ErrMetadata
				}
				for _, f := range required.Fields {
					if !slices.Contains(r.Fields, f) {
						return ErrMetadata
					}
				}
				matched = true
			}
		}
		if !matched {
			return ErrMetadata
		}
	}
	var set struct {
		Keys []PublicJWK `json:"keys"`
	}
	if err := v.read(ctx, d.Origin, KeysPath, &set); err != nil {
		return err
	}
	if len(set.Keys) != len(manifest.SigningKeyIDs) {
		return ErrMetadata
	}
	seen := map[string]bool{}
	for _, key := range set.Keys {
		if key.KeyType != "EC" || key.Curve != "P-256" || key.Algorithm != "ES256" || key.Use != "sig" || seen[key.ID] || !slices.Contains(manifest.SigningKeyIDs, key.ID) || d.Keys[key.ID] == nil {
			return ErrMetadata
		}
		seen[key.ID] = true
		x, ex := base64.RawURLEncoding.Strict().DecodeString(key.X)
		y, ey := base64.RawURLEncoding.Strict().DecodeString(key.Y)
		if ex != nil || ey != nil || len(x) != 32 || len(y) != 32 {
			return ErrMetadata
		}
		actual := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !elliptic.P256().IsOnCurve(actual.X, actual.Y) || !actual.Equal(d.Keys[key.ID]) {
			return ErrMetadata
		}
	}
	return nil
}
func (v *Verifier) Close() { v.client.CloseIdleConnections() }
