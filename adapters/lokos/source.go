package lokos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

// HTTPSource is a demo-native transport. Real LokOS onboarding supplies its own
// approved API integration; no core registry switch or native API assumption is used.
type HTTPSource struct {
	Origin    string
	Client    *http.Client
	AllowDemo bool
}

func (s HTTPSource) VerifySubject(ctx context.Context, p sdk.PrincipalContext, h sdk.Hint) (string, error) {
	raw, err := json.Marshal(struct {
		Principal sdk.PrincipalContext `json:"principal"`
		Hint      sdk.Hint             `json:"hint"`
	}{p, h})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(s.Origin, "/")+"/native/v1/verify-subject", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.Client.Do(req)
	if err != nil {
		return "", errors.New("source verifier unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == 403 || response.StatusCode == 404 {
		return "", sdk.ErrVerification
	}
	if response.StatusCode != 200 {
		return "", errors.New("source verifier unavailable")
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return "", sdk.ErrVerification
	}
	var result struct {
		Reference string `json:"source_mapping_ref"`
		Demo      bool   `json:"demo"`
	}
	if sdk.Decode(raw, &result) != nil || result.Reference == "" || (result.Demo && !s.AllowDemo) {
		return "", sdk.ErrVerification
	}
	return result.Reference, nil
}
func (s HTTPSource) Membership(ctx context.Context, reference string) (Membership, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(s.Origin, "/")+"/native/v1/members/"+url.PathEscape(reference), nil)
	if err != nil {
		return Membership{}, err
	}
	response, err := s.Client.Do(req)
	if err != nil {
		return Membership{}, errors.New("source unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Membership{}, errors.New("source read unsuccessful")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return Membership{}, errors.New("invalid source response")
	}
	var result struct {
		Status  string      `json:"membership_status"`
		Savings json.Number `json:"savings_rupees"`
		Demo    bool        `json:"demo"`
	}
	if sdk.Decode(raw, &result) != nil || (result.Demo && !s.AllowDemo) {
		return Membership{}, errors.New("invalid source response")
	}
	return Membership{Status: result.Status, SavingsRupees: result.Savings}, nil
}
