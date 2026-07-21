package taskhandler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/change"
)

const (
	investigationStepPayloadSchema    = 1
	investigationTaskCheckpointSchema = 1
	investigationRunCheckpointSchema  = 1
	defaultTaskCheckpointBytes        = 128 * 1024

	stepModeDecide     = "decide"
	stepModeTool       = "tool"
	stepModeSynthesize = "synthesize"
)

// InvestigationTaskStore is the only task-side persistence used by the V3
// step operation. asyncjob.Repository implements it, preserving the unified
// task lease as the sole claim and checkpoint fence.
type InvestigationTaskStore interface {
	Checkpoint(context.Context, asyncjob.Lease, asyncjob.Checkpoint, asyncjob.Mutation) error
	EnqueueIn(context.Context, asyncjob.DBTX, asyncjob.NewTask) (*asyncjob.Task, error)
}

type InvestigationStepConfig struct {
	DB                 *sql.DB
	Tasks              InvestigationTaskStore
	Model              agent.InvestigationModel
	Tools              agent.InvestigationReadTool
	ClaimPolicy        agent.ClaimPolicy
	ActionPolicies     map[string]agent.ToolActionPolicy
	RequiredSources    []string
	MaxCheckpointBytes int
	Now                func() time.Time
}

// NewInvestigationStep constructs the real subject-bound operation. The
// production registry still refuses to start until the other four owning
// operations are supplied; this constructor does not weaken that gate.
func NewInvestigationStep(config InvestigationStepConfig) (Operation, error) {
	if config.DB == nil || config.Tasks == nil || config.Model == nil || config.Tools == nil {
		return nil, errors.New("investigation.step requires MySQL, task store, model, and read-tool adapters")
	}
	if err := validateInvestigationPolicy(config.ClaimPolicy, config.ActionPolicies); err != nil {
		return nil, err
	}
	if config.MaxCheckpointBytes == 0 {
		config.MaxCheckpointBytes = defaultTaskCheckpointBytes
	}
	if config.MaxCheckpointBytes < 1024 || config.MaxCheckpointBytes > defaultTaskCheckpointBytes {
		return nil, errors.New("investigation.step checkpoint limit must be between 1024 and 131072 bytes")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	config.ActionPolicies = cloneActionPolicies(config.ActionPolicies)
	config.RequiredSources = stableUniqueInvestigation(config.RequiredSources)
	operation := &investigationStepOperation{
		cfg:    config,
		loader: mysqlInvestigationLoader{db: config.DB},
	}
	return operation.handle, nil
}

type investigationStepOperation struct {
	cfg    InvestigationStepConfig
	loader investigationLoader
}

type investigationLoader interface {
	Load(context.Context, asyncjob.Task) (investigationSnapshot, error)
}

type investigationSnapshot struct {
	Task                    asyncjob.Task
	RunPublicID             string
	LegacyStatus            string
	Status                  string
	Objective               string
	Model                   string
	PromptVersion           string
	Limits                  agent.Limits
	Usage                   agent.Usage
	RunCheckpoint           json.RawMessage
	RunCheckpointVersion    uint64
	RunCheckpointSchema     int
	RunCheckpointHash       string
	RunVersion              uint64
	ExpectedIncidentVersion uint64
	CancelRequestedAt       *time.Time
	DeadlineAt              time.Time
	RunCreatedAt            time.Time
	IncidentPublicID        string
	IncidentVersion         uint64
	CorrelationKey          string
	Cluster                 string
	Environment             string
	Namespace               string
	ServiceName             string
	TargetKind              string
	TargetName              string
	FirstSeenAt             time.Time
	Facts                   []agent.EvidenceFact
	Evidence                map[string]agent.EvidenceRecord
	State                   agent.InvestigationState
	StateHash               string
	ScopeRef                string
}

type investigationStepPayload struct {
	Mode                   string                `json:"mode"`
	AgentRunID             string                `json:"agent_run_id"`
	CycleNo                uint64                `json:"cycle_no"`
	BasisCheckpointVersion uint64                `json:"basis_checkpoint_version,omitempty"`
	Action                 *agent.ProposedAction `json:"action,omitempty"`
}

type investigationStepCheckpoint struct {
	SchemaVersion          uint32                   `json:"schema_version"`
	Mode                   string                   `json:"mode"`
	SubjectVersion         uint64                   `json:"subject_version"`
	BasisCheckpointVersion uint64                   `json:"basis_checkpoint_version"`
	BasisCheckpointHash    string                   `json:"basis_checkpoint_hash"`
	State                  agent.InvestigationState `json:"state"`
	StateHash              string                   `json:"state_hash"`
	Delta                  *agent.StateDelta        `json:"delta,omitempty"`
	Action                 *agent.ProposedAction    `json:"action,omitempty"`
	Observation            *agent.ToolObservation   `json:"observation,omitempty"`
	Diagnosis              *agent.DiagnosisRecord   `json:"diagnosis,omitempty"`
	Sufficiency            agent.SufficiencyResult  `json:"sufficiency"`
	Usage                  agent.Usage              `json:"usage"`
	StepNode               agent.Node               `json:"step_node"`
	StepSummary            string                   `json:"step_summary"`
	TerminalOutcome        string                   `json:"terminal_outcome,omitempty"`
	NextMode               string                   `json:"next_mode,omitempty"`
	NextAction             *agent.ProposedAction    `json:"next_action,omitempty"`
	DurationMS             int64                    `json:"duration_ms"`
	CapturedAt             time.Time                `json:"captured_at"`
}

func (o *investigationStepOperation) handle(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
	task := execution.Task
	if err := execution.Lease.Validate(); err != nil {
		return asyncjob.Dead("invalid_task_lease", "investigation.step lease is invalid", nil)
	}
	if dispatchKey(task) != investigationStepKey || task.SubjectID == 0 || task.CycleNo == 0 ||
		task.Queue != asyncjob.QueueInvestigate ||
		task.ExpectedSubjectVersion == 0 || task.PayloadSchemaVersion != investigationStepPayloadSchema ||
		execution.Lease.TaskID != task.ID || execution.Lease.ExpectedSubjectVersion != task.ExpectedSubjectVersion {
		return asyncjob.Dead("invalid_task_subject", "investigation.step task identity is invalid", nil)
	}

	snapshot, err := o.loader.Load(ctx, task)
	if err != nil {
		return investigationLoadFailure(err)
	}
	if err := o.bindPolicy(&snapshot); err != nil {
		return asyncjob.Dead("checkpoint_policy_mismatch", boundInvestigation(err.Error(), 2048), o.failRunMutation(snapshot, "checkpoint_policy_mismatch"))
	}
	payload, err := decodeInvestigationStepPayload(task.Payload, snapshot)
	if err != nil {
		return asyncjob.Dead("invalid_step_payload", boundInvestigation(err.Error(), 2048), nil)
	}
	if payload.Action != nil {
		if err := validateInvestigationToolAction(*payload.Action, snapshot, o.cfg.ActionPolicies); err != nil {
			return asyncjob.Dead("invalid_step_payload", boundInvestigation(err.Error(), 2048), nil)
		}
	}
	if snapshot.CancelRequestedAt != nil {
		prepared, prepareErr := o.prepareInsufficient(snapshot, payload, "cancel_requested", o.cfg.Now())
		if prepareErr != nil {
			return asyncjob.Dead("checkpoint_invariant", boundInvestigation(prepareErr.Error(), 2048), o.failRunMutation(snapshot, "checkpoint_invariant"))
		}
		return o.persistAndResolve(ctx, execution, snapshot, prepared)
	}
	if !snapshot.DeadlineAt.IsZero() && !o.cfg.Now().Before(snapshot.DeadlineAt) {
		prepared, prepareErr := o.prepareInsufficient(snapshot, payload, "run_deadline_exhausted", o.cfg.Now())
		if prepareErr != nil {
			return asyncjob.Dead("checkpoint_invariant", boundInvestigation(prepareErr.Error(), 2048), o.failRunMutation(snapshot, "checkpoint_invariant"))
		}
		return o.persistAndResolve(ctx, execution, snapshot, prepared)
	}

	if len(task.Checkpoint) > 0 {
		prepared, decodeErr := decodeInvestigationTaskCheckpoint(task, snapshot, payload)
		if decodeErr != nil {
			return asyncjob.Dead("corrupt_task_checkpoint", boundInvestigation(decodeErr.Error(), 2048), o.failRunMutation(snapshot, "corrupt_task_checkpoint"))
		}
		return o.resolvePrepared(snapshot, prepared)
	}

	prepared, err := o.executeOne(ctx, execution, snapshot, payload)
	if err != nil {
		return o.executionFailure(ctx, snapshot, payload, execution, err)
	}
	return o.persistAndResolve(ctx, execution, snapshot, prepared)
}

func (o *investigationStepOperation) bindPolicy(snapshot *investigationSnapshot) error {
	claimJSON, err := json.Marshal(o.cfg.ClaimPolicy)
	if err != nil {
		return fmt.Errorf("encode claim policy: %w", err)
	}
	actionJSON, err := json.Marshal(o.cfg.ActionPolicies)
	if err != nil {
		return fmt.Errorf("encode action policy: %w", err)
	}
	claimHash, actionHash := hashBytesInvestigation(claimJSON), hashBytesInvestigation(actionJSON)
	facets := make([]string, 0, len(o.cfg.ClaimPolicy.Requirements))
	for _, requirement := range o.cfg.ClaimPolicy.Requirements {
		facets = append(facets, requirement.Facet)
	}
	slices.Sort(facets)
	requiredSources := slices.Clone(o.cfg.RequiredSources)
	slices.Sort(requiredSources)
	if snapshot.State.CheckpointVersion == 0 {
		snapshot.State.Coverage = agent.CoverageRequirements{
			ClaimType: o.cfg.ClaimPolicy.ClaimType, ClaimPolicyVersion: o.cfg.ClaimPolicy.Version,
			ClaimPolicyHash: claimHash, ActionPolicyHash: actionHash,
			RequiredFacets: facets, RequiredSources: requiredSources,
		}
		encoded, err := json.Marshal(snapshot.State)
		if err != nil {
			return err
		}
		snapshot.StateHash = hashBytesInvestigation(encoded)
		return nil
	}
	if snapshot.State.Coverage.ClaimType != o.cfg.ClaimPolicy.ClaimType ||
		snapshot.State.Coverage.ClaimPolicyVersion != o.cfg.ClaimPolicy.Version ||
		snapshot.State.Coverage.ClaimPolicyHash != claimHash || snapshot.State.Coverage.ActionPolicyHash != actionHash ||
		!slices.Equal(snapshot.State.Coverage.RequiredFacets, facets) ||
		!slices.Equal(snapshot.State.Coverage.RequiredSources, requiredSources) {
		return errors.New("run checkpoint is bound to a different investigation policy")
	}
	return nil
}

func validateInvestigationPolicy(claim agent.ClaimPolicy, actions map[string]agent.ToolActionPolicy) error {
	if strings.TrimSpace(claim.Version) == "" || strings.TrimSpace(claim.ClaimType) == "" || len(claim.Requirements) == 0 || claim.MinIndependentCollectors < 1 {
		return errors.New("investigation.step requires a versioned claim policy")
	}
	if len(actions) == 0 {
		return errors.New("investigation.step requires an explicit read-tool allowlist")
	}
	for name, policy := range actions {
		if strings.TrimSpace(name) == "" || len(policy.TemplateIDs) == 0 || len(policy.ExpectedFactTypes) == 0 {
			return fmt.Errorf("invalid investigation tool policy %q", name)
		}
	}
	return nil
}

func validateInvestigationToolAction(action agent.ProposedAction, snapshot investigationSnapshot, policies map[string]agent.ToolActionPolicy) error {
	policy, ok := policies[action.Tool]
	if !ok || strings.TrimSpace(action.Tool) == "" || strings.TrimSpace(action.TemplateID) == "" {
		return fmt.Errorf("%w: action is not allowlisted", agent.ErrPermission)
	}
	if action.ScopeRef != snapshot.ScopeRef || strings.TrimSpace(action.ScopeRef) == "" {
		return fmt.Errorf("%w: action scope is outside the Incident", agent.ErrPermission)
	}
	if !slices.Contains(policy.TemplateIDs, action.TemplateID) {
		return fmt.Errorf("%w: action template is not allowlisted", agent.ErrPermission)
	}
	if len(action.BoundedParameters) == 0 || !json.Valid(action.BoundedParameters) {
		return fmt.Errorf("%w: action parameters must be valid JSON", agent.ErrInvalidArgument)
	}
	var parameters map[string]json.RawMessage
	if err := json.Unmarshal(action.BoundedParameters, &parameters); err != nil || parameters == nil {
		return fmt.Errorf("%w: action parameters must be a JSON object", agent.ErrInvalidArgument)
	}
	for key := range parameters {
		if !slices.Contains(policy.ParameterKeys, key) {
			return fmt.Errorf("%w: action parameter %q is not allowlisted", agent.ErrPermission, key)
		}
	}
	if strings.TrimSpace(action.PurposeSummary) == "" || len(action.ExpectedFactTypes) == 0 {
		return fmt.Errorf("%w: action purpose and expected facts are required", agent.ErrInvalidArgument)
	}
	for _, factType := range action.ExpectedFactTypes {
		if !slices.Contains(policy.ExpectedFactTypes, factType) {
			return fmt.Errorf("%w: expected fact type %q is not allowlisted", agent.ErrPermission, factType)
		}
	}
	return nil
}

func decodeInvestigationStepPayload(raw json.RawMessage, snapshot investigationSnapshot) (investigationStepPayload, error) {
	var payload investigationStepPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload, fmt.Errorf("decode payload: %w", err)
	}
	if payload.AgentRunID != snapshot.RunPublicID || payload.CycleNo != uint64(snapshot.Task.CycleNo) {
		return payload, errors.New("payload AgentRun or cycle does not match task subject")
	}
	if payload.Mode == "step" {
		payload.Mode = stepModeDecide
	}
	switch payload.Mode {
	case stepModeDecide:
		if payload.Action != nil {
			return payload, errors.New("decide payload cannot contain an action")
		}
	case stepModeTool:
		if payload.Action == nil {
			return payload, errors.New("tool payload requires an action")
		}
		if _, err := agent.ActionSignature(*payload.Action); err != nil {
			return payload, err
		}
	case stepModeSynthesize:
		if payload.Action != nil {
			return payload, errors.New("synthesize payload cannot contain an action")
		}
	default:
		return payload, fmt.Errorf("unsupported step mode %q", payload.Mode)
	}
	if payload.BasisCheckpointVersion != snapshot.State.CheckpointVersion {
		return payload, errors.New("payload basis checkpoint does not match the run")
	}
	return payload, nil
}

// mysqlInvestigationLoader reads only immutable/run projection data. It never
// claims or leases an AgentRun; the async task lease remains the sole fence.
type mysqlInvestigationLoader struct{ db *sql.DB }

func (l mysqlInvestigationLoader) Load(ctx context.Context, task asyncjob.Task) (investigationSnapshot, error) {
	if l.db == nil {
		return investigationSnapshot{}, errors.New("investigation MySQL loader is not configured")
	}
	const runQuery = `SELECT
 r.public_id, r.status, COALESCE(r.v3_status,''), r.objective, r.model, r.prompt_version,
 r.max_steps, r.used_steps, r.max_tool_calls, r.used_tool_calls,
 r.max_model_calls, r.used_model_calls, r.token_budget, r.input_tokens, r.output_tokens,
 r.max_evidence_items, r.used_evidence_items, r.max_runtime_ms, r.tool_timeout_ms,
 r.max_evidence_bytes, r.max_checkpoint_bytes, r.max_step_retries,
 r.current_checkpoint, r.checkpoint_version, r.checkpoint_schema_version, r.checkpoint_hash,
 r.row_version, r.expected_incident_version, r.cancel_requested_at, r.deadline_at, r.created_at,
 i.public_id, i.version, i.correlation_key, i.cluster, i.environment, i.namespace,
 i.service_name, i.target_kind, i.target_name, i.first_seen_at
FROM agent_runs r
JOIN incidents i ON i.id = r.incident_id
WHERE r.id = ? AND r.incident_id = ? AND r.domain_schema_version = 3
  AND r.cycle_no = ? AND r.expected_incident_version > 0
  AND i.domain_schema_version = 3 AND i.cycle_no = ? AND i.v3_status = 'investigating'`
	var result investigationSnapshot
	result.Task = task
	var checkpoint []byte
	var checkpointVersion uint64
	var checkpointSchema int
	var checkpointHash string
	var cancelAt, deadline sql.NullTime
	var firstSeen sql.NullTime
	var status, v3Status string
	var maxSteps, usedSteps, maxTools, usedTools, maxModels, usedModels int
	var tokenBudget, inputTokens, outputTokens int64
	var maxEvidence, usedEvidence, maxRuntime, toolTimeout, maxEvidenceBytes, maxCheckpointBytes, maxRetries int64
	if err := l.db.QueryRowContext(ctx, runQuery, task.SubjectID, task.IncidentID, task.CycleNo, task.CycleNo).Scan(
		&result.RunPublicID, &status, &v3Status, &result.Objective, &result.Model, &result.PromptVersion,
		&maxSteps, &usedSteps, &maxTools, &usedTools, &maxModels, &usedModels, &tokenBudget, &inputTokens, &outputTokens,
		&maxEvidence, &usedEvidence, &maxRuntime, &toolTimeout, &maxEvidenceBytes, &maxCheckpointBytes, &maxRetries,
		&checkpoint, &checkpointVersion, &checkpointSchema, &checkpointHash, &result.RunVersion,
		&result.ExpectedIncidentVersion, &cancelAt, &deadline, &result.RunCreatedAt,
		&result.IncidentPublicID, &result.IncidentVersion, &result.CorrelationKey, &result.Cluster, &result.Environment,
		&result.Namespace, &result.ServiceName, &result.TargetKind, &result.TargetName, &firstSeen,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return investigationSnapshot{}, asyncjob.ErrSubjectVersionMismatch
		}
		return investigationSnapshot{}, fmt.Errorf("load investigation.step subject: %w", err)
	}
	if result.RunPublicID == "" || result.IncidentPublicID == "" || result.RunVersion != task.ExpectedSubjectVersion ||
		result.IncidentVersion != result.ExpectedIncidentVersion {
		return investigationSnapshot{}, asyncjob.ErrSubjectVersionMismatch
	}
	if status != "PENDING" && status != "RUNNING" || v3Status != "pending" && v3Status != "running" {
		return investigationSnapshot{}, fmt.Errorf("%w: AgentRun is not active", asyncjob.ErrInvalidMutation)
	}
	result.LegacyStatus, result.Status = status, v3Status
	result.RunCheckpoint, result.RunCheckpointVersion = append([]byte(nil), checkpoint...), checkpointVersion
	result.RunCheckpointSchema, result.RunCheckpointHash = checkpointSchema, strings.ToLower(strings.TrimSpace(checkpointHash))
	if cancelAt.Valid {
		value := cancelAt.Time.UTC()
		result.CancelRequestedAt = &value
	}
	if deadline.Valid {
		result.DeadlineAt = deadline.Time.UTC()
	}
	if firstSeen.Valid {
		result.FirstSeenAt = firstSeen.Time.UTC()
	} else {
		result.FirstSeenAt = result.RunCreatedAt.UTC()
	}
	result.Limits = agent.Limits{
		MaxSteps: maxSteps, MaxToolCalls: maxTools, MaxModelCalls: maxModels, TokenBudget: tokenBudget,
		MaxEvidenceItems: int(maxEvidence), MaxRuntime: time.Duration(maxRuntime) * time.Millisecond,
		ToolTimeout: time.Duration(toolTimeout) * time.Millisecond, MaxEvidenceBytes: int(maxEvidenceBytes),
		MaxCheckpointSize: int(maxCheckpointBytes), MaxStepRetries: int(maxRetries),
	}
	result.Usage = agent.Usage{Steps: usedSteps, ToolCalls: usedTools, ModelCalls: usedModels, InputTokens: inputTokens, OutputTokens: outputTokens, Evidence: int(usedEvidence)}
	result.ScopeRef = hashCanonical("incident-scope", result.IncidentPublicID, fmt.Sprint(task.CycleNo))
	result.Evidence = make(map[string]agent.EvidenceRecord)
	if err := l.loadEvidence(ctx, &result); err != nil {
		return investigationSnapshot{}, err
	}
	state, stateHash, err := decodeRunState(result)
	if err != nil {
		return investigationSnapshot{}, err
	}
	result.State, result.StateHash = state, stateHash
	return result, nil
}

func (l mysqlInvestigationLoader) loadEvidence(ctx context.Context, snapshot *investigationSnapshot) error {
	const evidenceQuery = `SELECT public_id, source, tool_name, summary, facts_json,
 result_hash, raw_ref, redaction_json, truncated, valid, idempotency_key, collected_at
FROM evidence_items
WHERE incident_id = ? AND agent_run_id = ? AND domain_schema_version = 3 AND cycle_no = ?
ORDER BY collected_at, id`
	rows, err := l.db.QueryContext(ctx, evidenceQuery, snapshot.Task.IncidentID, snapshot.Task.SubjectID, snapshot.Task.CycleNo)
	if err != nil {
		return fmt.Errorf("load investigation evidence: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var publicID, source, toolName, summary, resultHash, rawRef string
		var factsJSON, redactionJSON []byte
		var truncated, valid bool
		var idempotency sql.NullString
		var collected time.Time
		if err := rows.Scan(&publicID, &source, &toolName, &summary, &factsJSON, &resultHash, &rawRef, &redactionJSON, &truncated, &valid, &idempotency, &collected); err != nil {
			return fmt.Errorf("scan investigation evidence: %w", err)
		}
		record := agent.EvidenceRecord{PublicID: publicID, IncidentID: snapshot.Task.IncidentID, RunID: snapshot.Task.SubjectID, SourceType: source, ToolName: toolName, Summary: summary, Facts: append([]byte(nil), factsJSON...), ResultHash: resultHash, RawRef: rawRef, Redaction: append([]byte(nil), redactionJSON...), Truncated: truncated, Valid: valid, CollectedAt: collected.UTC()}
		if idempotency.Valid {
			record.IdempotencyKey = idempotency.String
		}
		snapshot.Evidence[publicID] = record
		var envelope storedEvidenceEnvelope
		if json.Unmarshal(factsJSON, &envelope) != nil || envelope.SchemaVersion != 1 {
			continue
		}
		for _, fact := range envelope.Facts {
			if fact.ID == "" || fact.EvidenceID == "" || fact.IncidentID != snapshot.IncidentPublicID || fact.CycleNo != uint64(snapshot.Task.CycleNo) {
				continue
			}
			snapshot.Facts = append(snapshot.Facts, fact)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate investigation evidence: %w", err)
	}
	return nil
}

type storedEvidenceEnvelope struct {
	SchemaVersion   int                    `json:"schema_version"`
	Status          agent.CollectionStatus `json:"status"`
	SourceSystem    string                 `json:"source_system"`
	CollectionPath  string                 `json:"collection_path"`
	TemplateVersion string                 `json:"template_version"`
	Summary         string                 `json:"summary"`
	Facts           []agent.EvidenceFact   `json:"facts"`
	Truncated       bool                   `json:"truncated"`
	Provenance      map[string]string      `json:"provenance,omitempty"`
	SafeDeepLink    string                 `json:"safe_deep_link,omitempty"`
	ContentHash     string                 `json:"content_hash"`
}

func decodeRunState(snapshot investigationSnapshot) (agent.InvestigationState, string, error) {
	if len(snapshot.RunCheckpoint) == 0 {
		if snapshot.RunCheckpointVersion != 0 || snapshot.RunCheckpointHash != "" {
			return agent.InvestigationState{}, "", errors.New("run checkpoint metadata exists without a payload")
		}
		state := agent.InvestigationState{
			SchemaVersion:   investigationRunCheckpointSchema,
			RunID:           snapshot.RunPublicID,
			IncidentID:      snapshot.IncidentPublicID,
			CycleNo:         uint64(snapshot.Task.CycleNo),
			IncidentVersion: snapshot.ExpectedIncidentVersion,
			Correlation: agent.CorrelationSnapshot{
				Cluster: snapshot.Cluster, Environment: snapshot.Environment, Namespace: snapshot.Namespace,
				Workload: snapshot.ServiceName + "/" + snapshot.TargetName, TargetKind: snapshot.TargetKind,
			},
			Objective: snapshot.Objective,
			Window:    agent.QueryWindow{From: snapshot.FirstSeenAt.UTC(), To: snapshot.RunCreatedAt.UTC()},
			Coverage: agent.CoverageRequirements{
				ClaimType: snapshotClaimType(snapshot), RequiredFacets: claimFacets(snapshot),
			},
			Usage: snapshot.Usage, Limits: snapshot.Limits, NextNode: agent.NodeSelectAction,
			CheckpointVersion: 0, UpdatedAt: snapshot.RunCreatedAt.UTC(),
		}
		if state.Window.To.Before(state.Window.From) {
			state.Window.From = state.Window.To
		}
		encoded, err := json.Marshal(state)
		if err != nil {
			return agent.InvestigationState{}, "", fmt.Errorf("encode initial investigation state: %w", err)
		}
		return state, hashBytesInvestigation(encoded), nil
	}
	if snapshot.RunCheckpointSchema != investigationRunCheckpointSchema {
		return agent.InvestigationState{}, "", fmt.Errorf("unsupported run checkpoint schema %d", snapshot.RunCheckpointSchema)
	}
	if snapshot.RunCheckpointVersion == 0 || snapshot.RunCheckpointHash == "" {
		return agent.InvestigationState{}, "", errors.New("run checkpoint has incomplete metadata")
	}
	if hashBytesInvestigation(snapshot.RunCheckpoint) != snapshot.RunCheckpointHash {
		return agent.InvestigationState{}, "", errors.New("run checkpoint hash does not match payload")
	}
	var state agent.InvestigationState
	decoder := json.NewDecoder(bytes.NewReader(snapshot.RunCheckpoint))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return agent.InvestigationState{}, "", fmt.Errorf("decode run checkpoint: %w", err)
	}
	if state.SchemaVersion != investigationRunCheckpointSchema || state.RunID != snapshot.RunPublicID ||
		state.IncidentID != snapshot.IncidentPublicID || state.CycleNo != uint64(snapshot.Task.CycleNo) ||
		state.CheckpointVersion != snapshot.RunCheckpointVersion || state.CheckpointVersion == 0 {
		return agent.InvestigationState{}, "", errors.New("run checkpoint identity or version is invalid")
	}
	if state.Usage != snapshot.Usage || state.Limits != snapshot.Limits ||
		state.IncidentVersion != snapshot.ExpectedIncidentVersion {
		return agent.InvestigationState{}, "", errors.New("run checkpoint usage or limits diverge from AgentRun")
	}
	return state, snapshot.RunCheckpointHash, nil
}

func snapshotClaimType(snapshot investigationSnapshot) string {
	// The loader cannot carry config into this pure state constructor. The
	// operation replaces this value before presenting the state to a model; the
	// stable fallback keeps old rows replayable.
	if snapshot.State.Coverage.ClaimType != "" {
		return snapshot.State.Coverage.ClaimType
	}
	return "incident-investigation/v1"
}

func claimFacets(snapshot investigationSnapshot) []string {
	if len(snapshot.State.Coverage.RequiredFacets) > 0 {
		return slices.Clone(snapshot.State.Coverage.RequiredFacets)
	}
	return []string{"subject", "runtime"}
}

func (o *investigationStepOperation) statePolicy(snapshot investigationSnapshot) agent.ReducerPolicy {
	actions := make(map[string]agent.ToolActionPolicy, len(o.cfg.ActionPolicies))
	for name, policy := range o.cfg.ActionPolicies {
		actions[name] = agent.ToolActionPolicy{
			TemplateIDs: slices.Clone(policy.TemplateIDs), ParameterKeys: slices.Clone(policy.ParameterKeys),
			ExpectedFactTypes: slices.Clone(policy.ExpectedFactTypes),
		}
	}
	scopes := map[string]struct{}{snapshot.ScopeRef: {}}
	facts := make(map[string]agent.EvidenceFact, len(snapshot.Facts))
	for _, fact := range snapshot.Facts {
		facts[fact.ID] = fact
	}
	return agent.ReducerPolicy{MaxBytes: minInvestigation(snapshot.Limits.MaxCheckpointSize, defaultTaskCheckpointBytes), AllowedActions: actions, AllowedScopes: scopes, Evidence: facts}
}

func decodeInvestigationTaskCheckpoint(task asyncjob.Task, snapshot investigationSnapshot, payload investigationStepPayload) (preparedInvestigationStep, error) {
	if task.CheckpointSchema != investigationTaskCheckpointSchema || task.CheckpointVersion == 0 || task.CheckpointHash == "" {
		return preparedInvestigationStep{}, errors.New("task checkpoint metadata is incomplete")
	}
	if hashBytesInvestigation(task.Checkpoint) != task.CheckpointHash {
		return preparedInvestigationStep{}, errors.New("task checkpoint hash does not match payload")
	}
	var checkpoint investigationStepCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(task.Checkpoint))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&checkpoint); err != nil {
		return preparedInvestigationStep{}, fmt.Errorf("decode task checkpoint: %w", err)
	}
	if checkpoint.SchemaVersion != investigationTaskCheckpointSchema || checkpoint.Mode != payload.Mode ||
		checkpoint.SubjectVersion != task.ExpectedSubjectVersion || checkpoint.BasisCheckpointVersion != snapshot.State.CheckpointVersion ||
		checkpoint.BasisCheckpointHash != snapshot.StateHash || checkpoint.StateHash == "" ||
		checkpoint.State.SchemaVersion != investigationRunCheckpointSchema {
		return preparedInvestigationStep{}, errors.New("task checkpoint basis or identity is stale")
	}
	encodedState, err := json.Marshal(checkpoint.State)
	if err != nil || hashBytesInvestigation(encodedState) != checkpoint.StateHash {
		return preparedInvestigationStep{}, errors.New("task checkpoint state hash is invalid")
	}
	if checkpoint.State.RunID != snapshot.RunPublicID || checkpoint.State.IncidentID != snapshot.IncidentPublicID || checkpoint.State.CycleNo != uint64(task.CycleNo) {
		return preparedInvestigationStep{}, errors.New("task checkpoint state belongs to another subject")
	}
	if err := validateDecodedPrepared(snapshot, checkpoint); err != nil {
		return preparedInvestigationStep{}, err
	}
	if payload.Mode == stepModeTool {
		if checkpoint.Action == nil || payload.Action == nil {
			return preparedInvestigationStep{}, errors.New("task checkpoint tool action is missing")
		}
		left, leftErr := agent.ActionSignature(*checkpoint.Action)
		right, rightErr := agent.ActionSignature(*payload.Action)
		if leftErr != nil || rightErr != nil || left != right {
			return preparedInvestigationStep{}, errors.New("task checkpoint tool action does not match payload")
		}
	}
	return preparedInvestigationStep{checkpoint: checkpoint, state: checkpoint.State}, nil
}

type preparedInvestigationStep struct {
	checkpoint investigationStepCheckpoint
	state      agent.InvestigationState
}

func hashBytesInvestigation(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func minInvestigation(left, right int) int {
	if left <= 0 {
		return right
	}
	if left < right {
		return left
	}
	return right
}

func boundInvestigation(value string, max int) string {
	value = strings.ToValidUTF8(value, "?")
	if len(value) <= max {
		return value
	}
	for max > 0 && !utf8.ValidString(value[:max]) {
		max--
	}
	return value[:max]
}

func (o *investigationStepOperation) executeOne(ctx context.Context, execution asyncjob.Execution, snapshot investigationSnapshot, payload investigationStepPayload) (preparedInvestigationStep, error) {
	if payload.BasisCheckpointVersion != 0 && payload.BasisCheckpointVersion != snapshot.State.CheckpointVersion {
		return preparedInvestigationStep{}, fmt.Errorf("%w: payload checkpoint basis is stale", agent.ErrConflict)
	}
	switch payload.Mode {
	case stepModeDecide:
		if snapshot.State.NextNode != agent.NodeSelectAction && snapshot.State.NextNode != agent.NodePlanInvestigation && snapshot.State.NextNode != agent.NodeReplan {
			return preparedInvestigationStep{}, fmt.Errorf("%w: decide is invalid at node %s", agent.ErrConflict, snapshot.State.NextNode)
		}
		return o.executeDecision(ctx, execution, snapshot)
	case stepModeTool:
		if snapshot.State.NextNode != agent.NodeExecuteTool {
			return preparedInvestigationStep{}, fmt.Errorf("%w: tool is invalid at node %s", agent.ErrConflict, snapshot.State.NextNode)
		}
		return o.executeTool(ctx, execution, snapshot, *payload.Action)
	case stepModeSynthesize:
		if snapshot.State.NextNode != agent.NodeProduceDiagnosis {
			return preparedInvestigationStep{}, fmt.Errorf("%w: synthesis is invalid at node %s", agent.ErrConflict, snapshot.State.NextNode)
		}
		return o.executeSynthesis(ctx, execution, snapshot)
	default:
		return preparedInvestigationStep{}, fmt.Errorf("%w: unsupported step mode", agent.ErrInvalidArgument)
	}
}

func (o *investigationStepOperation) executeDecision(ctx context.Context, execution asyncjob.Execution, snapshot investigationSnapshot) (preparedInvestigationStep, error) {
	sufficiency, err := o.evaluateSufficiency(snapshot, snapshot.State, snapshot.Facts)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	view := agent.ModelView{
		State: snapshot.State, Facts: slices.Clone(snapshot.Facts), ScopeRef: snapshot.ScopeRef,
		AllowedActions:   modelActionSchemas(o.cfg.ActionPolicies),
		CandidateClaims:  []agent.ClaimPolicy{o.cfg.ClaimPolicy},
		ClaimSufficiency: map[string]agent.SufficiencyResult{o.cfg.ClaimPolicy.ClaimType: sufficiency},
	}
	input, _ := json.Marshal(view)
	reservation := agent.Usage{Steps: 1, ModelCalls: reservedModelCalls(o.cfg.Model), InputTokens: estimatedTokens(input), OutputTokens: 1}
	if err := snapshot.State.Usage.CanCharge(reservation, snapshot.State.Limits); err != nil {
		return o.prepareInsufficient(snapshot, investigationStepPayload{Mode: stepModeDecide}, "decision_budget_exhausted", o.cfg.Now())
	}
	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	started := o.cfg.Now()
	delta, modelUsage, err := o.cfg.Model.ProposeDelta(externalCtx, view)
	cancel()
	captured := o.cfg.Now()
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	output, _ := json.Marshal(delta)
	usage := normalizedModelUsage(modelUsage, input, output)
	if err := snapshot.State.Usage.CanCharge(usage, snapshot.State.Limits); err != nil {
		return o.prepareInsufficient(snapshot, investigationStepPayload{Mode: stepModeDecide}, "model_usage_exceeded_budget", captured)
	}
	policy := o.statePolicy(snapshot)
	policy.StepUsage = usage
	state, _, err := agent.ReduceStateDelta(snapshot.State, delta, policy)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	state.UpdatedAt = captured.UTC()
	sufficiency, err = o.evaluateSufficiency(snapshot, state, snapshot.Facts)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	checkpoint := investigationStepCheckpoint{
		SchemaVersion: investigationTaskCheckpointSchema, Mode: stepModeDecide,
		SubjectVersion: snapshot.Task.ExpectedSubjectVersion, BasisCheckpointVersion: snapshot.State.CheckpointVersion,
		BasisCheckpointHash: snapshot.StateHash, Delta: &delta, Sufficiency: sufficiency, Usage: usage,
		StepNode: agent.NodeSelectAction, StepSummary: "validated one bounded StateDelta", CapturedAt: captured.UTC(),
		DurationMS: nonnegativeDuration(started, captured),
	}
	switch {
	case sufficiency.Outcome == agent.SufficiencyReady:
		state.NextNode = agent.NodeProduceDiagnosis
		checkpoint.NextMode = stepModeSynthesize
	case sufficiency.Outcome == agent.SufficiencyInsufficient || delta.ProposedStop == agent.StopInsufficient:
		state.NextNode, state.TerminalOutcome = agent.NodeEnd, "insufficient_evidence"
		checkpoint.TerminalOutcome = state.TerminalOutcome
	case delta.ProposedStop == agent.StopDiagnose:
		// A model stop cannot override the deterministic coverage evaluator.
		state.NextNode, state.TerminalOutcome = agent.NodeEnd, "insufficient_evidence"
		checkpoint.TerminalOutcome = state.TerminalOutcome
	default:
		if delta.ProposedAction == nil {
			return preparedInvestigationStep{}, errors.New("validated continue decision lost its action")
		}
		checkpoint.NextMode, checkpoint.NextAction = stepModeTool, cloneAction(delta.ProposedAction)
	}
	return finalizePrepared(checkpoint, state)
}

func (o *investigationStepOperation) executeTool(ctx context.Context, execution asyncjob.Execution, snapshot investigationSnapshot, action agent.ProposedAction) (preparedInvestigationStep, error) {
	if err := validateInvestigationToolAction(action, snapshot, o.cfg.ActionPolicies); err != nil {
		return preparedInvestigationStep{}, err
	}
	signature, err := agent.ActionSignature(action)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	if !hasProposedAttempt(snapshot.State.ToolAttempts, signature, action.Tool) {
		return preparedInvestigationStep{}, fmt.Errorf("%w: tool action is not the reducer-approved pending signature", agent.ErrPermission)
	}
	usage := agent.Usage{Steps: 1, ToolCalls: 1, Evidence: 1}
	if err := snapshot.State.Usage.CanCharge(usage, snapshot.State.Limits); err != nil {
		return o.prepareInsufficient(snapshot, investigationStepPayload{Mode: stepModeTool, Action: &action}, "tool_budget_exhausted", o.cfg.Now())
	}
	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	if snapshot.State.Limits.ToolTimeout > 0 {
		var toolCancel context.CancelFunc
		externalCtx, toolCancel = context.WithTimeout(externalCtx, snapshot.State.Limits.ToolTimeout)
		defer toolCancel()
	}
	started := o.cfg.Now()
	observation, err := o.cfg.Tools.Execute(externalCtx, agent.InvestigationToolRequest{
		Action: action, IncidentPublicID: snapshot.IncidentPublicID, CycleNo: uint64(snapshot.Task.CycleNo),
		Correlation: agent.CorrelationSnapshot{
			Cluster: snapshot.Cluster, Environment: snapshot.Environment, Namespace: snapshot.Namespace,
			Workload: snapshot.TargetName, TargetKind: snapshot.TargetKind,
		},
		Window: snapshot.State.Window,
	})
	cancel()
	captured := o.cfg.Now()
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	evidenceID := deterministicPublicID("investigation-evidence", execution.Task.DedupeKey)
	observation, err = normalizeObservation(observation, snapshot, action, evidenceID)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	state := snapshot.State
	state.ToolAttempts = slices.Clone(state.ToolAttempts)
	for index := len(state.ToolAttempts) - 1; index >= 0; index-- {
		if state.ToolAttempts[index].Signature == signature {
			state.ToolAttempts[index].Status = string(observation.Status)
			state.ToolAttempts[index].Attempted = captured.UTC()
			break
		}
	}
	state.Evidence = slices.Clone(state.Evidence)
	for _, fact := range observation.Facts {
		state.Evidence = appendUniqueEvidenceRef(state.Evidence, agent.EvidenceReference{ID: fact.ID, FactType: fact.Type})
	}
	state.UnavailableSources = slices.Clone(state.UnavailableSources)
	if observation.Status == agent.CollectionUnavailable || observation.Status == agent.CollectionInvalid {
		state.UnavailableSources = appendUniqueUnavailable(state.UnavailableSources, agent.UnavailableSource{Source: observation.SourceSystem, Reason: string(observation.Status)})
	}
	state.Usage.Charge(usage)
	state.CheckpointVersion++
	state.NextNode = agent.NodeSelectAction
	state.UpdatedAt = captured.UTC()
	facts := append(slices.Clone(snapshot.Facts), observation.Facts...)
	sufficiency, err := o.evaluateSufficiency(snapshot, state, facts)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	checkpoint := investigationStepCheckpoint{
		SchemaVersion: investigationTaskCheckpointSchema, Mode: stepModeTool,
		SubjectVersion: snapshot.Task.ExpectedSubjectVersion, BasisCheckpointVersion: snapshot.State.CheckpointVersion,
		BasisCheckpointHash: snapshot.StateHash, Action: cloneAction(&action), Observation: &observation,
		Sufficiency: sufficiency, Usage: usage, StepNode: agent.NodeExecuteTool,
		StepSummary: boundInvestigation(observation.Summary, 4096), CapturedAt: captured.UTC(),
		DurationMS: nonnegativeDuration(started, captured),
	}
	switch sufficiency.Outcome {
	case agent.SufficiencyReady:
		state.NextNode, checkpoint.NextMode = agent.NodeProduceDiagnosis, stepModeSynthesize
	case agent.SufficiencyInsufficient:
		state.NextNode, state.TerminalOutcome = agent.NodeEnd, "insufficient_evidence"
		checkpoint.TerminalOutcome = state.TerminalOutcome
	default:
		checkpoint.NextMode = stepModeDecide
	}
	return finalizePrepared(checkpoint, state)
}

func (o *investigationStepOperation) executeSynthesis(ctx context.Context, execution asyncjob.Execution, snapshot investigationSnapshot) (preparedInvestigationStep, error) {
	sufficiency, err := o.evaluateSufficiency(snapshot, snapshot.State, snapshot.Facts)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	if sufficiency.Outcome != agent.SufficiencyReady {
		return o.prepareInsufficient(snapshot, investigationStepPayload{Mode: stepModeSynthesize}, "diagnosis_coverage_not_ready", o.cfg.Now())
	}
	view := agent.DiagnosisView{
		State: snapshot.State, Facts: slices.Clone(snapshot.Facts), Sufficiency: sufficiency,
		AllowedClaimTypes:  []string{snapshot.State.Coverage.ClaimType},
		SufficiencyByClaim: map[string]agent.SufficiencyResult{snapshot.State.Coverage.ClaimType: sufficiency},
	}
	input, _ := json.Marshal(view)
	reservation := agent.Usage{Steps: 1, ModelCalls: reservedModelCalls(o.cfg.Model), InputTokens: estimatedTokens(input), OutputTokens: 1}
	if err := snapshot.State.Usage.CanCharge(reservation, snapshot.State.Limits); err != nil {
		return o.prepareInsufficient(snapshot, investigationStepPayload{Mode: stepModeSynthesize}, "diagnosis_budget_exhausted", o.cfg.Now())
	}
	externalCtx, cancel, err := asyncjob.ExternalCallContext(ctx)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	started := o.cfg.Now()
	candidate, modelUsage, err := o.cfg.Model.SynthesizeDiagnosis(externalCtx, view)
	cancel()
	captured := o.cfg.Now()
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	output, _ := json.Marshal(candidate)
	usage := normalizedModelUsage(modelUsage, input, output)
	if err := snapshot.State.Usage.CanCharge(usage, snapshot.State.Limits); err != nil {
		return o.prepareInsufficient(snapshot, investigationStepPayload{Mode: stepModeSynthesize}, "diagnosis_usage_exceeded_budget", captured)
	}
	diagnosis, err := validateV3Diagnosis(candidate, snapshot, o.cfg.ClaimPolicy, sufficiency)
	if err != nil {
		return preparedInvestigationStep{}, agent.NewRuntimeError(agent.ErrorMalformedModel, err.Error(), err)
	}
	state := snapshot.State
	state.Usage.Charge(usage)
	state.CheckpointVersion++
	state.NextNode, state.TerminalOutcome = agent.NodeEnd, "diagnosed"
	state.UpdatedAt = captured.UTC()
	checkpoint := investigationStepCheckpoint{
		SchemaVersion: investigationTaskCheckpointSchema, Mode: stepModeSynthesize,
		SubjectVersion: snapshot.Task.ExpectedSubjectVersion, BasisCheckpointVersion: snapshot.State.CheckpointVersion,
		BasisCheckpointHash: snapshot.StateHash, Diagnosis: &diagnosis, Sufficiency: sufficiency, Usage: usage,
		StepNode: agent.NodeProduceDiagnosis, StepSummary: "synthesized and deterministically validated diagnosis",
		TerminalOutcome: "diagnosed", CapturedAt: captured.UTC(), DurationMS: nonnegativeDuration(started, captured),
	}
	return finalizePrepared(checkpoint, state)
}

func (o *investigationStepOperation) prepareInsufficient(snapshot investigationSnapshot, payload investigationStepPayload, reason string, at time.Time) (preparedInvestigationStep, error) {
	state := snapshot.State
	state.CheckpointVersion++
	state.NextNode, state.TerminalOutcome = agent.NodeEnd, "insufficient_evidence"
	state.UpdatedAt = at.UTC()
	sufficiency, err := o.evaluateSufficiency(snapshot, state, snapshot.Facts)
	if err != nil {
		return preparedInvestigationStep{}, err
	}
	if sufficiency.Outcome != agent.SufficiencyInsufficient {
		sufficiency.Outcome = agent.SufficiencyInsufficient
		sufficiency.ConfidenceCap = minFloat(sufficiency.ConfidenceCap, 0.3)
	}
	sufficiency.ReasonCodes = appendStableString(sufficiency.ReasonCodes, reason)
	checkpoint := investigationStepCheckpoint{
		SchemaVersion: investigationTaskCheckpointSchema, Mode: payload.Mode,
		SubjectVersion: snapshot.Task.ExpectedSubjectVersion, BasisCheckpointVersion: snapshot.State.CheckpointVersion,
		BasisCheckpointHash: snapshot.StateHash, Action: cloneAction(payload.Action), Sufficiency: sufficiency,
		StepNode: agent.NodeBudgetExceeded, StepSummary: boundInvestigation("investigation completed without sufficient evidence: "+reason, 4096),
		TerminalOutcome: "insufficient_evidence", CapturedAt: at.UTC(),
	}
	return finalizePrepared(checkpoint, state)
}

func (o *investigationStepOperation) evaluateSufficiency(snapshot investigationSnapshot, state agent.InvestigationState, facts []agent.EvidenceFact) (agent.SufficiencyResult, error) {
	unavailable := make([]string, 0)
	for _, source := range state.UnavailableSources {
		if slices.Contains(o.cfg.RequiredSources, source.Source) {
			unavailable = append(unavailable, source.Source)
		}
	}
	return agent.EvaluateSufficiency(agent.SufficiencyInput{
		IncidentID: snapshot.IncidentPublicID, CycleNo: uint64(snapshot.Task.CycleNo), Facts: facts,
		Policy: o.cfg.ClaimPolicy, BudgetExhausted: investigationBudgetExhausted(state.Usage, state.Limits) || consecutiveToolFailures(state.ToolAttempts) >= 3,
		RequiredSourcesUnavailable: unavailable,
	})
}

func investigationBudgetExhausted(usage agent.Usage, limits agent.Limits) bool {
	return usage.Steps >= limits.MaxSteps || usage.ToolCalls >= limits.MaxToolCalls ||
		usage.ModelCalls >= limits.MaxModelCalls || usage.TotalTokens() >= limits.TokenBudget ||
		usage.Evidence >= limits.MaxEvidenceItems
}

func consecutiveToolFailures(attempts []agent.ToolAttempt) int {
	count := 0
	for index := len(attempts) - 1; index >= 0; index-- {
		switch attempts[index].Status {
		case "unavailable", "invalid", "partial", "transient_failure":
			count++
		default:
			return count
		}
	}
	return count
}

func finalizePrepared(checkpoint investigationStepCheckpoint, state agent.InvestigationState) (preparedInvestigationStep, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return preparedInvestigationStep{}, fmt.Errorf("encode investigation state: %w", err)
	}
	checkpoint.State = state
	checkpoint.StateHash = hashBytesInvestigation(encoded)
	return preparedInvestigationStep{checkpoint: checkpoint, state: state}, nil
}

func (o *investigationStepOperation) persistAndResolve(ctx context.Context, execution asyncjob.Execution, snapshot investigationSnapshot, prepared preparedInvestigationStep) asyncjob.Result {
	stateJSON, stateErr := json.Marshal(prepared.state)
	if stateErr != nil || len(stateJSON) > snapshot.Limits.MaxCheckpointSize {
		return asyncjob.Dead("run_checkpoint_too_large", "investigation Run checkpoint exceeds its bounded size", o.failRunMutation(snapshot, "run_checkpoint_too_large"))
	}
	encoded, err := json.Marshal(prepared.checkpoint)
	if err != nil || len(encoded) > o.cfg.MaxCheckpointBytes {
		return asyncjob.Dead("checkpoint_too_large", "investigation task checkpoint exceeds its bounded size", o.failRunMutation(snapshot, "checkpoint_too_large"))
	}
	checkpoint := asyncjob.Checkpoint{
		SchemaVersion: investigationTaskCheckpointSchema, Version: execution.Task.CheckpointVersion + 1,
		Hash: hashBytesInvestigation(encoded), Payload: encoded,
	}
	if checkpoint.Version == 0 {
		checkpoint.Version = 1
	}
	if err := o.cfg.Tasks.Checkpoint(ctx, execution.Lease, checkpoint, nil); err != nil {
		if errors.Is(err, asyncjob.ErrLeaseLost) || errors.Is(err, context.Canceled) {
			// An invalid result deliberately leaves resolution to the current lease
			// owner or its eventual takeover.
			return asyncjob.Result{}
		}
		return asyncjob.RetryAfter(0, "checkpoint_persist_failed", boundInvestigation(err.Error(), 2048), nil)
	}
	return o.resolvePrepared(snapshot, prepared)
}

func (o *investigationStepOperation) resolvePrepared(snapshot investigationSnapshot, prepared preparedInvestigationStep) asyncjob.Result {
	return asyncjob.Succeeded(func(ctx context.Context, tx asyncjob.DBTX) error {
		return o.applyPrepared(ctx, tx, snapshot, prepared)
	})
}

func investigationLoadFailure(err error) asyncjob.Result {
	switch {
	case errors.Is(err, asyncjob.ErrSubjectVersionMismatch):
		return asyncjob.Dead("subject_version_mismatch", "investigation AgentRun no longer matches the task", nil)
	case errors.Is(err, asyncjob.ErrInvalidMutation):
		return asyncjob.Dead("invalid_agent_run_state", boundInvestigation(err.Error(), 2048), nil)
	default:
		return asyncjob.RetryAfter(0, "investigation_load_failed", boundInvestigation(err.Error(), 2048), nil)
	}
}

func (o *investigationStepOperation) executionFailure(ctx context.Context, snapshot investigationSnapshot, payload investigationStepPayload, execution asyncjob.Execution, executionErr error) asyncjob.Result {
	var runtimeErr *agent.RuntimeError
	retryable := errors.As(executionErr, &runtimeErr) && runtimeErr.Retryable
	malformed := errors.As(executionErr, &runtimeErr) && runtimeErr.Code == agent.ErrorMalformedModel
	retryLimit := execution.Lease.MaxAttempts
	if payload.Mode == stepModeTool && retryLimit > 2 {
		retryLimit = 2
	}
	if malformed {
		if reservedModelCalls(o.cfg.Model) > 1 {
			// The typed Eino invocation already performed its single repair.
			retryLimit = 1
		} else if retryLimit > 2 {
			retryLimit = 2
		}
	}
	if (retryable || malformed) && execution.Lease.Attempt < retryLimit {
		code := "investigation_dependency_error"
		if runtimeErr != nil {
			code = string(runtimeErr.Code)
		}
		return asyncjob.RetryAfter(0, code, boundInvestigation(executionErr.Error(), 2048), nil)
	}
	if errors.Is(executionErr, asyncjob.ErrExternalDeadlineMissing) {
		return asyncjob.Dead("external_deadline_missing", "investigation external-call deadline is not configured", o.failRunMutation(snapshot, "external_deadline_missing"))
	}
	prepared, err := o.prepareInsufficient(snapshot, payload, "step_execution_unavailable", o.cfg.Now())
	if err != nil {
		return asyncjob.Dead("investigation_step_failed", boundInvestigation(executionErr.Error(), 2048), nil)
	}
	return o.persistAndResolve(ctx, execution, snapshot, prepared)
}

func normalizedModelUsage(value agent.ModelUsage, input, output []byte) agent.Usage {
	inputTokens, outputTokens := value.InputTokens, value.OutputTokens
	modelCalls := value.Calls
	if modelCalls <= 0 {
		modelCalls = 1
	}
	if inputTokens <= 0 {
		inputTokens = estimatedTokens(input)
	}
	if outputTokens <= 0 {
		outputTokens = estimatedTokens(output)
	}
	return agent.Usage{Steps: 1, ModelCalls: modelCalls, InputTokens: inputTokens, OutputTokens: outputTokens}
}

func reservedModelCalls(model agent.InvestigationModel) int {
	if budgeted, ok := model.(agent.InvestigationModelCallBudget); ok {
		if calls := budgeted.MaxProviderCallsPerInvocation(); calls > 0 && calls <= 2 {
			return calls
		}
	}
	return 1
}

func estimatedTokens(value []byte) int64 {
	result := int64((len(value) + 3) / 4)
	if result < 1 {
		return 1
	}
	return result
}

func nonnegativeDuration(started, finished time.Time) int64 {
	duration := finished.Sub(started).Milliseconds()
	if duration < 0 {
		return 0
	}
	return duration
}

func cloneAction(action *agent.ProposedAction) *agent.ProposedAction {
	if action == nil {
		return nil
	}
	result := *action
	result.BoundedParameters = append([]byte(nil), action.BoundedParameters...)
	result.ExpectedFactTypes = slices.Clone(action.ExpectedFactTypes)
	return &result
}

func hasProposedAttempt(attempts []agent.ToolAttempt, signature, tool string) bool {
	for index := len(attempts) - 1; index >= 0; index-- {
		if attempts[index].Signature == signature {
			return attempts[index].Tool == tool && attempts[index].Status == "proposed"
		}
	}
	return false
}

func appendUniqueEvidenceRef(values []agent.EvidenceReference, addition agent.EvidenceReference) []agent.EvidenceReference {
	for _, value := range values {
		if value.ID == addition.ID {
			return values
		}
	}
	return append(values, addition)
}

func appendUniqueUnavailable(values []agent.UnavailableSource, addition agent.UnavailableSource) []agent.UnavailableSource {
	for _, value := range values {
		if value.Source == addition.Source && value.Reason == addition.Reason {
			return values
		}
	}
	return append(values, addition)
}

func appendStableString(values []string, addition string) []string {
	if !slices.Contains(values, addition) {
		values = append(values, addition)
		slices.Sort(values)
	}
	return values
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func normalizeObservation(value agent.ToolObservation, snapshot investigationSnapshot, action agent.ProposedAction, evidenceID string) (agent.ToolObservation, error) {
	value.SourceSystem = strings.TrimSpace(value.SourceSystem)
	value.CollectionPath = strings.TrimSpace(value.CollectionPath)
	value.TemplateVersion = strings.TrimSpace(value.TemplateVersion)
	value.Summary = boundInvestigation(strings.TrimSpace(value.Summary), 4096)
	if value.SourceSystem == "" || value.CollectionPath == "" || value.TemplateVersion == "" || value.Summary == "" {
		return agent.ToolObservation{}, fmt.Errorf("%w: observation provenance is incomplete", agent.ErrInvalidArgument)
	}
	if value.TemplateVersion != action.TemplateID {
		return agent.ToolObservation{}, fmt.Errorf("%w: observation template does not match the approved action", agent.ErrPermission)
	}
	switch value.Status {
	case agent.CollectionAvailable, agent.CollectionNoData, agent.CollectionPartial, agent.CollectionUnavailable, agent.CollectionInvalid:
	default:
		return agent.ToolObservation{}, fmt.Errorf("%w: unsupported observation status", agent.ErrInvalidArgument)
	}
	if value.Status == agent.CollectionAvailable && len(value.Facts) == 0 {
		return agent.ToolObservation{}, fmt.Errorf("%w: available observation requires typed facts", agent.ErrInvalidArgument)
	}
	if len(value.Facts) > 64 {
		return agent.ToolObservation{}, fmt.Errorf("%w: observation has too many facts", agent.ErrInvalidArgument)
	}
	seenFacts := make(map[string]struct{}, len(value.Facts))
	for index := range value.Facts {
		fact := &value.Facts[index]
		fact.ID = strings.TrimSpace(fact.ID)
		if fact.ID == "" || len(fact.ID) > 128 || strings.TrimSpace(fact.Type) == "" || strings.TrimSpace(fact.CorroborationGroup) == "" {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation fact identity is incomplete", agent.ErrInvalidArgument)
		}
		if _, duplicate := seenFacts[fact.ID]; duplicate {
			return agent.ToolObservation{}, fmt.Errorf("%w: duplicate observation fact id", agent.ErrInvalidArgument)
		}
		seenFacts[fact.ID] = struct{}{}
		if !slices.Contains(action.ExpectedFactTypes, fact.Type) {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation returned an unexpected fact type", agent.ErrPermission)
		}
		if fact.IncidentID != "" && fact.IncidentID != snapshot.IncidentPublicID || fact.CycleNo != 0 && fact.CycleNo != uint64(snapshot.Task.CycleNo) {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation fact escaped the Incident cycle", agent.ErrPermission)
		}
		if fact.SourceSystem != "" && fact.SourceSystem != value.SourceSystem || fact.CollectionPath != "" && fact.CollectionPath != value.CollectionPath {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation fact provenance diverges from its envelope", agent.ErrInvalidArgument)
		}
		fact.EvidenceID = evidenceID
		fact.IncidentID = snapshot.IncidentPublicID
		fact.CycleNo = uint64(snapshot.Task.CycleNo)
		fact.SourceSystem = value.SourceSystem
		fact.CollectionPath = value.CollectionPath
		if fact.CollectionStatus == "" {
			fact.CollectionStatus = value.Status
		}
		if fact.CollectionStatus != value.Status || strings.TrimSpace(fact.Authority) == "" || strings.TrimSpace(fact.Integrity) == "" ||
			strings.TrimSpace(fact.Freshness) == "" || strings.TrimSpace(fact.Completeness) == "" || strings.TrimSpace(fact.ClaimUse) == "" {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation fact trust axes are incomplete", agent.ErrInvalidArgument)
		}
		if len(fact.Attributes) > 24 {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation fact attributes exceed bounds", agent.ErrInvalidArgument)
		}
		attributeBytes := 0
		for key, item := range fact.Attributes {
			key = strings.TrimSpace(key)
			item = strings.TrimSpace(item)
			if key == "" || len(key) > 64 || len(item) > 1024 || strings.ContainsAny(key, "\r\n\t") || strings.ContainsAny(item, "\x00\r") {
				return agent.ToolObservation{}, fmt.Errorf("%w: observation fact attribute is invalid", agent.ErrInvalidArgument)
			}
			attributeBytes += len(key) + len(item)
		}
		if attributeBytes > 4096 {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation fact attributes exceed size bound", agent.ErrInvalidArgument)
		}
	}
	if value.SafeDeepLink != "" {
		parsed, err := url.Parse(value.SafeDeepLink)
		if err != nil || parsed.Host == "" || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.User != nil {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation deep link is unsafe", agent.ErrPermission)
		}
	}
	for key, item := range value.Provenance {
		if strings.TrimSpace(key) == "" || len(key) > 128 || len(item) > 1024 {
			return agent.ToolObservation{}, fmt.Errorf("%w: observation provenance exceeds bounds", agent.ErrInvalidArgument)
		}
	}
	value.ContentHash = ""
	canonical, err := json.Marshal(struct {
		Tool        string                `json:"tool"`
		Action      agent.ProposedAction  `json:"action"`
		Observation agent.ToolObservation `json:"observation"`
	}{Tool: action.Tool, Action: action, Observation: value})
	if err != nil {
		return agent.ToolObservation{}, fmt.Errorf("encode normalized observation: %w", err)
	}
	value.ContentHash = hashBytesInvestigation(canonical)
	envelope, err := evidenceEnvelope(value)
	if err != nil || len(envelope) > snapshot.Limits.MaxEvidenceBytes {
		return agent.ToolObservation{}, fmt.Errorf("%w: normalized observation exceeds Evidence bounds", agent.ErrBudgetExceeded)
	}
	return value, nil
}

func evidenceEnvelope(observation agent.ToolObservation) ([]byte, error) {
	return json.Marshal(storedEvidenceEnvelope{
		SchemaVersion: 1, Status: observation.Status, SourceSystem: observation.SourceSystem,
		CollectionPath: observation.CollectionPath, TemplateVersion: observation.TemplateVersion,
		Summary: observation.Summary, Facts: observation.Facts, Truncated: observation.Truncated,
		Provenance: observation.Provenance, SafeDeepLink: observation.SafeDeepLink, ContentHash: observation.ContentHash,
	})
}

func deterministicPublicID(kind, taskPublicID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind+"\x00"+taskPublicID)).String()
}

func validateV3Diagnosis(candidate agent.DiagnosisCandidate, snapshot investigationSnapshot, policy agent.ClaimPolicy, sufficiency agent.SufficiencyResult) (agent.DiagnosisRecord, error) {
	candidate.ClaimType = strings.TrimSpace(candidate.ClaimType)
	candidate.Summary = strings.TrimSpace(candidate.Summary)
	if candidate.ClaimType != policy.ClaimType || candidate.Summary == "" || len(candidate.Summary) > 4096 {
		return agent.DiagnosisRecord{}, errors.New("diagnosis claim type or summary is invalid")
	}
	switch candidate.Confidence {
	case agent.DiagnosisConfirmed, agent.DiagnosisLikely, agent.DiagnosisUnknown:
	default:
		return agent.DiagnosisRecord{}, errors.New("diagnosis confidence is not an allowed enum")
	}
	switch candidate.RemediationHint {
	case agent.RemediationRestoreRequiredEnv, agent.RemediationCollectMore, agent.RemediationNone:
	default:
		return agent.DiagnosisRecord{}, errors.New("diagnosis remediation hint is not an allowed enum")
	}
	if candidate.Confidence == agent.DiagnosisConfirmed {
		if sufficiency.Outcome != agent.SufficiencyReady {
			return agent.DiagnosisRecord{}, errors.New("confirmed diagnosis is unsupported by deterministic sufficiency")
		}
		if policy.ClaimType == agent.GoldenRequiredEnvClaimPolicy().ClaimType && candidate.RemediationHint != agent.RemediationRestoreRequiredEnv {
			return agent.DiagnosisRecord{}, errors.New("golden confirmed diagnosis requires restore_required_env")
		}
	} else if candidate.RemediationHint == agent.RemediationRestoreRequiredEnv {
		return agent.DiagnosisRecord{}, errors.New("restore_required_env requires a confirmed diagnosis")
	}
	if containsProhibitedDiagnosisText(candidate.Summary) {
		return agent.DiagnosisRecord{}, errors.New("diagnosis summary contains prohibited execution instructions")
	}
	if len(candidate.EvidenceFactIDs) == 0 || len(candidate.EvidenceFactIDs) > 64 || len(candidate.Unknowns) > 20 {
		return agent.DiagnosisRecord{}, errors.New("diagnosis evidence or unknowns exceed bounds")
	}
	facts := make(map[string]agent.EvidenceFact, len(snapshot.Facts))
	for _, fact := range snapshot.Facts {
		facts[fact.ID] = fact
	}
	candidate.EvidenceFactIDs = stableUniqueInvestigation(candidate.EvidenceFactIDs)
	evidenceIDs := make([]string, 0, len(candidate.EvidenceFactIDs))
	for _, id := range candidate.EvidenceFactIDs {
		fact, ok := facts[id]
		if !ok || fact.IncidentID != snapshot.IncidentPublicID || fact.CycleNo != uint64(snapshot.Task.CycleNo) ||
			fact.EvidenceID == "" || fact.CollectionStatus != agent.CollectionAvailable || fact.Integrity != "verified" ||
			fact.ClaimUse == "forbidden" || fact.Truncated {
			return agent.DiagnosisRecord{}, fmt.Errorf("diagnosis references unusable fact %q", id)
		}
		evidenceIDs = append(evidenceIDs, fact.EvidenceID)
	}
	if candidate.Confidence == agent.DiagnosisConfirmed {
		for _, required := range sufficiency.SupportingIDs {
			if !slices.Contains(candidate.EvidenceFactIDs, required) {
				return agent.DiagnosisRecord{}, fmt.Errorf("confirmed diagnosis omits supporting fact %q", required)
			}
		}
	}
	for index := range candidate.Unknowns {
		candidate.Unknowns[index] = strings.TrimSpace(candidate.Unknowns[index])
		if candidate.Unknowns[index] == "" || len(candidate.Unknowns[index]) > 1024 || containsProhibitedDiagnosisText(candidate.Unknowns[index]) {
			return agent.DiagnosisRecord{}, errors.New("diagnosis unknown is empty, oversized, or contains instructions")
		}
	}
	candidate.Unknowns = stableUniqueInvestigation(candidate.Unknowns)
	evidenceIDs = stableUniqueInvestigation(evidenceIDs)
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return agent.DiagnosisRecord{}, fmt.Errorf("encode diagnosis policy: %w", err)
	}
	record := agent.DiagnosisRecord{
		Candidate: candidate, ClaimPolicyVersion: policy.Version,
		ClaimPolicyHash: hashBytesInvestigation(policyJSON), EvidenceIDs: evidenceIDs,
	}
	unsigned, err := json.Marshal(record)
	if err != nil {
		return agent.DiagnosisRecord{}, fmt.Errorf("encode diagnosis record: %w", err)
	}
	record.DiagnosisHash = hashBytesInvestigation(unsigned)
	return record, nil
}

func containsProhibitedDiagnosisText(value string) bool {
	normalized := strings.ToLower(value)
	for _, prohibited := range []string{
		"kubectl apply", "kubectl patch", "kubectl delete", "argocd sync", "argo cd sync",
		"create pull request", "force push", "execute shell", "restart deployment", "scale deployment",
	} {
		if strings.Contains(normalized, prohibited) {
			return true
		}
	}
	return false
}

func stableUniqueInvestigation(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func cloneActionPolicies(values map[string]agent.ToolActionPolicy) map[string]agent.ToolActionPolicy {
	result := make(map[string]agent.ToolActionPolicy, len(values))
	for name, policy := range values {
		result[name] = agent.ToolActionPolicy{
			TemplateIDs: slices.Clone(policy.TemplateIDs), ParameterKeys: slices.Clone(policy.ParameterKeys),
			ExpectedFactTypes: slices.Clone(policy.ExpectedFactTypes),
		}
	}
	return result
}

func modelActionSchemas(values map[string]agent.ToolActionPolicy) []agent.ModelActionSchema {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	result := make([]agent.ModelActionSchema, 0, len(names))
	for _, name := range names {
		policy := values[name]
		templates := stableUniqueInvestigation(policy.TemplateIDs)
		parameters := stableUniqueInvestigation(policy.ParameterKeys)
		facts := stableUniqueInvestigation(policy.ExpectedFactTypes)
		result = append(result, agent.ModelActionSchema{
			Tool: name, TemplateIDs: templates, ParameterKeys: parameters, ExpectedFactTypes: facts,
		})
	}
	return result
}

func validateDecodedPrepared(snapshot investigationSnapshot, checkpoint investigationStepCheckpoint) error {
	if checkpoint.State.CheckpointVersion != snapshot.State.CheckpointVersion+1 {
		return errors.New("task checkpoint state version does not advance the run")
	}
	expected := snapshot.State.Usage
	expected.Charge(checkpoint.Usage)
	if expected != checkpoint.State.Usage {
		return errors.New("task checkpoint usage does not advance the run deterministically")
	}
	if checkpoint.CapturedAt.IsZero() || checkpoint.StepNode == "" || checkpoint.StepSummary == "" {
		return errors.New("task checkpoint step metadata is incomplete")
	}
	if checkpoint.TerminalOutcome != "" {
		if checkpoint.NextMode != "" || checkpoint.NextAction != nil || checkpoint.State.NextNode != agent.NodeEnd {
			return errors.New("terminal task checkpoint contains a next operation")
		}
	} else if checkpoint.NextMode == "" {
		return errors.New("non-terminal task checkpoint has no next operation")
	}
	if checkpoint.Mode == stepModeTool && checkpoint.Observation == nil && checkpoint.TerminalOutcome == "" {
		return errors.New("tool task checkpoint has no normalized observation")
	}
	if checkpoint.Mode == stepModeSynthesize && checkpoint.Diagnosis == nil && checkpoint.TerminalOutcome == "diagnosed" {
		return errors.New("diagnosed task checkpoint has no diagnosis record")
	}
	return nil
}

func (o *investigationStepOperation) applyPrepared(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, prepared preparedInvestigationStep) error {
	checkpoint := prepared.checkpoint
	state := prepared.state
	if checkpoint.SubjectVersion != snapshot.Task.ExpectedSubjectVersion || checkpoint.BasisCheckpointVersion != snapshot.State.CheckpointVersion || checkpoint.BasisCheckpointHash != snapshot.StateHash {
		return asyncjob.ErrSubjectVersionMismatch
	}
	stateJSON, err := json.Marshal(state)
	if err != nil || len(stateJSON) > snapshot.Limits.MaxCheckpointSize {
		return fmt.Errorf("%w: run checkpoint exceeds its persisted bound", asyncjob.ErrBusinessBudgetExceeded)
	}
	stateHash := hashBytesInvestigation(stateJSON)
	if stateHash != checkpoint.StateHash {
		return fmt.Errorf("%w: prepared state hash mismatch", asyncjob.ErrInvalidMutation)
	}
	stepPublicID := deterministicPublicID("investigation-step", snapshot.Task.DedupeKey)
	arguments, argumentsHash := stepArguments(checkpoint)
	evidencePublicID := ""
	if checkpoint.Observation != nil {
		evidencePublicID = deterministicPublicID("investigation-evidence", snapshot.Task.DedupeKey)
	}
	var sequence int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM agent_steps WHERE agent_run_id = ?`, snapshot.Task.SubjectID).Scan(&sequence); err != nil {
		return fmt.Errorf("allocate investigation step sequence: %w", err)
	}
	if sequence <= 0 {
		return errors.New("allocate investigation step returned invalid sequence")
	}
	stepNode := string(checkpoint.StepNode)
	if stepNode == "" {
		stepNode = string(agent.NodeCompleteRun)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_steps
 (public_id, agent_run_id, domain_schema_version, incident_id, cycle_no, sequence, step_type,
  short_reason, selected_tool, arguments_json, arguments_hash, result_summary, result_ref,
  evidence_public_id, status, retry_count, duration_ms, input_tokens, output_tokens, error_code,
  started_at, finished_at, created_at)
 VALUES (?, ?, 3, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'COMPLETED', 0, ?, ?, ?, '', ?, ?, ?)`,
		stepPublicID, snapshot.Task.SubjectID, snapshot.Task.IncidentID, snapshot.Task.CycleNo, sequence,
		stepNode, boundInvestigation(checkpoint.StepSummary, 1024), selectedTool(checkpoint), arguments, argumentsHash,
		boundInvestigation(checkpoint.StepSummary, 4096), "", evidencePublicID, checkpoint.DurationMS,
		checkpoint.Usage.InputTokens, checkpoint.Usage.OutputTokens, checkpoint.CapturedAt.UTC(), checkpoint.CapturedAt.UTC(), checkpoint.CapturedAt.UTC()); err != nil {
		return fmt.Errorf("persist investigation AgentStep: %w", err)
	}
	if checkpoint.Observation != nil {
		if err := insertInvestigationEvidence(ctx, tx, snapshot, checkpoint, evidencePublicID); err != nil {
			return err
		}
		if err := insertInvestigationChangeCandidates(ctx, tx, snapshot, checkpoint, evidencePublicID); err != nil {
			return err
		}
	}
	terminal := checkpoint.TerminalOutcome != ""
	legacyStatus, v3Status := "RUNNING", "running"
	var completedAt any
	var finalDiagnosis any
	if terminal {
		legacyStatus, v3Status = "COMPLETED", "completed"
		completedAt = checkpoint.CapturedAt.UTC()
		if checkpoint.Diagnosis != nil {
			final, marshalErr := json.Marshal(checkpoint.Diagnosis)
			if marshalErr != nil || len(final) > 32*1024 {
				return fmt.Errorf("%w: final diagnosis exceeds bounds", asyncjob.ErrInvalidMutation)
			}
			finalDiagnosis = final
		} else {
			finalDiagnosis = json.RawMessage(`{"summary":"Investigation completed without sufficient validated evidence.","degraded":true}`)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_runs
SET status = ?, v3_status = ?, started_at = COALESCE(started_at, ?), completed_at = ?,
    final_diagnosis = ?,
    used_steps = ?, used_tool_calls = ?, used_model_calls = ?, input_tokens = ?, output_tokens = ?,
    used_evidence_items = ?, current_checkpoint = ?, checkpoint_version = ?, checkpoint_schema_version = 1,
    checkpoint_hash = ?, row_version = row_version + 1, updated_at = ?
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
  AND status IN ('PENDING','RUNNING') AND v3_status IN ('pending','running')
  AND row_version = ?`,
		legacyStatus, v3Status, checkpoint.CapturedAt.UTC(), completedAt, finalDiagnosis,
		state.Usage.Steps, state.Usage.ToolCalls, state.Usage.ModelCalls, state.Usage.InputTokens, state.Usage.OutputTokens,
		state.Usage.Evidence, stateJSON, state.CheckpointVersion, stateHash, checkpoint.CapturedAt.UTC(),
		snapshot.Task.SubjectID, snapshot.Task.IncidentID, snapshot.Task.CycleNo, snapshot.Task.ExpectedSubjectVersion)
	if err != nil {
		return fmt.Errorf("persist investigation AgentRun checkpoint: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return fmt.Errorf("read investigation AgentRun update: %w", err)
		}
		return asyncjob.ErrSubjectVersionMismatch
	}
	metadata, _ := json.Marshal(map[string]any{
		"agent_run_id": snapshot.RunPublicID, "task_public_id": snapshot.Task.PublicID,
		"cycle_no": snapshot.Task.CycleNo, "step_node": stepNode, "terminal_outcome": checkpoint.TerminalOutcome,
	})
	if _, err := tx.ExecContext(ctx, `INSERT INTO incident_events
 (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version, event_type,
  idempotency_key, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
 VALUES (?, ?, 3, ?, 1, 'agent_step_completed', ?, 'system', ?, ?, ?, ?, ?)
 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		deterministicPublicID("investigation-event", snapshot.Task.DedupeKey), snapshot.Task.IncidentID, snapshot.Task.CycleNo,
		hashCanonical("event", snapshot.Task.DedupeKey, "agent_step_completed"), snapshot.RunPublicID,
		boundInvestigation(checkpoint.StepSummary, 2048), metadata, checkpoint.CapturedAt.UTC(), checkpoint.CapturedAt.UTC()); err != nil {
		return fmt.Errorf("append investigation step event: %w", err)
	}
	if terminal {
		if err := assessInvestigationChangeCandidates(ctx, tx, snapshot, checkpoint); err != nil {
			return err
		}
		if err := o.enqueueRemediationPrepare(ctx, tx, snapshot, checkpoint); err != nil {
			return err
		}
	} else {
		if err := o.enqueueNextInvestigationStep(ctx, tx, snapshot, state, checkpoint); err != nil {
			return err
		}
	}
	return nil
}

func (o *investigationStepOperation) enqueueRemediationPrepare(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, checkpoint investigationStepCheckpoint) error {
	diagnosis := checkpoint.Diagnosis
	if checkpoint.TerminalOutcome != "diagnosed" || diagnosis == nil ||
		diagnosis.Candidate.Confidence != agent.DiagnosisConfirmed ||
		diagnosis.Candidate.RemediationHint != agent.RemediationRestoreRequiredEnv {
		return nil
	}
	payload, err := json.Marshal(remediationPreparePayload{
		AgentRunID: snapshot.RunPublicID,
		CycleNo:    uint64(snapshot.Task.CycleNo),
	})
	if err != nil {
		return fmt.Errorf("encode remediation.prepare payload: %w", err)
	}
	nextSubjectVersion := snapshot.Task.ExpectedSubjectVersion + 1
	dedupe := hashCanonical("task", snapshot.RunPublicID, "remediation.prepare", fmt.Sprint(nextSubjectVersion))
	availableAt := checkpoint.CapturedAt.UTC()
	if _, err := o.cfg.Tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: snapshot.Task.IncidentID, CycleNo: snapshot.Task.CycleNo, Type: asyncjob.TaskRemediationPrepare,
		SubjectType: "agent_run", SubjectID: snapshot.Task.SubjectID, Transition: "remediation.prepare",
		ExpectedSubjectVersion: nextSubjectVersion, PayloadSchemaVersion: remediationPreparePayloadSchema,
		Payload: payload, DedupeKey: dedupe, Priority: snapshot.Task.Priority,
		AvailableAt: &availableAt, MaxAttempts: snapshot.Task.MaxAttempts,
	}); err != nil {
		return fmt.Errorf("enqueue remediation.prepare: %w", err)
	}
	return nil
}

func stepArguments(checkpoint investigationStepCheckpoint) (json.RawMessage, string) {
	var value any = map[string]any{"mode": checkpoint.Mode, "basis_checkpoint_version": checkpoint.BasisCheckpointVersion}
	if checkpoint.Action != nil {
		value = checkpoint.Action
	}
	if checkpoint.Diagnosis != nil {
		value = checkpoint.Diagnosis
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		encoded = []byte(`{"mode":"bounded"}`)
	}
	return encoded, hashBytesInvestigation(encoded)
}

func selectedTool(checkpoint investigationStepCheckpoint) string {
	if checkpoint.Action != nil {
		return boundInvestigation(checkpoint.Action.Tool, 128)
	}
	return ""
}

func insertInvestigationEvidence(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, checkpoint investigationStepCheckpoint, publicID string) error {
	observation := *checkpoint.Observation
	facts, err := evidenceEnvelope(observation)
	if err != nil {
		return fmt.Errorf("encode investigation Evidence: %w", err)
	}
	if len(facts) > snapshot.Limits.MaxEvidenceBytes {
		return fmt.Errorf("%w: investigation Evidence exceeds its bound", asyncjob.ErrBusinessBudgetExceeded)
	}
	producerKey := hashCanonical("agent-step", snapshot.RunPublicID, observation.SourceSystem, observation.CollectionPath, observation.TemplateVersion, observation.ContentHash)
	idempotencyKey := hashCanonical("agent-evidence", snapshot.Task.DedupeKey, observation.ContentHash)
	result, err := tx.ExecContext(ctx, `INSERT INTO evidence_items
 (public_id, incident_id, domain_schema_version, cycle_no, agent_run_id, type, source,
  producer_type, producer_dedupe_key, tool_name, resource_ref, time_range_json, query_text,
  summary, facts_json, result_hash, content_hash, raw_ref, redaction_json, truncated, valid,
  idempotency_key, collected_at, created_at)
 VALUES (?, ?, 3, ?, ?, 'agent_observation', ?, 'agent_step', ?, ?, ?, NULL, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		publicID, snapshot.Task.IncidentID, snapshot.Task.CycleNo, snapshot.Task.SubjectID,
		boundInvestigation(observation.SourceSystem, 128), producerKey, selectedTool(checkpoint),
		boundInvestigation(observation.CollectionPath, 1024), boundInvestigation(observation.Summary, 4096), facts,
		observation.ContentHash, observation.ContentHash, boundInvestigation(observation.SafeDeepLink, 1024),
		json.RawMessage(`{"policy":"v3-observation-redaction","raw_text_omitted":true}`), observation.Truncated,
		observation.Status != agent.CollectionInvalid, idempotencyKey, checkpoint.CapturedAt.UTC(), checkpoint.CapturedAt.UTC())
	if err != nil {
		return fmt.Errorf("persist investigation Evidence: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read investigation Evidence result: %w", err)
	}
	return nil
}

type investigationChangeCandidate struct {
	PublicID           string
	ChangeRef          string
	Repository         string
	Revision           string
	ImageDigest        string
	TargetPath         string
	Category           string
	ChangeTime         time.Time
	SupportingEvidence []string
	ContentHash        string
}

func insertInvestigationChangeCandidates(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, checkpoint investigationStepCheckpoint, evidencePublicID string) error {
	candidates, err := buildInvestigationChangeCandidates(snapshot, checkpoint, evidencePublicID)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		supporting, marshalErr := json.Marshal(candidate.SupportingEvidence)
		if marshalErr != nil {
			return fmt.Errorf("encode investigation ChangeCandidate evidence: %w", marshalErr)
		}
		result, execErr := tx.ExecContext(ctx, `INSERT INTO change_candidates
 (public_id, domain_schema_version, candidate_schema_version, incident_id, cycle_no, agent_run_id,
  change_ref, source_type, repository, commit_sha, gitops_revision, image_digest, target_path,
  category, change_time, supporting_evidence_json, content_hash, created_at)
 VALUES (?, 3, 1, ?, ?, ?, ?, 'github_commit', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
			candidate.PublicID, snapshot.Task.IncidentID, snapshot.Task.CycleNo, snapshot.Task.SubjectID,
			candidate.ChangeRef, candidate.Repository, candidate.Revision, candidate.Revision, candidate.ImageDigest,
			candidate.TargetPath, candidate.Category, candidate.ChangeTime.UTC(), supporting, candidate.ContentHash,
			checkpoint.CapturedAt.UTC())
		if execErr != nil {
			return fmt.Errorf("persist investigation ChangeCandidate: %w", execErr)
		}
		candidateID, lastIDErr := result.LastInsertId()
		if lastIDErr != nil || candidateID <= 0 {
			if lastIDErr != nil {
				return fmt.Errorf("read investigation ChangeCandidate result: %w", lastIDErr)
			}
			return fmt.Errorf("%w: persisted ChangeCandidate returned an invalid id", asyncjob.ErrInvalidMutation)
		}
		if err := verifyInvestigationChangeCandidate(ctx, tx, uint64(candidateID), snapshot, candidate); err != nil {
			return err
		}
	}
	return nil
}

func buildInvestigationChangeCandidates(snapshot investigationSnapshot, checkpoint investigationStepCheckpoint, evidencePublicID string) ([]investigationChangeCandidate, error) {
	observation := checkpoint.Observation
	if observation == nil || observation.CollectionPath != "argocd/deployment-context" || observation.Status != agent.CollectionAvailable {
		return nil, nil
	}
	result := make([]investigationChangeCandidate, 0, 10)
	seenRefs := make(map[string]struct{}, 10)
	for _, fact := range observation.Facts {
		if fact.Type != "deployment.change_ref" {
			continue
		}
		if !usableInvestigationCandidateFact(fact, snapshot) || fact.EvidenceID != evidencePublicID {
			return nil, fmt.Errorf("%w: deployment ChangeCandidate is not bound to the persisted Evidence", asyncjob.ErrInvalidMutation)
		}
		changeRef := strings.TrimSpace(fact.Attributes["change_ref"])
		repository := strings.ToLower(strings.Trim(strings.TrimSpace(fact.Attributes["repository"]), "/"))
		revision := strings.ToLower(strings.TrimSpace(fact.Attributes["revision"]))
		imageDigest := strings.ToLower(strings.TrimSpace(fact.Attributes["image_digest"]))
		targetPath := strings.TrimPrefix(strings.TrimSpace(fact.Attributes["path"]), "./")
		changeTime, timeErr := time.Parse(time.RFC3339, strings.TrimSpace(fact.Attributes["deployed_at"]))
		isCurrent, currentErr := strconv.ParseBool(strings.TrimSpace(fact.Attributes["is_current"]))
		if _, parseErr := uuid.Parse(changeRef); parseErr != nil || strings.Count(repository, "/") != 1 ||
			!change.ValidExactGitObjectID(revision) || !validImageDigest(imageDigest) || targetPath == "" ||
			strings.HasPrefix(targetPath, "../") || change.SensitivePath(targetPath, nil) || timeErr != nil || currentErr != nil {
			return nil, fmt.Errorf("%w: deployment ChangeCandidate identity is invalid", asyncjob.ErrInvalidMutation)
		}
		if _, duplicate := seenRefs[changeRef]; duplicate {
			return nil, fmt.Errorf("%w: deployment context contains duplicate change references", asyncjob.ErrInvalidMutation)
		}
		seenRefs[changeRef] = struct{}{}
		category := "low_confidence"
		if isCurrent {
			category = "high_confidence"
		}
		supporting := []string{evidencePublicID}
		candidate := investigationChangeCandidate{
			ChangeRef: changeRef, Repository: repository, Revision: revision, ImageDigest: imageDigest,
			TargetPath: targetPath, Category: category, ChangeTime: changeTime.UTC(),
			SupportingEvidence: supporting,
		}
		contentHash, hashErr := investigationChangeCandidateContentHash(snapshot, candidate)
		if hashErr != nil {
			return nil, hashErr
		}
		candidate.ContentHash = contentHash
		candidate.PublicID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("cloudops-change-candidate\x00"+snapshot.RunPublicID+"\x00"+changeRef+"\x00"+contentHash)).String()
		result = append(result, candidate)
	}
	return result, nil
}

func investigationChangeCandidateContentHash(snapshot investigationSnapshot, candidate investigationChangeCandidate) (string, error) {
	canonical, err := json.Marshal(struct {
		SchemaVersion          int
		CandidateSchemaVersion int
		IncidentID             string
		AgentRunID             string
		CycleNo                uint64
		ChangeRef              string
		SourceType             string
		Repository             string
		Revision               string
		ImageDigest            string
		TargetPath             string
		Category               string
		ChangeTime             time.Time
		Supporting             []string
	}{
		SchemaVersion: 3, CandidateSchemaVersion: 1,
		IncidentID: snapshot.IncidentPublicID, AgentRunID: snapshot.RunPublicID, CycleNo: uint64(snapshot.Task.CycleNo),
		ChangeRef: candidate.ChangeRef, SourceType: "github_commit", Repository: candidate.Repository,
		Revision: candidate.Revision, ImageDigest: candidate.ImageDigest, TargetPath: candidate.TargetPath,
		Category: candidate.Category, ChangeTime: candidate.ChangeTime.UTC(), Supporting: candidate.SupportingEvidence,
	})
	if err != nil {
		return "", fmt.Errorf("encode investigation ChangeCandidate hash input: %w", err)
	}
	return hashBytesInvestigation(canonical), nil
}

func usableInvestigationCandidateFact(fact agent.EvidenceFact, snapshot investigationSnapshot) bool {
	return fact.ID != "" && fact.EvidenceID != "" && fact.IncidentID == snapshot.IncidentPublicID &&
		fact.CycleNo == uint64(snapshot.Task.CycleNo) && fact.CollectionStatus == agent.CollectionAvailable &&
		fact.Integrity == "verified" && fact.ClaimUse != "forbidden" && !fact.Truncated
}

func verifyInvestigationChangeCandidate(ctx context.Context, tx asyncjob.DBTX, candidateID uint64, snapshot investigationSnapshot, expected investigationChangeCandidate) error {
	var actual investigationChangeCandidate
	var incidentID, cycleNo, agentRunID uint64
	var domainSchemaVersion, candidateSchemaVersion uint16
	var sourceType, commitSHA, gitopsRevision string
	var supportingJSON []byte
	if err := tx.QueryRowContext(ctx, `SELECT public_id, domain_schema_version, candidate_schema_version,
 incident_id, cycle_no, agent_run_id, change_ref, source_type, repository, commit_sha,
 gitops_revision, image_digest, target_path, category, change_time, supporting_evidence_json, content_hash
FROM change_candidates WHERE id = ? FOR UPDATE`, candidateID).Scan(
		&actual.PublicID, &domainSchemaVersion, &candidateSchemaVersion, &incidentID, &cycleNo, &agentRunID,
		&actual.ChangeRef, &sourceType, &actual.Repository, &commitSHA, &gitopsRevision, &actual.ImageDigest,
		&actual.TargetPath, &actual.Category, &actual.ChangeTime, &supportingJSON, &actual.ContentHash); err != nil {
		return fmt.Errorf("reload investigation ChangeCandidate: %w", err)
	}
	actual.Revision = gitopsRevision
	if json.Unmarshal(supportingJSON, &actual.SupportingEvidence) != nil ||
		domainSchemaVersion != 3 || candidateSchemaVersion != 1 || incidentID != snapshot.Task.IncidentID ||
		cycleNo != uint64(snapshot.Task.CycleNo) || agentRunID != snapshot.Task.SubjectID || sourceType != "github_commit" ||
		commitSHA != gitopsRevision || actual.PublicID != expected.PublicID ||
		actual.ChangeRef != expected.ChangeRef || actual.Repository != expected.Repository || actual.Revision != expected.Revision ||
		actual.ImageDigest != expected.ImageDigest || actual.TargetPath != expected.TargetPath || actual.Category != expected.Category ||
		!actual.ChangeTime.UTC().Equal(expected.ChangeTime.UTC()) || !slices.Equal(actual.SupportingEvidence, expected.SupportingEvidence) ||
		actual.ContentHash != expected.ContentHash {
		return fmt.Errorf("%w: idempotent ChangeCandidate replay diverged from immutable payload", asyncjob.ErrInvalidMutation)
	}
	return nil
}

const investigationChangeValidatorVersion = "required-env-correlation-validator/v1"

type persistedInvestigationChangeCandidate struct {
	ID uint64
	investigationChangeCandidate
}

type investigationChangeAssessment struct {
	Status                string
	SupportingEvidence    []string
	ContradictingEvidence []string
	ValidatorVersion      string
	PolicyHash            string
	DiagnosisHash         string
}

func assessInvestigationChangeCandidates(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, checkpoint investigationStepCheckpoint) error {
	if checkpoint.TerminalOutcome == "" {
		return nil
	}
	policyHash := checkpoint.State.Coverage.ClaimPolicyHash
	diagnosisHash := ""
	if checkpoint.Diagnosis != nil {
		policyHash = checkpoint.Diagnosis.ClaimPolicyHash
		diagnosisHash = checkpoint.Diagnosis.DiagnosisHash
	}
	if !validSHA256Text(policyHash) || checkpoint.Diagnosis != nil && !validSHA256Text(diagnosisHash) {
		return fmt.Errorf("%w: terminal ChangeCandidate assessment lacks policy-bound identity", asyncjob.ErrInvalidMutation)
	}
	candidates, err := loadInvestigationChangeCandidatesForAssessment(ctx, tx, snapshot)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		assessment, assessErr := buildInvestigationChangeAssessment(snapshot, checkpoint, candidate, policyHash, diagnosisHash)
		if assessErr != nil {
			return assessErr
		}
		if err := persistInvestigationChangeAssessment(ctx, tx, snapshot, checkpoint, candidate, assessment); err != nil {
			return err
		}
	}
	return nil
}

func loadInvestigationChangeCandidatesForAssessment(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot) ([]persistedInvestigationChangeCandidate, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, public_id, change_ref, repository, commit_sha, gitops_revision,
 image_digest, target_path, category, change_time, supporting_evidence_json, content_hash
FROM change_candidates
WHERE incident_id = ? AND cycle_no = ? AND agent_run_id = ? AND domain_schema_version = 3
ORDER BY id FOR UPDATE`, snapshot.Task.IncidentID, snapshot.Task.CycleNo, snapshot.Task.SubjectID)
	if err != nil {
		return nil, fmt.Errorf("load investigation ChangeCandidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]persistedInvestigationChangeCandidate, 0, 10)
	for rows.Next() {
		var candidate persistedInvestigationChangeCandidate
		var commitSHA, gitopsRevision string
		var supportingJSON []byte
		if err := rows.Scan(&candidate.ID, &candidate.PublicID, &candidate.ChangeRef, &candidate.Repository,
			&commitSHA, &gitopsRevision, &candidate.ImageDigest, &candidate.TargetPath, &candidate.Category,
			&candidate.ChangeTime, &supportingJSON, &candidate.ContentHash); err != nil {
			return nil, fmt.Errorf("scan investigation ChangeCandidate: %w", err)
		}
		if json.Unmarshal(supportingJSON, &candidate.SupportingEvidence) != nil || commitSHA != gitopsRevision {
			return nil, fmt.Errorf("%w: persisted ChangeCandidate is malformed", asyncjob.ErrInvalidMutation)
		}
		candidate.Revision = gitopsRevision
		if err := validatePersistedInvestigationChangeCandidate(snapshot, candidate); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate investigation ChangeCandidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close investigation ChangeCandidates before assessment writes: %w", err)
	}
	return candidates, nil
}

func validatePersistedInvestigationChangeCandidate(snapshot investigationSnapshot, candidate persistedInvestigationChangeCandidate) error {
	if candidate.ID == 0 || !validSHA256Text(candidate.ContentHash) || candidate.ChangeTime.IsZero() ||
		!change.ValidExactGitObjectID(candidate.Revision) || !validImageDigest(candidate.ImageDigest) ||
		strings.Count(candidate.Repository, "/") != 1 || candidate.TargetPath == "" ||
		strings.HasPrefix(candidate.TargetPath, "../") || change.SensitivePath(candidate.TargetPath, nil) ||
		candidate.Category != "high_confidence" && candidate.Category != "low_confidence" {
		return fmt.Errorf("%w: persisted ChangeCandidate identity is invalid", asyncjob.ErrInvalidMutation)
	}
	if _, err := uuid.Parse(candidate.ChangeRef); err != nil {
		return fmt.Errorf("%w: persisted ChangeCandidate change_ref is invalid", asyncjob.ErrInvalidMutation)
	}
	candidate.SupportingEvidence = stableUniqueInvestigation(candidate.SupportingEvidence)
	if len(candidate.SupportingEvidence) == 0 || len(candidate.SupportingEvidence) > 64 {
		return fmt.Errorf("%w: persisted ChangeCandidate evidence is invalid", asyncjob.ErrInvalidMutation)
	}
	expectedHash, err := investigationChangeCandidateContentHash(snapshot, candidate.investigationChangeCandidate)
	if err != nil {
		return err
	}
	expectedPublicID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("cloudops-change-candidate\x00"+snapshot.RunPublicID+"\x00"+candidate.ChangeRef+"\x00"+expectedHash)).String()
	if candidate.ContentHash != expectedHash || candidate.PublicID != expectedPublicID {
		return fmt.Errorf("%w: persisted ChangeCandidate hash binding is invalid", asyncjob.ErrInvalidMutation)
	}
	return nil
}

func buildInvestigationChangeAssessment(snapshot investigationSnapshot, checkpoint investigationStepCheckpoint, candidate persistedInvestigationChangeCandidate, policyHash, diagnosisHash string) (investigationChangeAssessment, error) {
	assessment := investigationChangeAssessment{
		Status: "unknown", SupportingEvidence: []string{}, ContradictingEvidence: []string{},
		ValidatorVersion: investigationChangeValidatorVersion, PolicyHash: policyHash, DiagnosisHash: diagnosisHash,
	}
	positive := append([]string(nil), candidate.SupportingEvidence...)
	contradicting := make([]string, 0, 4)
	current, deployed, argoNotDeployed := false, false, false
	sourceUnchanged, imageUnchanged, identityChanged := false, false, false
	detailRemoved, detailNotRemoved, ciSucceeded := false, false, false
	for _, fact := range snapshot.Facts {
		if !usableInvestigationCandidateFact(fact, snapshot) {
			continue
		}
		switch fact.Type {
		case "deployment.change_ref":
			if !slices.Contains(candidate.SupportingEvidence, fact.EvidenceID) || !sameInvestigationCandidateFact(fact, candidate, false) {
				continue
			}
			value, err := strconv.ParseBool(strings.TrimSpace(fact.Attributes["is_current"]))
			if err != nil {
				return investigationChangeAssessment{}, fmt.Errorf("%w: candidate deployment current marker is invalid", asyncjob.ErrInvalidMutation)
			}
			current = current || value
			positive = append(positive, fact.EvidenceID)
		case "argocd.bad_revision_deployed":
			if slices.Contains(candidate.SupportingEvidence, fact.EvidenceID) && strings.EqualFold(fact.Attributes["deployed_revision"], candidate.Revision) {
				deployed = true
				positive = append(positive, fact.EvidenceID)
			}
		case "argocd.bad_revision_not_deployed":
			if slices.Contains(candidate.SupportingEvidence, fact.EvidenceID) && strings.EqualFold(fact.Attributes["deployed_revision"], candidate.Revision) {
				argoNotDeployed = true
				contradicting = append(contradicting, fact.EvidenceID)
			}
		case "source_revision.unchanged":
			if slices.Contains(candidate.SupportingEvidence, fact.EvidenceID) {
				sourceUnchanged = true
				positive = append(positive, fact.EvidenceID)
			}
		case "image_digest.unchanged":
			if slices.Contains(candidate.SupportingEvidence, fact.EvidenceID) && strings.EqualFold(fact.Attributes["image_digest"], candidate.ImageDigest) {
				imageUnchanged = true
				positive = append(positive, fact.EvidenceID)
			}
		case "deployment.source_and_image_changed":
			if slices.Contains(candidate.SupportingEvidence, fact.EvidenceID) {
				identityChanged = true
				contradicting = append(contradicting, fact.EvidenceID)
			}
		case "gitops.required_env_removed":
			if sameInvestigationCandidateFact(fact, candidate, true) {
				detailRemoved = true
				positive = append(positive, fact.EvidenceID)
			}
		case "gitops.required_env_not_removed":
			if sameInvestigationCandidateFact(fact, candidate, true) {
				detailNotRemoved = true
				contradicting = append(contradicting, fact.EvidenceID)
			}
		case "change.ci_succeeded":
			if sameInvestigationCandidateFact(fact, candidate, false) {
				ciSucceeded = true
				positive = append(positive, fact.EvidenceID)
			}
		case "change.ci_not_succeeded":
			if sameInvestigationCandidateFact(fact, candidate, false) {
				contradicting = append(contradicting, fact.EvidenceID)
			}
		}
	}
	positive = stableUniqueInvestigation(positive)
	contradicting = stableUniqueInvestigation(contradicting)
	if detailNotRemoved && !detailRemoved || argoNotDeployed && !deployed || identityChanged && !(sourceUnchanged && imageUnchanged) {
		assessment.Status = "excluded"
		assessment.SupportingEvidence = stableUniqueInvestigation(candidate.SupportingEvidence)
		assessment.ContradictingEvidence = contradicting
		return assessment, nil
	}
	diagnosis := checkpoint.Diagnosis
	if diagnosis != nil && diagnosis.Candidate.Confidence == agent.DiagnosisConfirmed &&
		diagnosis.Candidate.ClaimType == agent.GoldenRequiredEnvClaimPolicy().ClaimType &&
		diagnosis.Candidate.RemediationHint == agent.RemediationRestoreRequiredEnv &&
		current && deployed && sourceUnchanged && imageUnchanged && detailRemoved && !detailNotRemoved && ciSucceeded &&
		allInvestigationEvidenceReferenced(positive, diagnosis.EvidenceIDs) && len(contradicting) == 0 {
		assessment.Status = "matched"
		assessment.SupportingEvidence = positive
		return assessment, nil
	}
	for _, evidenceID := range positive {
		if checkpoint.Diagnosis != nil && slices.Contains(checkpoint.Diagnosis.EvidenceIDs, evidenceID) {
			assessment.SupportingEvidence = append(assessment.SupportingEvidence, evidenceID)
		}
	}
	assessment.SupportingEvidence = stableUniqueInvestigation(assessment.SupportingEvidence)
	assessment.ContradictingEvidence = contradicting
	return assessment, nil
}

func sameInvestigationCandidateFact(fact agent.EvidenceFact, candidate persistedInvestigationChangeCandidate, requirePath bool) bool {
	if strings.TrimSpace(fact.Attributes["change_ref"]) != candidate.ChangeRef ||
		!strings.EqualFold(strings.Trim(strings.TrimSpace(fact.Attributes["repository"]), "/"), candidate.Repository) ||
		!strings.EqualFold(strings.TrimSpace(fact.Attributes["revision"]), candidate.Revision) {
		return false
	}
	if !requirePath {
		return true
	}
	return strings.TrimPrefix(strings.TrimSpace(fact.Attributes["path"]), "./") == candidate.TargetPath
}

func allInvestigationEvidenceReferenced(required, actual []string) bool {
	for _, evidenceID := range stableUniqueInvestigation(required) {
		if !slices.Contains(actual, evidenceID) {
			return false
		}
	}
	return len(required) > 0
}

func persistInvestigationChangeAssessment(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, checkpoint investigationStepCheckpoint, candidate persistedInvestigationChangeCandidate, assessment investigationChangeAssessment) error {
	var latestID uint64
	var latestStatus, latestValidator, latestPolicy, latestHash string
	var latestSupportingJSON, latestContradictingJSON []byte
	var latestSupersedes sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, status, supporting_evidence_json, contradicting_evidence_json,
 validator_version, policy_hash, content_hash, supersedes_assessment_id
FROM change_candidate_assessments
WHERE candidate_id = ?
ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE`, candidate.ID).Scan(
		&latestID, &latestStatus, &latestSupportingJSON, &latestContradictingJSON,
		&latestValidator, &latestPolicy, &latestHash, &latestSupersedes)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load latest ChangeCandidate assessment: %w", err)
	}
	if err == nil {
		var latestSupporting, latestContradicting []string
		if json.Unmarshal(latestSupportingJSON, &latestSupporting) != nil || json.Unmarshal(latestContradictingJSON, &latestContradicting) != nil ||
			latestID == 0 || !validSHA256Text(latestHash) {
			return fmt.Errorf("%w: latest ChangeCandidate assessment is malformed", asyncjob.ErrInvalidMutation)
		}
		latestSupersedesID := uint64(0)
		if latestSupersedes.Valid {
			latestSupersedesID = uint64(latestSupersedes.Int64)
		}
		expectedLatestHash, hashErr := investigationChangeAssessmentContentHash(candidate, assessment, latestSupersedesID)
		if hashErr != nil {
			return hashErr
		}
		if latestStatus == assessment.Status && latestValidator == assessment.ValidatorVersion && latestPolicy == assessment.PolicyHash &&
			slices.Equal(latestSupporting, assessment.SupportingEvidence) && slices.Equal(latestContradicting, assessment.ContradictingEvidence) &&
			latestHash == expectedLatestHash {
			return nil
		}
	}
	supersedesID := latestID
	contentHash, err := investigationChangeAssessmentContentHash(candidate, assessment, supersedesID)
	if err != nil {
		return err
	}
	publicID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("cloudops-change-assessment\x00"+candidate.PublicID+"\x00"+contentHash)).String()
	supporting, err := json.Marshal(assessment.SupportingEvidence)
	if err != nil {
		return fmt.Errorf("encode ChangeCandidate assessment supporting Evidence: %w", err)
	}
	contradicting, err := json.Marshal(assessment.ContradictingEvidence)
	if err != nil {
		return fmt.Errorf("encode ChangeCandidate assessment contradicting Evidence: %w", err)
	}
	var supersedes any
	if supersedesID != 0 {
		supersedes = supersedesID
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO change_candidate_assessments
 (public_id, domain_schema_version, assessment_schema_version, incident_id, cycle_no, candidate_id,
  status, supporting_evidence_json, contradicting_evidence_json, validator_version, policy_hash,
  content_hash, supersedes_assessment_id, created_at)
 VALUES (?, 3, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, publicID, snapshot.Task.IncidentID,
		snapshot.Task.CycleNo, candidate.ID, assessment.Status, supporting, contradicting,
		assessment.ValidatorVersion, assessment.PolicyHash, contentHash, supersedes, checkpoint.CapturedAt.UTC())
	if err != nil {
		return fmt.Errorf("persist ChangeCandidate assessment: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return fmt.Errorf("read ChangeCandidate assessment result: %w", err)
		}
		return fmt.Errorf("%w: ChangeCandidate assessment insert affected %d rows", asyncjob.ErrInvalidMutation, affected)
	}
	return nil
}

func investigationChangeAssessmentContentHash(candidate persistedInvestigationChangeCandidate, assessment investigationChangeAssessment, supersedesID uint64) (string, error) {
	canonical, err := json.Marshal(struct {
		SchemaVersion           int
		AssessmentSchemaVersion int
		CandidatePublicID       string
		CandidateHash           string
		Status                  string
		Supporting              []string
		Contradicting           []string
		ValidatorVersion        string
		PolicyHash              string
		DiagnosisHash           string
		SupersedesAssessmentID  uint64
	}{
		SchemaVersion: 3, AssessmentSchemaVersion: 1, CandidatePublicID: candidate.PublicID,
		CandidateHash: candidate.ContentHash, Status: assessment.Status,
		Supporting: assessment.SupportingEvidence, Contradicting: assessment.ContradictingEvidence,
		ValidatorVersion: assessment.ValidatorVersion, PolicyHash: assessment.PolicyHash,
		DiagnosisHash: assessment.DiagnosisHash, SupersedesAssessmentID: supersedesID,
	})
	if err != nil {
		return "", fmt.Errorf("encode ChangeCandidate assessment hash input: %w", err)
	}
	return hashBytesInvestigation(canonical), nil
}

func (o *investigationStepOperation) enqueueNextInvestigationStep(ctx context.Context, tx asyncjob.DBTX, snapshot investigationSnapshot, state agent.InvestigationState, checkpoint investigationStepCheckpoint) error {
	nextMode := checkpoint.NextMode
	if nextMode == "" {
		return fmt.Errorf("%w: non-terminal investigation step has no next mode", asyncjob.ErrInvalidMutation)
	}
	payload := investigationStepPayload{Mode: nextMode, AgentRunID: snapshot.RunPublicID, CycleNo: uint64(snapshot.Task.CycleNo), BasisCheckpointVersion: state.CheckpointVersion, Action: cloneAction(checkpoint.NextAction)}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode next investigation step payload: %w", err)
	}
	nextSubjectVersion := snapshot.Task.ExpectedSubjectVersion + 1
	dedupe := hashCanonical("task", snapshot.RunPublicID, "investigation.step", fmt.Sprint(nextSubjectVersion))
	if _, err := o.cfg.Tasks.EnqueueIn(ctx, tx, asyncjob.NewTask{
		IncidentID: snapshot.Task.IncidentID, CycleNo: snapshot.Task.CycleNo, Type: asyncjob.TaskInvestigationAdvance,
		SubjectType: "agent_run", SubjectID: snapshot.Task.SubjectID, Transition: "investigation.step",
		ExpectedSubjectVersion: nextSubjectVersion, PayloadSchemaVersion: investigationStepPayloadSchema,
		Payload: payloadJSON, DedupeKey: dedupe, Priority: snapshot.Task.Priority, MaxAttempts: snapshot.Task.MaxAttempts,
	}); err != nil {
		return fmt.Errorf("enqueue next investigation step: %w", err)
	}
	return nil
}

func (o *investigationStepOperation) failRunMutation(snapshot investigationSnapshot, reason string) asyncjob.Mutation {
	at := o.cfg.Now().UTC()
	return func(ctx context.Context, tx asyncjob.DBTX) error {
		result, err := tx.ExecContext(ctx, `UPDATE agent_runs
	SET status = 'FAILED', v3_status = 'failed', failure_code = ?, failure_summary = ?,
	    completed_at = ?, row_version = row_version + 1, updated_at = ?
WHERE id = ? AND incident_id = ? AND domain_schema_version = 3 AND cycle_no = ?
  AND status IN ('PENDING','RUNNING') AND v3_status IN ('pending','running')
  AND row_version = ?`, reason, boundInvestigation(reason, 2048), at, at,
			snapshot.Task.SubjectID, snapshot.Task.IncidentID, snapshot.Task.CycleNo, snapshot.Task.ExpectedSubjectVersion)
		if err != nil {
			return fmt.Errorf("mark investigation AgentRun failed: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return asyncjob.ErrSubjectVersionMismatch
		}
		metadata, _ := json.Marshal(map[string]any{"agent_run_id": snapshot.RunPublicID, "task_public_id": snapshot.Task.PublicID, "reason": reason})
		if _, err := tx.ExecContext(ctx, `INSERT INTO incident_events
 (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version, event_type,
  idempotency_key, actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
 VALUES (?, ?, 3, ?, 1, 'agent_run_failed', ?, 'system', ?, ?, ?, ?, ?)
 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
			deterministicPublicID("investigation-failure-event", snapshot.Task.PublicID), snapshot.Task.IncidentID,
			snapshot.Task.CycleNo, hashCanonical("event", snapshot.Task.PublicID, "agent_run_failed"),
			snapshot.RunPublicID, "investigation AgentRun failed", metadata, at, at); err != nil {
			return fmt.Errorf("append investigation failure event: %w", err)
		}
		return nil
	}
}
