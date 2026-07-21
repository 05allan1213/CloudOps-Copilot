package agent

import (
	"fmt"
	"sort"
	"strings"
)

type CollectionStatus string

const (
	CollectionAvailable   CollectionStatus = "available"
	CollectionNoData      CollectionStatus = "no_data"
	CollectionPartial     CollectionStatus = "partial"
	CollectionUnavailable CollectionStatus = "unavailable"
	CollectionInvalid     CollectionStatus = "invalid"
)

type EvidenceFact struct {
	ID                 string            `json:"fact_id"`
	EvidenceID         string            `json:"evidence_id"`
	IncidentID         string            `json:"incident_id"`
	CycleNo            uint64            `json:"cycle_no"`
	Type               string            `json:"type"`
	SourceSystem       string            `json:"source_system"`
	CollectionPath     string            `json:"collection_path"`
	CorroborationGroup string            `json:"corroboration_group"`
	Authority          string            `json:"authority"`
	Integrity          string            `json:"integrity"`
	Freshness          string            `json:"freshness"`
	Completeness       string            `json:"completeness"`
	ClaimUse           string            `json:"claim_use"`
	CollectionStatus   CollectionStatus  `json:"collection_status"`
	Direct             bool              `json:"direct"`
	Truncated          bool              `json:"truncated"`
	MigratedLegacy     bool              `json:"migrated_legacy,omitempty"`
	DerivedFrom        []string          `json:"derived_from,omitempty"`
	Attributes         map[string]string `json:"attributes,omitempty"`
}

type FactRequirement struct {
	Facet string   `json:"facet"`
	AnyOf []string `json:"any_of"`
}

type ClaimPolicy struct {
	Version                  string            `json:"version"`
	ClaimType                string            `json:"claim_type"`
	Requirements             []FactRequirement `json:"requirements"`
	BlockingFactTypes        []string          `json:"blocking_fact_types"`
	MinIndependentCollectors int               `json:"min_independent_collectors"`
	RequireDirectFact        bool              `json:"require_direct_fact"`
}

type SufficiencyOutcome string

const (
	SufficiencyContinue     SufficiencyOutcome = "CONTINUE"
	SufficiencyReady        SufficiencyOutcome = "READY_FOR_DIAGNOSIS"
	SufficiencyInsufficient SufficiencyOutcome = "INSUFFICIENT_EVIDENCE"
)

type SufficiencyInput struct {
	IncidentID                 string
	CycleNo                    uint64
	Facts                      []EvidenceFact
	Policy                     ClaimPolicy
	BudgetExhausted            bool
	RequiredSourcesUnavailable []string
}

type SufficiencyResult struct {
	Outcome       SufficiencyOutcome `json:"outcome"`
	MissingFacets []string           `json:"missing_facets"`
	ConfidenceCap float64            `json:"confidence_cap"`
	ReasonCodes   []string           `json:"reason_codes"`
	SupportingIDs []string           `json:"supporting_fact_ids"`
	BlockingIDs   []string           `json:"blocking_fact_ids"`
}

// GoldenRequiredEnvClaimPolicy freezes the deterministic evidence truth table
// for required_env_config_regression/v1. Runbooks and model summaries never
// satisfy a facet.
func GoldenRequiredEnvClaimPolicy() ClaimPolicy {
	return ClaimPolicy{
		Version:   "required-env-claim-policy/v1",
		ClaimType: "required_env_config_regression/v1",
		Requirements: []FactRequirement{
			{Facet: "subject", AnyOf: []string{"workload.subject_confirmed"}},
			{Facet: "desired-change", AnyOf: []string{"gitops.required_env_removed"}},
			{Facet: "deployed-argo", AnyOf: []string{"argocd.bad_revision_deployed"}},
			{Facet: "deployed-kubernetes", AnyOf: []string{"kubernetes.required_env_absent"}},
			{Facet: "source-identity", AnyOf: []string{"source_revision.unchanged"}},
			{Facet: "image-identity", AnyOf: []string{"image_digest.unchanged"}},
			{Facet: "metric-symptom", AnyOf: []string{"metric.readiness_or_5xx_failure"}},
			{Facet: "log-symptom", AnyOf: []string{"log.required_env_missing"}},
			{Facet: "trace-symptom", AnyOf: []string{"trace.request_failure"}},
		},
		BlockingFactTypes: []string{
			"kubernetes.required_env_present",
			"argocd.bad_revision_not_deployed",
			"deployment.source_and_image_changed",
		},
		MinIndependentCollectors: 2,
		RequireDirectFact:        true,
	}
}

// EvaluateSufficiency applies only deterministic policy. Model confidence and
// stop proposals are deliberately absent from the input.
func EvaluateSufficiency(input SufficiencyInput) (SufficiencyResult, error) {
	if strings.TrimSpace(input.IncidentID) == "" || input.CycleNo == 0 {
		return SufficiencyResult{}, fmt.Errorf("%w: incident cycle is required", ErrInvalidArgument)
	}
	if err := validateClaimPolicy(input.Policy); err != nil {
		return SufficiencyResult{}, err
	}

	usable := make([]EvidenceFact, 0, len(input.Facts))
	blocking := make([]string, 0)
	for _, fact := range input.Facts {
		if fact.IncidentID != input.IncidentID || fact.CycleNo != input.CycleNo {
			continue
		}
		if !usableFact(fact) {
			continue
		}
		if containsString(input.Policy.BlockingFactTypes, fact.Type) {
			blocking = append(blocking, fact.ID)
			continue
		}
		usable = append(usable, fact)
	}

	result := SufficiencyResult{Outcome: SufficiencyContinue, ConfidenceCap: 0.49}
	result.BlockingIDs = stableUnique(blocking)
	if len(result.BlockingIDs) > 0 {
		result.Outcome = SufficiencyInsufficient
		result.ConfidenceCap = 0
		result.ReasonCodes = []string{"blocking_contradiction"}
		return result, nil
	}

	byType := make(map[string][]EvidenceFact)
	for _, fact := range usable {
		byType[fact.Type] = append(byType[fact.Type], fact)
	}
	selected := make([]EvidenceFact, 0, len(input.Policy.Requirements))
	for _, requirement := range input.Policy.Requirements {
		var chosen *EvidenceFact
		for _, factType := range requirement.AnyOf {
			for index := range byType[factType] {
				fact := &byType[factType][index]
				if chosen == nil || fact.ID < chosen.ID {
					chosen = fact
				}
			}
		}
		if chosen == nil {
			result.MissingFacets = append(result.MissingFacets, requirement.Facet)
			continue
		}
		selected = append(selected, *chosen)
		result.SupportingIDs = append(result.SupportingIDs, chosen.ID)
	}

	collectors := independentCollectors(selected)
	if len(collectors) < input.Policy.MinIndependentCollectors {
		result.ReasonCodes = append(result.ReasonCodes, "insufficient_independent_collectors")
	}
	if input.Policy.RequireDirectFact && !hasDirectFact(selected) {
		result.ReasonCodes = append(result.ReasonCodes, "direct_fact_missing")
	}
	if len(result.MissingFacets) > 0 {
		result.ReasonCodes = append(result.ReasonCodes, "required_facets_missing")
	}
	result.MissingFacets = stableUnique(result.MissingFacets)
	result.SupportingIDs = stableUnique(result.SupportingIDs)
	result.ReasonCodes = stableUnique(result.ReasonCodes)

	if len(result.MissingFacets) == 0 && len(result.ReasonCodes) == 0 {
		result.Outcome = SufficiencyReady
		result.ConfidenceCap = 1
		return result, nil
	}
	if input.BudgetExhausted || len(input.RequiredSourcesUnavailable) > 0 {
		result.Outcome = SufficiencyInsufficient
		result.ConfidenceCap = 0.3
		if input.BudgetExhausted {
			result.ReasonCodes = append(result.ReasonCodes, "budget_exhausted")
		}
		if len(input.RequiredSourcesUnavailable) > 0 {
			result.ReasonCodes = append(result.ReasonCodes, "required_source_unavailable")
		}
		result.ReasonCodes = stableUnique(result.ReasonCodes)
	}
	return result, nil
}

func validateClaimPolicy(policy ClaimPolicy) error {
	if strings.TrimSpace(policy.Version) == "" || strings.TrimSpace(policy.ClaimType) == "" || len(policy.Requirements) == 0 || policy.MinIndependentCollectors < 1 {
		return fmt.Errorf("%w: invalid claim policy", ErrInvalidArgument)
	}
	seen := make(map[string]struct{}, len(policy.Requirements))
	for _, requirement := range policy.Requirements {
		if strings.TrimSpace(requirement.Facet) == "" || len(requirement.AnyOf) == 0 {
			return fmt.Errorf("%w: invalid fact requirement", ErrInvalidArgument)
		}
		if _, ok := seen[requirement.Facet]; ok {
			return fmt.Errorf("%w: duplicate fact requirement", ErrInvalidArgument)
		}
		seen[requirement.Facet] = struct{}{}
		for _, factType := range requirement.AnyOf {
			if strings.TrimSpace(factType) == "" {
				return fmt.Errorf("%w: empty fact type", ErrInvalidArgument)
			}
		}
	}
	return nil
}

func usableFact(fact EvidenceFact) bool {
	return fact.ID != "" && fact.EvidenceID != "" && fact.Type != "" && fact.SourceSystem != "" &&
		fact.CollectionPath != "" && fact.CorroborationGroup != "" && fact.CollectionStatus == CollectionAvailable &&
		fact.Integrity == "verified" && fact.Freshness == "fresh" && fact.Completeness == "complete" &&
		fact.ClaimUse != "forbidden" && !fact.Truncated && !fact.MigratedLegacy
}

func independentCollectors(facts []EvidenceFact) map[string]struct{} {
	collectors := make(map[string]struct{})
	seenParents := make(map[string]struct{})
	for _, fact := range facts {
		parents := stableUnique(fact.DerivedFrom)
		sharedParent := false
		for _, parent := range parents {
			if _, ok := seenParents[parent]; ok {
				sharedParent = true
				break
			}
		}
		if sharedParent {
			continue
		}
		for _, parent := range parents {
			seenParents[parent] = struct{}{}
		}
		collectors[fact.SourceSystem+"\x00"+fact.CollectionPath] = struct{}{}
	}
	return collectors
}

func hasDirectFact(facts []EvidenceFact) bool {
	for _, fact := range facts {
		if fact.Direct {
			return true
		}
	}
	return false
}

func stableUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
