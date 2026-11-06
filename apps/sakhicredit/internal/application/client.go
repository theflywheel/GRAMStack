// Package application owns SakhiCredit business state and resource-specific validation.
package application

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"bytes"
	"github.com/gowebpki/jcs"
	gramstack "github.com/theflywheel/GRAMStack/sdk/go/client"
	"io"
	"net"
	"net/url"
)

var (
	ErrInput       = gramstack.ErrInput
	ErrUnavailable = gramstack.ErrUnavailable
	ErrNotFound    = errors.New("application not found")
	ErrConflict    = errors.New("application state conflict")
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	memberPattern  = regexp.MustCompile(`^member-[0-9]{1,32}$`)
	amountPattern  = regexp.MustCompile(`^(0|[1-9][0-9]{0,12})(\.[0-9]{1,2})?$`)
)

type ClientConfig = gramstack.Config
type Accepted = gramstack.Accepted
type PlatformStatus = gramstack.Status
type RemoteError = gramstack.RemoteError
type Result gramstack.Result
type Client struct{ platform *gramstack.Client }
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
func NewClient(config ClientConfig) (*Client, error) {
	platform, err := gramstack.New(config)
	if err != nil {
		return nil, err
	}
	return &Client{platform: platform}, nil
}
func (c *Client) Create(ctx context.Context, id string, input Input) (Accepted, error) {
	if !idPattern.MatchString(id) || input.validate() != nil {
		return Accepted{}, ErrInput
	}
	request := gramstack.AccessRequest{
		Registry: "lokos", Resource: "shg.membership",
		SubjectHint: gramstack.SubjectHint{Type: "member_reference", Value: input.MemberReference},
		Fields:      input.fields(), Purpose: "credit_eligibility",
		Access:         gramstack.Access{Mode: "single_use", ValidForSeconds: 300},
		RecipientTerms: gramstack.RecipientTerms{RetentionPolicyID: "terms-v1"},
	}
	return c.platform.Create(ctx, "sakhi-create-"+id, request)
}
func (c *Client) Status(ctx context.Context, id string, cancel bool) (PlatformStatus, error) {
	if cancel {
		return c.platform.Cancel(ctx, id)
	}
	return c.platform.Status(ctx, id)
}
func (c *Client) grant(ctx context.Context, id, applicationID string) (*gramstack.Grant, error) {
	return c.platform.IssueGrant(ctx, id, "sakhi-grant-"+applicationID)
}
func (c *Client) fetch(ctx context.Context, id string, grant *gramstack.Grant, input Input) (Result, error) {
	value, err := c.platform.Retrieve(ctx, grant, input.fields())
	if err != nil {
		return Result{}, err
	}
	result := Result(value)
	if err = result.validate(id, input); err != nil {
		return Result{}, err
	}
	return result, nil
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

func trustedURL(raw string, loopback, origin bool) bool {
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
