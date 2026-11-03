package oidcadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gowebpki/jcs"
)

// OIDC trusts the pinned issuer's published public keys, not an x5c CA chain.
// Some demo providers publish invalid optional certificate metadata. This
// opt-in transformer removes only that metadata, retaining the public key,
// algorithm, key ID and use for the normal JOSE signature verifier.
type publicJWKTransport struct {
	endpoint string
	base     http.RoundTripper
}

func (t publicJWKTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil || r.URL.String() != t.endpoint || response.StatusCode != 200 {
		return response, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("invalid provider key set")
	}
	if _, err = jcs.Transform(raw); err != nil {
		return nil, errors.New("invalid provider key set JSON")
	}
	var set struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if json.Unmarshal(raw, &set) != nil || len(set.Keys) == 0 || len(set.Keys) > 100 {
		return nil, errors.New("invalid provider key set")
	}
	for _, key := range set.Keys {
		delete(key, "x5c")
		delete(key, "x5t")
		delete(key, "x5t#S256")
	}
	encoded, err := json.Marshal(set)
	if err != nil {
		return nil, errors.New("invalid provider key set")
	}
	response.Body = io.NopCloser(bytes.NewReader(encoded))
	response.ContentLength = int64(len(encoded))
	response.Header.Del("Content-Length")
	return response, nil
}
