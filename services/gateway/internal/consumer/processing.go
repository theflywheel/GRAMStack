package consumer

import (
	"fmt"
	"strings"
)

type ProcessingProfile struct {
	ID                      string   `json:"id"`
	Version                 string   `json:"version"`
	ResponsibleOrganisation string   `json:"responsible_organisation"`
	Processors              []string `json:"processors"`
	DerivedDataHandling     string   `json:"derived_data_handling"`
}

func (p ProcessingProfile) terms(text string) (string, error) {
	if !idPattern.MatchString(p.ID) || !idPattern.MatchString(p.Version) ||
		!idPattern.MatchString(p.ResponsibleOrganisation) || p.Processors == nil ||
		len(p.Processors) > 16 || strings.TrimSpace(p.DerivedDataHandling) == "" ||
		len(p.DerivedDataHandling) > 2048 {
		return "", ErrInput
	}
	seen := make(map[string]bool, len(p.Processors))
	for _, processor := range p.Processors {
		if !idPattern.MatchString(processor) || seen[processor] {
			return "", ErrInput
		}
		seen[processor] = true
	}
	processors := "None declared"
	if len(p.Processors) > 0 {
		processors = strings.Join(p.Processors, ", ")
	}
	return fmt.Sprintf("%s\nProcessing profile: %s (version %s)\nResponsible organisation: %s\nDeclared processors: %s\nDerived data handling: %s",
		text, p.ID, p.Version, p.ResponsibleOrganisation, processors, p.DerivedDataHandling), nil
}
