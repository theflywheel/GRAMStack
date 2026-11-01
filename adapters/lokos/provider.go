// Package lokos implements registry-specific source mapping through the Go SDK.
package lokos

import (
	"context"
	"encoding/json"
	"errors"
	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
)

// Membership is the canonical demo source projection; the real upstream mapping
// must be implemented against the registry-approved source contract.
type Membership struct {
	Status        string
	SavingsRupees json.Number
}
type Source interface {
	Membership(context.Context, string) (Membership, error)
}
type Provider struct{ source Source }

var _ sdk.Provider = (*Provider)(nil)

func New(source Source) (*Provider, error) {
	if source == nil {
		return nil, errors.New("LokOS source required")
	}
	return &Provider{source: source}, nil
}
func (*Provider) Resource() sdk.Resource {
	return sdk.Resource{Name: "shg.membership", Version: "1", Fields: []string{"membership_status", "savings_summary"}}
}
func (p *Provider) Fetch(ctx context.Context, subject sdk.Subject, fields []string) (sdk.Data, error) {
	if subject.Reference == "" {
		return nil, errors.New("verified subject reference required")
	}
	record, err := p.source.Membership(ctx, subject.Reference)
	if err != nil {
		return nil, err
	}
	data := sdk.Data{}
	for _, field := range fields {
		switch field {
		case "membership_status":
			switch record.Status {
			case "active", "inactive", "suspended":
			default:
				return nil, errors.New("unsupported source membership status")
			}
			data[field], _ = json.Marshal(record.Status)
		case "savings_summary":
			if record.SavingsRupees == "" {
				return nil, errors.New("source savings amount missing")
			}
			// Validation does not convert the decimal representation used in the response.
			amount, err := record.SavingsRupees.Float64()
			if err != nil || amount < 0 {
				return nil, errors.New("invalid source savings amount")
			}
			value, err := json.Marshal(struct {
				Amount   json.Number `json:"amount"`
				Currency string      `json:"currency"`
			}{record.SavingsRupees, "INR"})
			if err != nil {
				return nil, errors.New("invalid source savings amount")
			}
			data[field] = value
		default:
			return nil, sdk.ErrFields
		}
	}
	return data, nil
}
