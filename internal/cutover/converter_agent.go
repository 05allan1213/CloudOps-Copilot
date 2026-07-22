package cutover

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

const (
	AgentCheckpointConverterVersion = "agent-checkpoint/v2"
	agentCheckpointSourceSchema     = 1
	agentCheckpointTargetSchema     = agent.InvestigationStateSchemaVersion
	agentCheckpointHardLimit        = 128 * 1024
)

type LegacyAgentEvidence struct {
	PublicID       string
	IncidentID     string
	CycleNo        uint64
	FactType       string
	Fresh          bool
	Valid          bool
	ContentHash    string
	CollectedAt    time.Time
	MigratedLegacy bool
}

type AgentCheckpointInput struct {
	SourceSchemaVersion uint32
	TargetSchemaVersion uint32
	RunPublicID         string
	IncidentPublicID    string
	CycleNo             uint64
	IncidentVersion     uint64
	CheckpointVersion   uint64
	CheckpointHash      string
	Checkpoint          json.RawMessage
	Limits              agent.Limits
	Usage               agent.Usage
	Evidence            []LegacyAgentEvidence
	CompletedSignatures []string
	MaxCheckpointBytes  int
}

type AgentCheckpointConversion struct {
	ConverterVersion        string
	Compatible              bool
	ReasonCode              string
	InputHash               string
	CheckpointCanonicalHash string
	OutputHash              string
	Output                  json.RawMessage
	State                   agent.InvestigationState
	NextMode                string
}

func ConvertAgentCheckpoint(input AgentCheckpointInput) AgentCheckpointConversion {
	result := AgentCheckpointConversion{ConverterVersion: AgentCheckpointConverterVersion}
	checkpointComponent := string(input.Checkpoint)
	if canonical, err := canonicalJSON(input.Checkpoint); err == nil {
		checkpointComponent = string(canonical)
	}
	result.InputHash = canonicalHashFields(
		AgentCheckpointConverterVersion,
		fmt.Sprint(input.SourceSchemaVersion),
		fmt.Sprint(input.TargetSchemaVersion),
		input.RunPublicID,
		input.IncidentPublicID,
		fmt.Sprint(input.CycleNo),
		fmt.Sprint(input.IncidentVersion),
		fmt.Sprint(input.CheckpointVersion),
		input.CheckpointHash,
		checkpointComponent,
		canonicalComponent(input.Limits),
		canonicalComponent(input.Usage),
		canonicalComponent(input.Evidence),
		canonicalComponent(input.CompletedSignatures),
		fmt.Sprint(input.MaxCheckpointBytes),
	)
	fail := func(code string) AgentCheckpointConversion {
		result.ReasonCode = code
		result.OutputHash = canonicalHashFields(AgentCheckpointConverterVersion, "failed", code, result.InputHash)
		return result
	}
	if input.SourceSchemaVersion != agentCheckpointSourceSchema || input.TargetSchemaVersion != agentCheckpointTargetSchema {
		return fail("checkpoint_schema_version_unsupported")
	}
	if strings.TrimSpace(input.RunPublicID) == "" || strings.TrimSpace(input.IncidentPublicID) == "" || input.CycleNo == 0 || input.IncidentVersion == 0 {
		return fail("checkpoint_ownership_missing")
	}
	limit := input.MaxCheckpointBytes
	if limit <= 0 {
		limit = input.Limits.MaxCheckpointSize
	}
	if limit <= 0 || limit > agentCheckpointHardLimit {
		limit = agentCheckpointHardLimit
	}
	if len(input.Checkpoint) == 0 || len(input.Checkpoint) > limit || !json.Valid(input.Checkpoint) {
		return fail("checkpoint_size_or_json_invalid")
	}
	if input.CheckpointVersion == 0 || !isSHA256(input.CheckpointHash) {
		return fail("checkpoint_metadata_missing")
	}
	digest := sha256.Sum256(input.Checkpoint)
	if hex.EncodeToString(digest[:]) != input.CheckpointHash {
		return fail("checkpoint_hash_mismatch")
	}
	canonicalHash, _, err := canonicalHashJSON(input.Checkpoint)
	if err != nil {
		return fail("checkpoint_canonical_hash_invalid")
	}
	result.CheckpointCanonicalHash = canonicalHash
	if err := validateAgentBudget(input.Usage, input.Limits); err != nil {
		return fail("checkpoint_budget_invalid")
	}
	evidence, err := validateAgentEvidence(input)
	if err != nil {
		return fail(err.Error())
	}
	state, err := decodeLegacyAgentState(input, evidence)
	if err != nil {
		return fail(err.Error())
	}
	if err := validateConvertedAgentState(input, state); err != nil {
		return fail(err.Error())
	}
	if err := validateCompletedSignatures(state.ToolAttempts, input.CompletedSignatures); err != nil {
		return fail(err.Error())
	}
	encoded, err := json.Marshal(state)
	if err != nil || len(encoded) > limit {
		return fail("checkpoint_output_too_large")
	}
	outputHash, canonicalOutput, err := canonicalHashJSON(encoded)
	if err != nil || len(canonicalOutput) > limit {
		return fail("checkpoint_output_canonicalization_failed")
	}
	result.Compatible = true
	result.ReasonCode = "checkpoint_converted"
	result.Output = canonicalOutput
	result.OutputHash = outputHash
	result.State = state
	result.NextMode = nextAgentMode(state.NextNode)
	return result
}

func validateAgentEvidence(input AgentCheckpointInput) ([]agent.EvidenceReference, error) {
	seen := make(map[string]struct{}, len(input.Evidence))
	result := make([]agent.EvidenceReference, 0, len(input.Evidence))
	for _, item := range input.Evidence {
		if item.PublicID == "" || item.IncidentID != input.IncidentPublicID || item.CycleNo != input.CycleNo {
			return nil, errors.New("checkpoint_evidence_ownership_invalid")
		}
		if !item.Valid || !item.Fresh || item.CollectedAt.IsZero() || !isSHA256(item.ContentHash) || strings.TrimSpace(item.FactType) == "" {
			return nil, errors.New("checkpoint_evidence_stale_or_invalid")
		}
		if _, duplicate := seen[item.PublicID]; duplicate {
			return nil, errors.New("checkpoint_evidence_duplicate")
		}
		seen[item.PublicID] = struct{}{}
		result = append(result, agent.EvidenceReference{ID: item.PublicID, FactType: item.FactType})
	}
	slices.SortFunc(result, func(left, right agent.EvidenceReference) int { return strings.Compare(left.ID, right.ID) })
	return result, nil
}

func decodeLegacyAgentState(input AgentCheckpointInput, evidence []agent.EvidenceReference) (agent.InvestigationState, error) {
	var direct agent.InvestigationState
	if strictJSONDecode(input.Checkpoint, &direct) == nil && direct.SchemaVersion == agentCheckpointTargetSchema {
		direct.Evidence = mergeEvidenceReferences(direct.Evidence, evidence)
		return direct, nil
	}
	var graph agent.GraphState
	if err := strictJSONDecode(input.Checkpoint, &graph); err != nil {
		return agent.InvestigationState{}, errors.New("checkpoint_fields_invalid")
	}
	if graph.Investigation != nil {
		state := *graph.Investigation
		state.Evidence = mergeEvidenceReferences(state.Evidence, evidence)
		return state, nil
	}
	state := agent.InvestigationState{
		SchemaVersion:   agentCheckpointTargetSchema,
		RunID:           graph.RunPublicID,
		IncidentID:      graph.IncidentPublicID,
		CycleNo:         input.CycleNo,
		IncidentVersion: input.IncidentVersion,
		Correlation: agent.CorrelationSnapshot{
			Cluster: graph.Incident.Cluster, Environment: "", Namespace: graph.Incident.Namespace,
			Workload: graph.Incident.TargetName, TargetKind: graph.Incident.TargetKind,
		},
		Objective: graph.Objective,
		Window:    agent.QueryWindow{From: graph.StartedAt.UTC(), To: graph.DeadlineAt.UTC()},
		Coverage: agent.CoverageRequirements{
			ClaimType:          "migrated-legacy-investigation/v1",
			ClaimPolicyVersion: "migrated-legacy-audit/v1",
			ClaimPolicyHash:    canonicalHashFields("migrated-legacy-audit/v1", input.RunPublicID),
			ActionPolicyHash:   canonicalHashFields("migrated-legacy-read-only/v1", input.IncidentPublicID),
			RequiredFacets:     []string{"subject", "runtime"},
		},
		Evidence:          evidence,
		Usage:             graph.Usage,
		Limits:            graph.Limits,
		NextNode:          mapLegacyAgentNode(graph.NextNode),
		TerminalOutcome:   legacyGraphTerminalOutcome(graph),
		CheckpointVersion: graph.CheckpointVersion,
		UpdatedAt:         graph.StartedAt.UTC(),
	}
	if state.RunID == "" {
		state.RunID = input.RunPublicID
	}
	if state.IncidentID == "" {
		state.IncidentID = input.IncidentPublicID
	}
	if state.CheckpointVersion == 0 {
		state.CheckpointVersion = input.CheckpointVersion
	}
	if state.Usage == (agent.Usage{}) {
		state.Usage = input.Usage
	}
	if state.Limits == (agent.Limits{}) {
		state.Limits = input.Limits
	}
	if state.Window.From.IsZero() {
		state.Window.From = graph.StartedAt.UTC()
	}
	if state.Window.To.IsZero() || state.Window.To.Before(state.Window.From) {
		state.Window.To = state.Window.From
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = state.Window.To
	}
	for index, question := range graph.Plan.Questions {
		state.Questions = append(state.Questions, agent.OpenQuestion{ID: fmt.Sprintf("legacy-question-%d", index+1), Question: question})
	}
	for _, observation := range graph.Observations {
		if observation.Tool == "" || observation.ArgumentsHash == "" {
			continue
		}
		state.ToolAttempts = append(state.ToolAttempts, agent.ToolAttempt{
			Signature: canonicalHashFields("legacy-tool-signature/v1", observation.Tool, observation.ArgumentsHash),
			Tool:      observation.Tool, Status: "completed", Attempted: observation.ObservedAt.UTC(),
		})
	}
	return state, nil
}

func strictJSONDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateConvertedAgentState(input AgentCheckpointInput, state agent.InvestigationState) error {
	if state.SchemaVersion != agentCheckpointTargetSchema || state.RunID != input.RunPublicID || state.IncidentID != input.IncidentPublicID ||
		state.CycleNo != input.CycleNo || state.IncidentVersion != input.IncidentVersion || state.CheckpointVersion != input.CheckpointVersion {
		return errors.New("checkpoint_identity_or_version_invalid")
	}
	if state.Usage != input.Usage || state.Limits != input.Limits {
		return errors.New("checkpoint_budget_mapping_mismatch")
	}
	if strings.TrimSpace(state.Objective) == "" || len(state.Objective) > 2048 || state.Window.From.IsZero() ||
		state.Window.To.IsZero() || state.Window.To.Before(state.Window.From) || state.UpdatedAt.IsZero() {
		return errors.New("checkpoint_fields_invalid")
	}
	if strings.TrimSpace(state.Correlation.Cluster) == "" || strings.TrimSpace(state.Correlation.Namespace) == "" ||
		strings.TrimSpace(state.Correlation.Workload) == "" || strings.TrimSpace(state.Correlation.TargetKind) == "" {
		return errors.New("checkpoint_scope_invalid")
	}
	if strings.TrimSpace(state.Coverage.ClaimType) == "" || strings.TrimSpace(state.Coverage.ClaimPolicyVersion) == "" ||
		!isSHA256(state.Coverage.ClaimPolicyHash) || !isSHA256(state.Coverage.ActionPolicyHash) ||
		len(state.Coverage.RequiredFacets) == 0 {
		return errors.New("checkpoint_state_delta_mapping_invalid")
	}
	if state.Usage.Evidence != len(input.Evidence) || len(state.Evidence) != len(input.Evidence) {
		return errors.New("checkpoint_evidence_usage_mismatch")
	}
	ownedEvidence := make(map[string]string, len(input.Evidence))
	for _, item := range input.Evidence {
		ownedEvidence[item.PublicID] = item.FactType
	}
	for _, reference := range state.Evidence {
		if ownedEvidence[reference.ID] != reference.FactType {
			return errors.New("checkpoint_evidence_ownership_invalid")
		}
	}
	for _, question := range state.Questions {
		if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Question) == "" {
			return errors.New("checkpoint_state_delta_mapping_invalid")
		}
	}
	if !validAgentNextNode(state.NextNode) {
		return errors.New("checkpoint_next_node_invalid")
	}
	if state.NextNode == agent.NodeExecuteTool {
		return errors.New("checkpoint_pending_tool_action_unrecoverable")
	}
	seen := make(map[string]struct{}, len(state.ToolAttempts))
	for _, attempt := range state.ToolAttempts {
		if !isSHA256(attempt.Signature) || strings.TrimSpace(attempt.Tool) == "" ||
			(attempt.Status == "completed" && attempt.Attempted.IsZero()) {
			return errors.New("checkpoint_tool_signature_invalid")
		}
		if _, duplicate := seen[attempt.Signature]; duplicate {
			return errors.New("checkpoint_tool_signature_duplicate")
		}
		seen[attempt.Signature] = struct{}{}
	}
	return validateAgentBudget(state.Usage, state.Limits)
}

func validateCompletedSignatures(attempts []agent.ToolAttempt, completed []string) error {
	seen := make(map[string]struct{}, len(completed))
	for _, signature := range completed {
		if !isSHA256(signature) {
			return errors.New("checkpoint_completed_signature_invalid")
		}
		if _, duplicate := seen[signature]; duplicate {
			return errors.New("checkpoint_completed_signature_duplicate")
		}
		seen[signature] = struct{}{}
	}
	checkpointCompleted := make(map[string]struct{})
	for _, attempt := range attempts {
		if attempt.Status != "completed" {
			continue
		}
		checkpointCompleted[attempt.Signature] = struct{}{}
		if _, ok := seen[attempt.Signature]; !ok {
			return errors.New("checkpoint_completed_signature_missing")
		}
	}
	for signature := range seen {
		if _, ok := checkpointCompleted[signature]; !ok {
			return errors.New("checkpoint_completed_signature_unmapped")
		}
	}
	return nil
}

func validateAgentBudget(usage agent.Usage, limits agent.Limits) error {
	if limits.MaxSteps <= 0 || limits.MaxToolCalls <= 0 || limits.MaxModelCalls <= 0 || limits.TokenBudget <= 0 ||
		limits.MaxEvidenceItems <= 0 || limits.MaxRuntime <= 0 || limits.ToolTimeout <= 0 || limits.MaxEvidenceBytes <= 0 ||
		limits.MaxCheckpointSize <= 0 || limits.MaxCheckpointSize > agentCheckpointHardLimit || limits.MaxStepRetries < 0 {
		return errors.New("budget_limits_invalid")
	}
	if usage.Steps < 0 || usage.ToolCalls < 0 || usage.ModelCalls < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.Evidence < 0 {
		return errors.New("budget_usage_negative")
	}
	return usage.CanCharge(agent.Usage{}, limits)
}

func mapLegacyAgentNode(node agent.Node) agent.Node {
	switch node {
	case agent.NodeSelectAction, agent.NodeExecuteTool, agent.NodeProduceDiagnosis, agent.NodeEnd:
		return node
	case agent.NodeLoadIncident, agent.NodeBuildObjective, agent.NodePlanInvestigation,
		agent.NodePersistObservation, agent.NodeEvaluateCoverage, agent.NodeReplan:
		return agent.NodeSelectAction
	case agent.NodeValidateDiagnosis, agent.NodeCompleteRun:
		return agent.NodeProduceDiagnosis
	case agent.NodeBudgetExceeded, agent.NodeCancelled, agent.NodeTerminalFailure:
		return agent.NodeEnd
	default:
		return node
	}
}

func legacyGraphTerminalOutcome(graph agent.GraphState) string {
	if graph.NextNode != agent.NodeEnd {
		return ""
	}
	if strings.TrimSpace(graph.Diagnosis.Summary) != "" {
		return "diagnosed"
	}
	return "insufficient_evidence"
}

func validAgentNextNode(node agent.Node) bool {
	return node == agent.NodeSelectAction || node == agent.NodeExecuteTool || node == agent.NodeProduceDiagnosis || node == agent.NodeEnd
}

func nextAgentMode(node agent.Node) string {
	switch node {
	case agent.NodeExecuteTool:
		return "tool"
	case agent.NodeProduceDiagnosis:
		return "synthesize"
	default:
		return "decide"
	}
}

func mergeEvidenceReferences(left, right []agent.EvidenceReference) []agent.EvidenceReference {
	values := append(append([]agent.EvidenceReference(nil), left...), right...)
	slices.SortFunc(values, func(a, b agent.EvidenceReference) int { return strings.Compare(a.ID, b.ID) })
	result := values[:0]
	for _, item := range values {
		if len(result) == 0 || result[len(result)-1].ID != item.ID {
			result = append(result, item)
		}
	}
	return result
}
