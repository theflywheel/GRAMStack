package consumer

import (
	"fmt"
	"slices"
	"strings"
)

// PurposeManifest describes one operator-approved purpose for a policy's
// resource. Its field catalogue does not expand the request's selected fields.
type PurposeManifest struct {
	Version             string            `json:"version"`
	Owner               string            `json:"owner"`
	FieldJustifications map[string]string `json:"field_justifications"`
}

func (p PurposeManifest) terms(policy Policy) (string, error) {
	if !idPattern.MatchString(p.Version) || !idPattern.MatchString(p.Owner) ||
		len(policy.Purposes) != 1 || !idPattern.MatchString(policy.Purposes[0]) ||
		len(policy.Fields) == 0 || len(policy.Fields) != len(p.FieldJustifications) {
		return "", ErrInput
	}
	fields := slices.Clone(policy.Fields)
	slices.Sort(fields)
	var text strings.Builder
	fmt.Fprintf(&text, "%s\nPurpose manifest: %s (version %s)\nPurpose owner: %s\nPolicy field explanations (only the fields listed in your request can be shared):",
		policy.TermsText, policy.Purposes[0], p.Version, p.Owner)
	for i, field := range fields {
		reason := p.FieldJustifications[field]
		if !idPattern.MatchString(field) || (i > 0 && field == fields[i-1]) ||
			strings.TrimSpace(reason) == "" || len(reason) > 512 || strings.ContainsAny(reason, "\r\n") {
			return "", ErrInput
		}
		fmt.Fprintf(&text, "\n- %s: %s", field, reason)
	}
	return text.String(), nil
}
