package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

// AuthorityClient uses a pinned HTTPS origin and service-authenticated client.
// Transport credentials and server-name verification belong to the host.
type AuthorityClient struct {
	origin string
	client *http.Client
}

func NewAuthorityClient(origin string, client *http.Client) (*AuthorityClient, error) {
	if !trustedOrigin(origin, false) || client == nil || client.Timeout <= 0 {
		return nil, ErrGrant
	}
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &AuthorityClient{strings.TrimSuffix(origin, "/"), &copied}, nil
}
func (c *AuthorityClient) post(ctx context.Context, path string, input, output any, want int) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return ErrRetrievalDependency
	}
	request, err := http.NewRequestWithContext(ctx, "POST", c.origin+path, bytes.NewReader(raw))
	if err != nil {
		return ErrRetrievalDependency
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Gramstack-Contract-Version", CoreVersion)
	response, err := c.client.Do(request)
	if err != nil {
		return ErrRetrievalDependency
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		switch response.StatusCode {
		case 403, 404:
			return ErrGrant
		case 409:
			return ErrAttemptExists
		case 410:
			return ErrGrantExpired
		default:
			return ErrRetrievalDependency
		}
	}
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" || response.Header.Get("Gramstack-Contract-Version") != CoreVersion {
		return ErrRetrievalDependency
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, 32769))
	if err != nil || len(raw) > 32768 || Decode(raw, output) != nil {
		return ErrRetrievalDependency
	}
	return nil
}
func (c *AuthorityClient) Redeem(ctx context.Context, input RedemptionRequest) (Reservation, error) {
	var out Reservation
	err := c.post(ctx, "/internal/v1/grants/redeem", input, &out, 200)
	return out, err
}
func (c *AuthorityClient) Record(ctx context.Context, signed string) (EvidenceAcknowledgement, error) {
	var out EvidenceAcknowledgement
	err := c.post(ctx, "/internal/v1/evidence", SignedEnvelope{signed}, &out, 201)
	return out, err
}
