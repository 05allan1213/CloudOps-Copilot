package taskhandler

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

func TestBuildDurableEvidenceMetadataFreezesTrustAndDerivedInputs(t *testing.T) {
	facts := []agent.EvidenceFact{
		{Authority: "authoritative", Integrity: "verified", Freshness: "fresh", Completeness: "complete", ClaimUse: "support", CorroborationGroup: "desired-state"},
		{Authority: "runtime_observation", Integrity: "verified", Freshness: "fresh", Completeness: "complete", ClaimUse: "blocking", CorroborationGroup: "runtime"},
	}
	metadata, err := buildDurableEvidenceMetadata(facts, map[string]string{"source": "fixture"}, []string{"evidence-1"}, []string{"sample-1"}, []string{strings.Repeat("a", 64), strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ClaimUse != "mixed" || !validSHA256Text(metadata.FactSchemaHash) || !validSHA256Text(metadata.ProvenanceHash) {
		t.Fatalf("metadata=%+v", metadata)
	}
	if jsonArrayLength(metadata.InputEvidenceIDs) != 1 || jsonArrayLength(metadata.InputSampleIDs) != 1 || jsonArrayLength(metadata.InputHashes) != 2 {
		t.Fatalf("derived inputs evidence=%s samples=%s hashes=%s", metadata.InputEvidenceIDs, metadata.InputSampleIDs, metadata.InputHashes)
	}
}

func TestCanonicalEvidenceRawJSONNormalizesMySQLEncoding(t *testing.T) {
	fromGo := json.RawMessage(`{"agent_step_id":"step-1","cycle_no":9007199254740993,"source_system":"kubernetes"}`)
	fromMySQL := []byte(`{ "source_system": "kubernetes", "cycle_no": 9007199254740993, "agent_step_id": "step-1" }`)
	canonical, err := canonicalEvidenceRawJSON(fromMySQL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, fromGo) {
		t.Fatalf("canonical=%s want=%s", canonical, fromGo)
	}
	if hashBytesInvestigation(canonical) != hashBytesInvestigation(fromGo) {
		t.Fatal("canonical MySQL JSON hash differs from the persisted Go JSON hash")
	}
	if _, err := canonicalEvidenceRawJSON([]byte(`{"source_system":"kubernetes"} {"extra":true}`)); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
}

func TestNormalizeObservationRequiresDurableCurrentCycleDerivedInputs(t *testing.T) {
	snapshot := testInvestigationSnapshot(t, stepModeTool, nil)
	parent := agent.EvidenceFact{
		ID: "fact-parent", EvidenceID: "evidence-parent", IncidentID: snapshot.IncidentPublicID,
		CycleNo: uint64(snapshot.Task.CycleNo), Type: "metric.parent", SourceSystem: "prometheus",
		CollectionPath: "prometheus/parent", CorroborationGroup: "runtime", Authority: "runtime_observation",
		Integrity: "verified", Freshness: "fresh", Completeness: "complete", ClaimUse: "support",
		CollectionStatus: agent.CollectionAvailable, Direct: true,
	}
	snapshot.Facts = []agent.EvidenceFact{parent}
	snapshot.Evidence[parent.EvidenceID] = agent.EvidenceRecord{PublicID: parent.EvidenceID, ResultHash: strings.Repeat("a", 64), Valid: true}
	action := agent.ProposedAction{Tool: "inspect_workload", ScopeRef: snapshot.ScopeRef, TemplateID: "workload-snapshot/v1", ExpectedFactTypes: []string{"workload.derived"}}
	observation := agent.ToolObservation{
		Status: agent.CollectionAvailable, SourceSystem: "kubernetes", CollectionPath: "kubernetes/workload",
		TemplateVersion: action.TemplateID, Summary: "derived fixture",
		Facts: []agent.EvidenceFact{{ID: "fact-derived", Type: "workload.derived", CorroborationGroup: "runtime", Authority: "runtime_observation", Integrity: "verified", Freshness: "fresh", Completeness: "complete", ClaimUse: "support", CollectionStatus: agent.CollectionAvailable, DerivedFrom: []string{parent.ID}}},
	}
	normalized, err := normalizeObservation(observation, snapshot, action, "evidence-derived")
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.InputEvidenceIDs) != 1 || normalized.InputEvidenceIDs[0] != parent.EvidenceID || len(normalized.InputHashes) != 1 {
		t.Fatalf("derived bindings=%+v", normalized)
	}
	var envelope storedEvidenceEnvelope
	encoded, _ := evidenceEnvelope(normalized)
	if json.Unmarshal(encoded, &envelope) != nil || len(envelope.InputEvidenceIDs) != 1 {
		t.Fatalf("durable envelope=%s", encoded)
	}

	observation.Facts[0].DerivedFrom = []string{"missing-fact"}
	if _, err := normalizeObservation(observation, snapshot, action, "evidence-invalid"); err == nil {
		t.Fatal("derived Evidence without a current-cycle durable input was accepted")
	}
}
