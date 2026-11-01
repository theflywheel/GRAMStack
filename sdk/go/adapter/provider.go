// Package adapter contains reusable registry-adapter primitives. Source helpers
// do not authorize retrieval: the authenticated grant/enforcement pipeline must
// admit a request before invoking them.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

var ErrFields = errors.New("invalid resource fields")

type Resource struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Fields  []string `json:"fields"`
}

// Subject is an opaque, request-scoped source mapping resolved by a trusted
// registry-approved verifier. Raw client-provided hints are not verified subjects.
type Subject struct{ Reference string }
type Data map[string]json.RawMessage

// Provider is the registry-specific hook called after authorization. Implementers
// supply source access and mappings; SDK processing always projects selected fields.
type Provider interface {
	Resource() Resource
	Fetch(context.Context, Subject, []string) (Data, error)
}

// ReadSelected validates the field selection before source access and removes
// all unrequested source fields. It is a pipeline primitive, not an HTTP API.
func ReadSelected(ctx context.Context, provider Provider, subject Subject, fields []string) (Data, error) {
	if subject.Reference == "" {
		return nil, errors.New("verified subject reference required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource := provider.Resource()
	seen := map[string]bool{}
	if len(fields) == 0 {
		return nil, ErrFields
	}
	for _, field := range fields {
		if seen[field] || !slices.Contains(resource.Fields, field) {
			return nil, ErrFields
		}
		seen[field] = true
	}
	data, err := provider.Fetch(ctx, subject, slices.Clone(fields))
	if err != nil {
		return nil, fmt.Errorf("source retrieval failed: %w", err)
	}
	selected := Data{}
	for _, field := range fields {
		value, ok := data[field]
		if !ok || !json.Valid(value) {
			return nil, fmt.Errorf("source omitted or malformed authorized field %q", field)
		}
		selected[field] = slices.Clone(value)
	}
	return selected, nil
}
