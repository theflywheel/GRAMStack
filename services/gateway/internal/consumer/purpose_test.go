package consumer

import (
	"strings"
	"testing"
)

func TestPurposeManifestTerms(t *testing.T) {
	manifest := PurposeManifest{Version: "1", Owner: "sakhi-credit", FieldJustifications: map[string]string{
		"membership_status": "Verify membership.",
		"savings_summary":   "Review savings.",
	}}
	policy := Policy{Purposes: []string{"credit_eligibility"}, Fields: []string{"savings_summary", "membership_status"}, TermsText: "Approved terms."}
	terms, err := manifest.terms(policy)
	if err != nil || !strings.HasSuffix(terms, "\n- membership_status: Verify membership.\n- savings_summary: Review savings.") {
		t.Fatalf("unexpected purpose terms %q, %v", terms, err)
	}
	for _, fields := range [][]string{{"savings_summary"}, {"savings_summary", "unapproved"}, {"savings_summary", "savings_summary"}} {
		invalid := policy
		invalid.Fields = fields
		if _, err := manifest.terms(invalid); err == nil {
			t.Fatalf("manifest accepted inconsistent fields %v", fields)
		}
	}
	policy.Purposes = append(policy.Purposes, "marketing")
	if _, err := manifest.terms(policy); err == nil {
		t.Fatal("a single manifest was accepted for multiple purposes")
	}
}
