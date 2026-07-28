package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// InvestigationState is the durable, model-independent view of one bounded
// investigation. It intentionally contains identifiers and typed summaries,
// never provider responses, prompts, credentials, or raw log/diff payloads.
type InvestigationState struct {
	SchemaVersion      int                  `json:"schema_version"`
	RunID              string               `json:"run_id"`
	IncidentID         string               `json:"incident_id"`
	CycleNo            uint64               `json:"cycle_no"`
	IncidentVersion    uint64               `json:"incident_version"`
	Correlation        CorrelationSnapshot  `json:"correlation"`
	Objective          string               `json:"objective"`
	Window             QueryWindow          `json:"window"`
	Coverage           CoverageRequirements `json:"coverage"`
	Hypotheses         []HypothesisState    `json:"hypotheses"`
	Questions          []OpenQuestion       `json:"open_questions"`
	Evidence           []EvidenceReference  `json:"evidence"`
	ToolAttempts       []ToolAttempt        `json:"tool_attempts"`
	UnavailableSources []UnavailableSource  `json:"unavailable_sources"`
	Usage              Usage                `json:"usage"`
	Limits             Limits               `json:"limits"`
	NextNode           Node                 `json:"next_node"`
	TerminalOutcome    string               `json:"terminal_outcome,omitempty"`
	LastAppliedDelta   string               `json:"last_applied_delta,omitempty"`
	CheckpointVersion  uint64               `json:"checkpoint_version"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

type CorrelationSnapshot struct {
	Cluster     string `json:"cluster"`
	Environment string `json:"environment"`
	Namespace   string `json:"namespace"`
	Workload    string `json:"workload"`
	TargetKind  string `json:"target_kind"`
}

type QueryWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type CoverageRequirements struct {
	ClaimType          string   `json:"claim_type"`
	ClaimPolicyVersion string   `json:"claim_policy_version"`
	ClaimPolicyHash    string   `json:"claim_policy_hash"`
	ActionPolicyHash   string   `json:"action_policy_hash"`
	RequiredFacets     []string `json:"required_facets"`
	RequiredSources    []string `json:"required_sources,omitempty"`
}

type HypothesisStatus string

const (
	HypothesisActive    HypothesisStatus = "active"
	HypothesisSupported HypothesisStatus = "supported"
	HypothesisWeakened  HypothesisStatus = "weakened"
	HypothesisRejected  HypothesisStatus = "rejected"
)

type HypothesisState struct {
	ID         string           `json:"id"`
	Statement  string           `json:"statement"`
	Status     HypothesisStatus `json:"status"`
	EvidenceID []string         `json:"evidence_ids"`
}

type OpenQuestion struct {
	ID         string   `json:"id"`
	Question   string   `json:"question"`
	Answer     string   `json:"answer,omitempty"`
	EvidenceID []string `json:"evidence_ids,omitempty"`
	Closed     bool     `json:"closed"`
}

type EvidenceReference struct {
	ID       string `json:"id"`
	FactType string `json:"fact_type"`
}

type ToolAttempt struct {
	Signature string    `json:"signature"`
	Tool      string    `json:"tool"`
	Status    string    `json:"status"`
	Attempted time.Time `json:"attempted_at"`
}

type UnavailableSource struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type HypothesisOperation string

const (
	HypothesisAdd     HypothesisOperation = "add"
	HypothesisSupport HypothesisOperation = "support"
	HypothesisWeaken  HypothesisOperation = "weaken"
	HypothesisReject  HypothesisOperation = "reject"
)

type HypothesisOp struct {
	Operation   HypothesisOperation `json:"operation" jsonschema:"enum=add,enum=support,enum=weaken,enum=reject"`
	ID          string              `json:"id"`
	Statement   string              `json:"statement,omitempty"`
	EvidenceIDs []string            `json:"evidence_ids,omitempty"`
}

type QuestionOperation string

const (
	QuestionAdd    QuestionOperation = "add"
	QuestionAnswer QuestionOperation = "answer"
	QuestionClose  QuestionOperation = "close"
)

type QuestionOp struct {
	Operation   QuestionOperation `json:"operation" jsonschema:"enum=add,enum=answer,enum=close"`
	ID          string            `json:"id"`
	Question    string            `json:"question,omitempty"`
	Answer      string            `json:"answer,omitempty"`
	EvidenceIDs []string          `json:"evidence_ids,omitempty"`
}

type StopProposal string

const (
	StopContinue     StopProposal = "continue"
	StopDiagnose     StopProposal = "diagnose"
	StopInsufficient StopProposal = "insufficient"
)

type ProposedAction struct {
	Tool              string          `json:"tool"`
	ScopeRef          string          `json:"scope_ref"`
	TemplateID        string          `json:"template_id"`
	BoundedParameters json.RawMessage `json:"bounded_parameters"`
	ExpectedFactTypes []string        `json:"expected_fact_types"`
	PurposeSummary    string          `json:"purpose_summary"`
}

type StateDelta struct {
	SchemaVersion          int             `json:"schema_version" jsonschema:"enum=1"`
	BasisCheckpointVersion uint64          `json:"basis_checkpoint_version"`
	HypothesisOps          []HypothesisOp  `json:"hypothesis_ops,omitempty"`
	QuestionOps            []QuestionOp    `json:"question_ops,omitempty"`
	ProposedAction         *ProposedAction `json:"proposed_action,omitempty"`
	ProposedStop           StopProposal    `json:"proposed_stop" jsonschema:"enum=continue,enum=diagnose,enum=insufficient"`
}

type ToolActionPolicy struct {
	TemplateIDs       []string
	ParameterKeys     []string
	ParameterSpecs    map[string]ParameterSpec
	ExpectedFactTypes []string
}

type ReducerPolicy struct {
	MaxBytes       int
	AllowedActions map[string]ToolActionPolicy
	AllowedScopes  map[string]struct{}
	Evidence       map[string]EvidenceFact
	StepUsage      Usage
}

// ReduceStateDelta validates and applies a model proposal atomically. The
// input state is copied; callers only persist the returned value after the
// task lease and checkpoint fence have been checked by the repository.
func ReduceStateDelta(state InvestigationState, delta StateDelta, policy ReducerPolicy) (InvestigationState, string, error) {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = InvestigationStateSchemaVersion
	}
	if state.SchemaVersion != InvestigationStateSchemaVersion {
		return InvestigationState{}, "", fmt.Errorf("%w: unsupported investigation state schema", ErrInvalidArgument)
	}
	if delta.SchemaVersion != InvestigationStateSchemaVersion {
		return InvestigationState{}, "", fmt.Errorf("%w: unsupported state delta schema", ErrInvalidArgument)
	}
	if delta.BasisCheckpointVersion != state.CheckpointVersion {
		return InvestigationState{}, "", fmt.Errorf("%w: stale checkpoint basis", ErrConflict)
	}
	maxBytes := policy.MaxBytes
	if maxBytes == 0 {
		maxBytes = 128 * 1024
	}
	encoded, err := json.Marshal(delta)
	if err != nil || len(encoded) > maxBytes {
		return InvestigationState{}, "", fmt.Errorf("%w: state delta exceeds bounded size", ErrInvalidArgument)
	}
	if err := validateStateDelta(delta, state, policy); err != nil {
		return InvestigationState{}, "", err
	}

	result := state
	result.Hypotheses = append([]HypothesisState(nil), state.Hypotheses...)
	result.Questions = append([]OpenQuestion(nil), state.Questions...)
	result.Evidence = append([]EvidenceReference(nil), state.Evidence...)
	result.ToolAttempts = append([]ToolAttempt(nil), state.ToolAttempts...)
	result.UnavailableSources = append([]UnavailableSource(nil), state.UnavailableSources...)
	for _, op := range delta.HypothesisOps {
		switch op.Operation {
		case HypothesisAdd:
			result.Hypotheses = append(result.Hypotheses, HypothesisState{ID: op.ID, Statement: op.Statement, Status: HypothesisActive, EvidenceID: append([]string(nil), op.EvidenceIDs...)})
		default:
			for i := range result.Hypotheses {
				if result.Hypotheses[i].ID != op.ID {
					continue
				}
				result.Hypotheses[i].EvidenceID = appendUniqueStrings(result.Hypotheses[i].EvidenceID, op.EvidenceIDs...)
				switch op.Operation {
				case HypothesisSupport:
					result.Hypotheses[i].Status = HypothesisSupported
				case HypothesisWeaken:
					result.Hypotheses[i].Status = HypothesisWeakened
				case HypothesisReject:
					result.Hypotheses[i].Status = HypothesisRejected
				}
				break
			}
		}
	}
	for _, op := range delta.QuestionOps {
		switch op.Operation {
		case QuestionAdd:
			result.Questions = append(result.Questions, OpenQuestion{ID: op.ID, Question: op.Question})
		default:
			for i := range result.Questions {
				if result.Questions[i].ID != op.ID {
					continue
				}
				result.Questions[i].EvidenceID = appendUniqueStrings(result.Questions[i].EvidenceID, op.EvidenceIDs...)
				switch op.Operation {
				case QuestionAnswer:
					result.Questions[i].Answer = op.Answer
				case QuestionClose:
					result.Questions[i].Closed = true
				}
				break
			}
		}
	}
	if delta.ProposedAction != nil {
		params, _ := canonicalJSON(delta.ProposedAction.BoundedParameters)
		signature := actionSignature(delta.ProposedAction.Tool, delta.ProposedAction.TemplateID, delta.ProposedAction.ScopeRef, params)
		result.ToolAttempts = append(result.ToolAttempts, ToolAttempt{Signature: signature, Tool: delta.ProposedAction.Tool, Status: "proposed"})
		result.NextNode = NodeExecuteTool
	}
	result.Usage.Charge(policy.StepUsage)
	result.CheckpointVersion++
	result.LastAppliedDelta = hashBytes(encoded)
	return result, result.LastAppliedDelta, nil
}

func validateStateDelta(delta StateDelta, state InvestigationState, policy ReducerPolicy) error {
	if delta.ProposedStop == "" {
		return fmt.Errorf("%w: proposed_stop is required", ErrInvalidArgument)
	}
	if delta.ProposedStop != StopContinue && delta.ProposedStop != StopDiagnose && delta.ProposedStop != StopInsufficient {
		return fmt.Errorf("%w: unsupported proposed_stop", ErrInvalidArgument)
	}
	if len(delta.HypothesisOps) > 32 || len(delta.QuestionOps) > 32 {
		return fmt.Errorf("%w: too many state operations", ErrInvalidArgument)
	}
	hypotheses := make(map[string]HypothesisState, len(state.Hypotheses))
	for _, item := range state.Hypotheses {
		hypotheses[item.ID] = item
	}
	questions := make(map[string]OpenQuestion, len(state.Questions))
	for _, item := range state.Questions {
		questions[item.ID] = item
	}
	for _, op := range delta.HypothesisOps {
		if !boundedID(op.ID) {
			return fmt.Errorf("%w: invalid hypothesis id", ErrInvalidArgument)
		}
		switch op.Operation {
		case HypothesisAdd:
			if _, exists := hypotheses[op.ID]; exists || strings.TrimSpace(op.Statement) == "" {
				return fmt.Errorf("%w: duplicate or empty hypothesis", ErrInvalidArgument)
			}
			hypotheses[op.ID] = HypothesisState{ID: op.ID}
		case HypothesisSupport, HypothesisWeaken, HypothesisReject:
			if _, exists := hypotheses[op.ID]; !exists {
				return fmt.Errorf("%w: unknown hypothesis %q", ErrInvalidArgument, op.ID)
			}
		default:
			return fmt.Errorf("%w: unsupported hypothesis operation", ErrInvalidArgument)
		}
		if err := validateEvidenceRefs(op.EvidenceIDs, state.IncidentID, state.CycleNo, policy.Evidence); err != nil {
			return err
		}
	}
	for _, op := range delta.QuestionOps {
		if !boundedID(op.ID) {
			return fmt.Errorf("%w: invalid question id", ErrInvalidArgument)
		}
		switch op.Operation {
		case QuestionAdd:
			if _, exists := questions[op.ID]; exists || strings.TrimSpace(op.Question) == "" {
				return fmt.Errorf("%w: duplicate or empty question", ErrInvalidArgument)
			}
			questions[op.ID] = OpenQuestion{ID: op.ID}
		case QuestionAnswer, QuestionClose:
			if _, exists := questions[op.ID]; !exists {
				return fmt.Errorf("%w: unknown question %q", ErrInvalidArgument, op.ID)
			}
		default:
			return fmt.Errorf("%w: unsupported question operation", ErrInvalidArgument)
		}
		if op.Operation == QuestionAnswer && strings.TrimSpace(op.Answer) == "" {
			return fmt.Errorf("%w: empty question answer", ErrInvalidArgument)
		}
		if err := validateEvidenceRefs(op.EvidenceIDs, state.IncidentID, state.CycleNo, policy.Evidence); err != nil {
			return err
		}
	}
	if delta.ProposedAction != nil {
		action := delta.ProposedAction
		config, ok := policy.AllowedActions[action.Tool]
		if !ok || strings.TrimSpace(action.TemplateID) == "" || strings.TrimSpace(action.ScopeRef) == "" {
			return fmt.Errorf("%w: action is not allowlisted", ErrPermission)
		}
		if len(policy.AllowedScopes) > 0 {
			if _, ok := policy.AllowedScopes[action.ScopeRef]; !ok {
				return fmt.Errorf("%w: action scope is outside incident scope", ErrPermission)
			}
		}
		if !containsString(config.TemplateIDs, action.TemplateID) {
			return fmt.Errorf("%w: action template is not allowlisted", ErrPermission)
		}
		if len(action.BoundedParameters) == 0 || !json.Valid(action.BoundedParameters) {
			return fmt.Errorf("%w: action parameters must be valid JSON", ErrInvalidArgument)
		}
		if err := ValidateActionParameters(action.BoundedParameters, config); err != nil {
			return err
		}
		if strings.TrimSpace(action.PurposeSummary) == "" || len(action.ExpectedFactTypes) == 0 {
			return fmt.Errorf("%w: action purpose and expected facts are required", ErrInvalidArgument)
		}
		for _, factType := range action.ExpectedFactTypes {
			if !containsString(config.ExpectedFactTypes, factType) {
				return fmt.Errorf("%w: expected fact type %q is not allowlisted", ErrPermission, factType)
			}
		}
		canonical, _ := canonicalJSON(action.BoundedParameters)
		signature := actionSignature(action.Tool, action.TemplateID, action.ScopeRef, canonical)
		for _, attempt := range state.ToolAttempts {
			if attempt.Signature == signature && attempt.Status != "transient_failure" {
				return fmt.Errorf("%w: duplicate tool signature", ErrConflict)
			}
		}
	}
	if delta.ProposedStop == StopContinue && delta.ProposedAction == nil {
		return fmt.Errorf("%w: continue requires a proposed action", ErrInvalidArgument)
	}
	if delta.ProposedStop != StopContinue && delta.ProposedAction != nil {
		return fmt.Errorf("%w: terminal proposal cannot include an action", ErrInvalidArgument)
	}
	if err := state.Usage.CanCharge(policy.StepUsage, state.Limits); err != nil {
		return err
	}
	return nil
}

// ValidateActionParameters checks both the fixed key allowlist and the scalar
// value contracts exposed to the model. It is shared by the durable reducer
// and task execution so an action cannot pass one boundary and fail another
// for a different reason.
func ValidateActionParameters(raw json.RawMessage, policy ToolActionPolicy) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return fmt.Errorf("%w: action parameters must be a JSON object", ErrInvalidArgument)
	}
	for key := range values {
		if !containsString(policy.ParameterKeys, key) {
			return fmt.Errorf("%w: action parameter %q is not allowlisted", ErrPermission, key)
		}
		if spec, ok := policy.ParameterSpecs[key]; ok {
			if err := validateParameterValue(values[key], spec); err != nil {
				return fmt.Errorf("%w: action parameter %q %v", ErrInvalidArgument, key, err)
			}
		}
	}
	return nil
}

func validateParameterValue(raw json.RawMessage, spec ParameterSpec) error {
	if spec.Type != ParameterString && spec.Type != ParameterInteger && spec.Type != ParameterBoolean {
		return fmt.Errorf("has an unsupported type contract")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("is not a JSON scalar")
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("contains multiple JSON values")
	}
	switch spec.Type {
	case ParameterString:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		if len(spec.Enum) > 0 && !containsString(spec.Enum, text) {
			return fmt.Errorf("is outside the allowed enum")
		}
	case ParameterInteger:
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("must be an integer")
		}
		if _, err := number.Int64(); err != nil {
			return fmt.Errorf("must be an integer")
		}
	case ParameterBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	}
	return nil
}

func validateEvidenceRefs(ids []string, incidentID string, cycle uint64, evidence map[string]EvidenceFact) error {
	for _, id := range ids {
		fact, ok := evidence[id]
		if !ok || fact.IncidentID != incidentID || fact.CycleNo != cycle {
			return fmt.Errorf("%w: evidence reference is outside incident cycle", ErrPermission)
		}
	}
	return nil
}

func boundedID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; !ok {
			values = append(values, value)
			seen[value] = struct{}{}
		}
	}
	return values
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func canonicalJSON(value json.RawMessage) ([]byte, error) {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}

func actionSignature(tool, template, scope string, params []byte) string {
	data := strings.Join([]string{tool, template, scope, string(params)}, "\x00")
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// ActionSignature returns the canonical signature used by the reducer and the
// durable tool-execution task. Exporting the single implementation prevents a
// task payload from being interpreted with a different signing rule.
func ActionSignature(action ProposedAction) (string, error) {
	if len(action.BoundedParameters) == 0 || !json.Valid(action.BoundedParameters) {
		return "", fmt.Errorf("%w: action parameters must be valid JSON", ErrInvalidArgument)
	}
	params, err := canonicalJSON(action.BoundedParameters)
	if err != nil {
		return "", fmt.Errorf("%w: action parameters must be valid JSON", ErrInvalidArgument)
	}
	return actionSignature(action.Tool, action.TemplateID, action.ScopeRef, params), nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

const InvestigationStateSchemaVersion = 1
