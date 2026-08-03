package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TypedEvidenceContractVersion   = 1
	TypedEvidenceFactSchemaVersion = 1
	TypedEvidenceProducerAgentStep = "agent_step"
	TypedEvidenceProducerVersion   = "agent-step-evidence/v1"
)

// TypedEvidenceDocument is the durable subset required to decide whether an
// Evidence row may participate in deterministic diagnosis authority.
type TypedEvidenceDocument struct {
	PublicID               string
	ContractVersion        int
	ProducerType           string
	ProducerID             string
	ProducerVersion        string
	ProducerDedupeKey      string
	AgentStepID            uint64
	FactSchemaVersion      int
	FactSchemaHash         string
	Facts                  json.RawMessage
	Provenance             json.RawMessage
	ProvenanceHash         string
	TrustAxes              json.RawMessage
	ClaimUse               string
	CorroborationGroups    json.RawMessage
	InputEvidenceIDs       json.RawMessage
	InputSampleIDs         json.RawMessage
	InputHashes            json.RawMessage
	ResultHash             string
	ContentHash            string
	RedactionPolicyVersion string
	RedactionCounts        json.RawMessage
	PromptSafetyFlags      json.RawMessage
	Truncated              bool
	Valid                  bool
	MigratedLegacy         bool
	MigratedLegacyContext  bool
	ObservedAt             time.Time
	CollectedAt            time.Time
}

type TypedEvidenceExpectation struct {
	IncidentID            string
	CycleNo               uint64
	MigratedLegacy        bool
	MigratedLegacyContext bool
}

type TypedEvidenceEnvelope struct {
	SchemaVersion    int               `json:"schema_version"`
	Status           CollectionStatus  `json:"status"`
	SourceSystem     string            `json:"source_system"`
	CollectionPath   string            `json:"collection_path"`
	TemplateVersion  string            `json:"template_version"`
	Summary          string            `json:"summary"`
	Facts            []EvidenceFact    `json:"facts"`
	Truncated        bool              `json:"truncated"`
	Provenance       map[string]string `json:"provenance,omitempty"`
	SafeDeepLink     string            `json:"safe_deep_link,omitempty"`
	InputEvidenceIDs []string          `json:"input_evidence_ids,omitempty"`
	InputSampleIDs   []string          `json:"input_sample_ids,omitempty"`
	InputHashes      []string          `json:"input_hashes,omitempty"`
	ContentHash      string            `json:"content_hash"`
}

// TypedEvidenceFactSchemaHash returns the immutable schema identity used by
// investigation Evidence. A different hash denotes context-only data.
func TypedEvidenceFactSchemaHash() string {
	return typedEvidenceHashCanonical("evidence-fact-schema", "agent.EvidenceFact/v1", "typed-facts-envelope/v1")
}

// DecodeTypedEvidence validates the complete durable authority envelope before
// returning facts. Any mismatch fails closed; callers must not salvage a
// partial subset from a rejected document.
func DecodeTypedEvidence(document TypedEvidenceDocument, expected TypedEvidenceExpectation) ([]EvidenceFact, error) {
	if strings.TrimSpace(expected.IncidentID) == "" || expected.CycleNo == 0 {
		return nil, fmt.Errorf("%w: typed Evidence incident cycle is required", ErrInvalidArgument)
	}
	if document.ContractVersion != TypedEvidenceContractVersion ||
		document.ProducerType != TypedEvidenceProducerAgentStep || document.AgentStepID == 0 ||
		strings.TrimSpace(document.ProducerID) == "" || document.ProducerVersion != TypedEvidenceProducerVersion ||
		strings.TrimSpace(document.ProducerDedupeKey) == "" ||
		document.FactSchemaVersion != TypedEvidenceFactSchemaVersion || document.FactSchemaHash != TypedEvidenceFactSchemaHash() ||
		!document.Valid || document.Truncated || !typedEvidenceSHA256(document.ContentHash) ||
		document.ResultHash != document.ContentHash || !typedEvidenceSHA256(document.ProvenanceHash) ||
		document.RedactionPolicyVersion != "observation-redaction/v1" ||
		document.ObservedAt.IsZero() || document.CollectedAt.IsZero() || document.ObservedAt.After(document.CollectedAt) ||
		document.MigratedLegacy != expected.MigratedLegacy || document.MigratedLegacyContext != expected.MigratedLegacyContext {
		return nil, fmt.Errorf("%w: typed Evidence durable identity is invalid", ErrPermission)
	}

	jsonFields := []struct {
		name  string
		value json.RawMessage
	}{
		{name: "facts", value: document.Facts},
		{name: "provenance", value: document.Provenance},
		{name: "trust axes", value: document.TrustAxes},
		{name: "corroboration groups", value: document.CorroborationGroups},
		{name: "input Evidence IDs", value: document.InputEvidenceIDs},
		{name: "input sample IDs", value: document.InputSampleIDs},
		{name: "input hashes", value: document.InputHashes},
		{name: "redaction counts", value: document.RedactionCounts},
		{name: "prompt safety flags", value: document.PromptSafetyFlags},
	}
	canonical := make(map[string]json.RawMessage, len(jsonFields))
	for _, field := range jsonFields {
		value, err := canonicalTypedEvidenceJSON(field.value)
		if err != nil {
			return nil, fmt.Errorf("%w: canonicalize typed Evidence %s: %v", ErrPermission, field.name, err)
		}
		canonical[field.name] = value
	}
	if typedEvidenceHash(canonical["provenance"]) != document.ProvenanceHash {
		return nil, fmt.Errorf("%w: typed Evidence provenance hash diverges", ErrPermission)
	}

	var envelope TypedEvidenceEnvelope
	if err := json.Unmarshal(canonical["facts"], &envelope); err != nil ||
		envelope.SchemaVersion != TypedEvidenceFactSchemaVersion || envelope.Status != CollectionAvailable ||
		envelope.Truncated || envelope.ContentHash != document.ContentHash || len(envelope.Facts) == 0 || len(envelope.Facts) > 64 ||
		!slices.Equal(stableDiagnosisStrings(envelope.InputEvidenceIDs), decodeTypedEvidenceStrings(canonical["input Evidence IDs"])) ||
		!slices.Equal(stableDiagnosisStrings(envelope.InputSampleIDs), decodeTypedEvidenceStrings(canonical["input sample IDs"])) ||
		!slices.Equal(stableDiagnosisStrings(envelope.InputHashes), decodeTypedEvidenceStrings(canonical["input hashes"])) {
		return nil, fmt.Errorf("%w: typed Evidence envelope is invalid", ErrPermission)
	}
	metadata, err := typedEvidenceMetadata(envelope.Facts, envelope.Provenance, envelope.InputEvidenceIDs, envelope.InputSampleIDs, envelope.InputHashes)
	if err != nil || metadata.provenanceHash != document.ProvenanceHash ||
		!bytes.Equal(metadata.provenance, canonical["provenance"]) || !bytes.Equal(metadata.trustAxes, canonical["trust axes"]) ||
		metadata.claimUse != document.ClaimUse || !bytes.Equal(metadata.corroborationGroups, canonical["corroboration groups"]) ||
		!bytes.Equal(metadata.inputEvidenceIDs, canonical["input Evidence IDs"]) ||
		!bytes.Equal(metadata.inputSampleIDs, canonical["input sample IDs"]) || !bytes.Equal(metadata.inputHashes, canonical["input hashes"]) {
		return nil, fmt.Errorf("%w: typed Evidence trust or provenance metadata diverges", ErrPermission)
	}

	facts := make([]EvidenceFact, 0, len(envelope.Facts))
	seen := make(map[string]struct{}, len(envelope.Facts))
	for _, fact := range envelope.Facts {
		fact.ID = strings.TrimSpace(fact.ID)
		if fact.ID == "" || fact.EvidenceID != document.PublicID || fact.IncidentID != expected.IncidentID ||
			fact.CycleNo != expected.CycleNo || fact.MigratedLegacy != expected.MigratedLegacy ||
			fact.CollectionStatus != envelope.Status || strings.TrimSpace(fact.Type) == "" {
			return nil, fmt.Errorf("%w: typed Evidence fact ownership is invalid", ErrPermission)
		}
		if _, duplicate := seen[fact.ID]; duplicate {
			return nil, fmt.Errorf("%w: typed Evidence fact identity is duplicated", ErrPermission)
		}
		seen[fact.ID] = struct{}{}
		facts = append(facts, fact)
	}
	return facts, nil
}

type typedEvidenceMetadataValues struct {
	provenance          json.RawMessage
	provenanceHash      string
	trustAxes           json.RawMessage
	claimUse            string
	corroborationGroups json.RawMessage
	inputEvidenceIDs    json.RawMessage
	inputSampleIDs      json.RawMessage
	inputHashes         json.RawMessage
}

type workspaceTypedEvidenceValues struct {
	facts    json.RawMessage
	metadata typedEvidenceMetadataValues
}

func buildWorkspaceTypedEvidence(
	observation WorkspaceToolObservation,
	evidencePublicID, incidentPublicID, runPublicID, stepPublicID, snapshotHash, contentHash string,
	cycleNo uint64,
	migratedLegacy bool,
) (workspaceTypedEvidenceValues, error) {
	if len(observation.TypedFacts) == 0 || len(observation.TypedFacts) > 64 ||
		strings.TrimSpace(evidencePublicID) == "" || strings.TrimSpace(incidentPublicID) == "" || cycleNo == 0 ||
		strings.TrimSpace(runPublicID) == "" || strings.TrimSpace(stepPublicID) == "" ||
		!typedEvidenceSHA256(snapshotHash) || !typedEvidenceSHA256(contentHash) {
		return workspaceTypedEvidenceValues{}, errors.New("Workspace typed Evidence identity is incomplete")
	}
	facts := make([]EvidenceFact, len(observation.TypedFacts))
	for index, candidate := range observation.TypedFacts {
		candidate.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf(
			"workspace-fact\x00%s\x00%s\x00%d", evidencePublicID, candidate.Type, index,
		))).String()
		candidate.EvidenceID = evidencePublicID
		candidate.IncidentID = incidentPublicID
		candidate.CycleNo = cycleNo
		candidate.CollectionStatus = CollectionAvailable
		candidate.MigratedLegacy = migratedLegacy
		candidate.Truncated = false
		candidate.DerivedFrom = stableDiagnosisStrings(candidate.DerivedFrom)
		if !usableFact(candidate) || candidate.ClaimUse != "support" && candidate.ClaimUse != "blocking" {
			return workspaceTypedEvidenceValues{}, fmt.Errorf("Workspace typed Evidence candidate %d is invalid", index)
		}
		candidate.Attributes = cloneEvidenceAttributes(candidate.Attributes)
		facts[index] = candidate
	}
	provenance := map[string]string{
		"agent_run_id": runPublicID, "agent_step_id": stepPublicID, "collector": observation.Tool,
		"provider": observation.Source, "resource_ref": observation.ResourceRef, "scope_snapshot_hash": snapshotHash,
	}
	if revision := strings.TrimSpace(observation.SourceRevision); revision != "" {
		provenance["source_revision"] = revision
	}
	metadata, err := typedEvidenceMetadata(facts, provenance, nil, nil, nil)
	if err != nil {
		return workspaceTypedEvidenceValues{}, err
	}
	envelope, err := json.Marshal(TypedEvidenceEnvelope{
		SchemaVersion: TypedEvidenceFactSchemaVersion, Status: CollectionAvailable,
		SourceSystem: observation.Source, CollectionPath: observation.Tool, TemplateVersion: WorkspaceToolVersion,
		Summary: observation.Summary, Facts: facts, Truncated: false, Provenance: provenance, ContentHash: contentHash,
	})
	if err != nil {
		return workspaceTypedEvidenceValues{}, err
	}
	return workspaceTypedEvidenceValues{facts: envelope, metadata: metadata}, nil
}

func cloneEvidenceAttributes(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "" || value == "" || len(key) > 128 || len(value) > 1024 {
			continue
		}
		result[key] = value
	}
	return result
}

func typedEvidenceMetadata(facts []EvidenceFact, provenance map[string]string, inputEvidenceIDs, inputSampleIDs, inputHashes []string) (typedEvidenceMetadataValues, error) {
	if len(facts) == 0 || len(facts) > 64 || len(inputHashes) != len(inputEvidenceIDs)+len(inputSampleIDs) {
		return typedEvidenceMetadataValues{}, errors.New("typed Evidence facts or inputs are inconsistent")
	}
	authority, integrity, freshness, completeness := []string{}, []string{}, []string{}, []string{}
	claimUses, corroboration := []string{}, []string{}
	for _, fact := range facts {
		if strings.TrimSpace(fact.Authority) == "" || strings.TrimSpace(fact.Integrity) == "" ||
			strings.TrimSpace(fact.Freshness) == "" || strings.TrimSpace(fact.Completeness) == "" ||
			strings.TrimSpace(fact.CorroborationGroup) == "" {
			return typedEvidenceMetadataValues{}, errors.New("typed Evidence trust axes are incomplete")
		}
		authority = append(authority, fact.Authority)
		integrity = append(integrity, fact.Integrity)
		freshness = append(freshness, fact.Freshness)
		completeness = append(completeness, fact.Completeness)
		claimUses = append(claimUses, normalizedTypedEvidenceClaimUse(fact.ClaimUse))
		corroboration = append(corroboration, fact.CorroborationGroup)
	}
	authority, integrity = stableDiagnosisStrings(authority), stableDiagnosisStrings(integrity)
	freshness, completeness = stableDiagnosisStrings(freshness), stableDiagnosisStrings(completeness)
	claimUses, corroboration = stableDiagnosisStrings(claimUses), stableDiagnosisStrings(corroboration)
	if slices.Contains(claimUses, "") || len(corroboration) == 0 {
		return typedEvidenceMetadataValues{}, errors.New("typed Evidence claim use or corroboration is invalid")
	}
	claimUse := "mixed"
	if len(claimUses) == 1 {
		claimUse = claimUses[0]
	}
	if provenance == nil {
		provenance = map[string]string{}
	}
	provenanceJSON, _ := json.Marshal(provenance)
	trustJSON, _ := json.Marshal(map[string]any{
		"authority": authority, "integrity": integrity, "freshness": freshness, "completeness": completeness,
	})
	corroborationJSON, _ := json.Marshal(corroboration)
	inputEvidenceJSON, _ := json.Marshal(stableDiagnosisStrings(inputEvidenceIDs))
	inputSampleJSON, _ := json.Marshal(stableDiagnosisStrings(inputSampleIDs))
	inputHashesJSON, _ := json.Marshal(stableDiagnosisStrings(inputHashes))
	return typedEvidenceMetadataValues{
		provenance: provenanceJSON, provenanceHash: typedEvidenceHash(provenanceJSON), trustAxes: trustJSON,
		claimUse: claimUse, corroborationGroups: corroborationJSON, inputEvidenceIDs: inputEvidenceJSON,
		inputSampleIDs: inputSampleJSON, inputHashes: inputHashesJSON,
	}, nil
}

func canonicalTypedEvidenceJSON(raw []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON data")
	}
	return json.Marshal(value)
}

func decodeTypedEvidenceStrings(raw json.RawMessage) []string {
	var result []string
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	return result
}

func normalizedTypedEvidenceClaimUse(value string) string {
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

func typedEvidenceHashCanonical(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(strings.TrimSpace(part)))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func typedEvidenceHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func typedEvidenceSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
