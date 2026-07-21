package taskhandler

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

const (
	evidenceContractVersion = 1
	evidenceFactSchema      = 1

	evidenceProducerAgentStep           = "agent_step"
	evidenceProducerVerificationCheck   = "verification_check"
	evidenceProducerDeliveryObservation = "delivery_observation"
	evidenceProducerSystemEnrichment    = "system_enrichment"
)

type durableEvidenceMetadata struct {
	FactSchemaHash      string
	ProvenanceJSON      json.RawMessage
	ProvenanceHash      string
	TrustAxesJSON       json.RawMessage
	ClaimUse            string
	CorroborationGroups json.RawMessage
	InputEvidenceIDs    json.RawMessage
	InputSampleIDs      json.RawMessage
	InputHashes         json.RawMessage
	RedactionCounts     json.RawMessage
	PromptSafetyFlags   json.RawMessage
}

func buildDurableEvidenceMetadata(facts []agent.EvidenceFact, provenance map[string]string, inputEvidenceIDs, inputSampleIDs, inputHashes []string) (durableEvidenceMetadata, error) {
	if len(facts) == 0 || len(facts) > 64 || len(inputHashes) != len(inputEvidenceIDs)+len(inputSampleIDs) {
		return durableEvidenceMetadata{}, errors.New("durable Evidence facts or derived inputs are inconsistent")
	}
	authority, integrity, freshness, completeness := []string{}, []string{}, []string{}, []string{}
	claimUses, corroboration := []string{}, []string{}
	for _, fact := range facts {
		if strings.TrimSpace(fact.Authority) == "" || strings.TrimSpace(fact.Integrity) == "" ||
			strings.TrimSpace(fact.Freshness) == "" || strings.TrimSpace(fact.Completeness) == "" ||
			strings.TrimSpace(fact.CorroborationGroup) == "" {
			return durableEvidenceMetadata{}, errors.New("durable Evidence trust axes are incomplete")
		}
		authority = append(authority, fact.Authority)
		integrity = append(integrity, fact.Integrity)
		freshness = append(freshness, fact.Freshness)
		completeness = append(completeness, fact.Completeness)
		claimUses = append(claimUses, normalizedEvidenceClaimUse(fact.ClaimUse))
		corroboration = append(corroboration, fact.CorroborationGroup)
	}
	authority, integrity = stableUniqueInvestigation(authority), stableUniqueInvestigation(integrity)
	freshness, completeness = stableUniqueInvestigation(freshness), stableUniqueInvestigation(completeness)
	claimUses, corroboration = stableUniqueInvestigation(claimUses), stableUniqueInvestigation(corroboration)
	if slices.Contains(claimUses, "") || len(corroboration) == 0 {
		return durableEvidenceMetadata{}, errors.New("durable Evidence claim use or corroboration is invalid")
	}
	claimUse := "mixed"
	if len(claimUses) == 1 {
		claimUse = claimUses[0]
	}
	provenanceJSON, err := canonicalEvidenceJSON(nonNilStringMap(provenance))
	if err != nil {
		return durableEvidenceMetadata{}, err
	}
	trustJSON, err := canonicalEvidenceJSON(map[string]any{
		"authority": authority, "integrity": integrity, "freshness": freshness, "completeness": completeness,
	})
	if err != nil {
		return durableEvidenceMetadata{}, err
	}
	corroborationJSON, _ := canonicalEvidenceJSON(corroboration)
	inputEvidenceJSON, _ := canonicalEvidenceJSON(stableUniqueInvestigation(inputEvidenceIDs))
	inputSampleJSON, _ := canonicalEvidenceJSON(stableUniqueInvestigation(inputSampleIDs))
	inputHashesJSON, _ := canonicalEvidenceJSON(stableUniqueInvestigation(inputHashes))
	if jsonArrayLength(inputHashesJSON) != jsonArrayLength(inputEvidenceJSON)+jsonArrayLength(inputSampleJSON) {
		return durableEvidenceMetadata{}, errors.New("durable Evidence input identities and hashes diverge")
	}
	return durableEvidenceMetadata{
		FactSchemaHash: hashCanonical("evidence-fact-schema", "agent.EvidenceFact/v1", "typed-facts-envelope/v1"),
		ProvenanceJSON: provenanceJSON, ProvenanceHash: hashBytesInvestigation(provenanceJSON),
		TrustAxesJSON: trustJSON, ClaimUse: claimUse, CorroborationGroups: corroborationJSON,
		InputEvidenceIDs: inputEvidenceJSON, InputSampleIDs: inputSampleJSON, InputHashes: inputHashesJSON,
		RedactionCounts:   json.RawMessage(`{"raw_text_omitted":1,"secrets_omitted":1}`),
		PromptSafetyFlags: json.RawMessage(`{"instruction_untrusted_omitted":true,"raw_natural_language_omitted":true}`),
	}, nil
}

func canonicalEvidenceJSON(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil || !json.Valid(encoded) {
		return nil, errors.New("durable Evidence metadata is not canonical JSON")
	}
	return encoded, nil
}

func nonNilStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return value
}

func normalizedEvidenceClaimUse(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "support", "supporting", "allowed":
		return "support"
	case "blocking":
		return "blocking"
	case "context":
		return "context"
	case "forbidden":
		return "forbidden"
	default:
		return ""
	}
}

func jsonArrayLength(value json.RawMessage) int {
	var items []any
	if json.Unmarshal(value, &items) != nil {
		return -1
	}
	return len(items)
}

func decodeEvidenceStringArray(value json.RawMessage) []string {
	var result []string
	if json.Unmarshal(value, &result) != nil {
		return nil
	}
	return result
}

func evidenceTemplateIdentity(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndex(value, "/")
	if separator <= 0 || separator == len(value)-1 {
		return "", "", errors.New("durable Evidence query template identity is not versioned")
	}
	return value[:separator], value[separator+1:], nil
}
