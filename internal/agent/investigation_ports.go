package agent

import "context"

// ModelView is the bounded, provider-neutral input for one V3 investigation
// decision. Provider prompts and raw responses never cross this port.
type ModelView struct {
	State            InvestigationState           `json:"state"`
	Facts            []EvidenceFact               `json:"facts"`
	ScopeRef         string                       `json:"scope_ref"`
	AllowedActions   []ModelActionSchema          `json:"allowed_actions"`
	CandidateClaims  []ClaimPolicy                `json:"candidate_claims,omitempty"`
	ClaimSufficiency map[string]SufficiencyResult `json:"claim_sufficiency,omitempty"`
	ActionCandidates []ProposedAction             `json:"action_candidates,omitempty"`
	// ActionCandidatesExhaustive means the caller has supplied the complete
	// bounded action set for this decision. An empty exhaustive set requires a
	// terminal stop; production callers with dynamic tool selection leave it false.
	ActionCandidatesExhaustive bool `json:"action_candidates_exhaustive,omitempty"`
}

// ModelActionSchema is the provider-visible projection of a fixed Go-owned
// action policy. It contains no endpoint, credential, query language, or
// provider-specific authorization data.
type ModelActionSchema struct {
	Tool              string   `json:"tool"`
	TemplateIDs       []string `json:"template_ids"`
	ParameterKeys     []string `json:"parameter_keys"`
	ExpectedFactTypes []string `json:"expected_fact_types"`
}

// DiagnosisView is the bounded input for a diagnosis synthesis step.
// Sufficiency is calculated by project code and is not delegated to the model.
type DiagnosisView struct {
	State                   InvestigationState           `json:"state"`
	Facts                   []EvidenceFact               `json:"facts"`
	Sufficiency             SufficiencyResult            `json:"sufficiency"`
	AllowedClaimTypes       []string                     `json:"allowed_claim_types,omitempty"`
	SufficiencyByClaim      map[string]SufficiencyResult `json:"-"`
	RequiredEvidenceByClaim map[string][]string          `json:"required_evidence_by_claim,omitempty"`
}

// InvestigationModel is the V3 model boundary. Each call represents exactly
// one durable graph step.
type InvestigationModel interface {
	ProposeDelta(context.Context, ModelView) (StateDelta, ModelUsage, error)
	SynthesizeDiagnosis(context.Context, DiagnosisView) (DiagnosisCandidate, ModelUsage, error)
}

// InvestigationModelCallBudget is an optional capability used to reserve the
// maximum provider calls before an invocation. Typed structured models return
// two because one repair is allowed; fixture models default to one.
type InvestigationModelCallBudget interface {
	MaxProviderCallsPerInvocation() int
}

type DiagnosisConfidence string

const (
	DiagnosisConfirmed DiagnosisConfidence = "confirmed"
	DiagnosisLikely    DiagnosisConfidence = "likely"
	DiagnosisUnknown   DiagnosisConfidence = "unknown"
)

type RemediationHint string

const (
	RemediationRestoreRequiredEnv RemediationHint = "restore_required_env"
	RemediationCollectMore        RemediationHint = "collect_more_evidence"
	RemediationNone               RemediationHint = "none"
)

// DiagnosisCandidate is untrusted structured model output. The application
// validates it against current-cycle facts before creating DiagnosisRecord.
type DiagnosisCandidate struct {
	ClaimType       string              `json:"claim_type"`
	Summary         string              `json:"summary"`
	Confidence      DiagnosisConfidence `json:"confidence" jsonschema:"enum=confirmed,enum=likely,enum=unknown"`
	EvidenceFactIDs []string            `json:"evidence_fact_ids"`
	Unknowns        []string            `json:"unknowns,omitempty"`
	RemediationHint RemediationHint     `json:"remediation_hint" jsonschema:"enum=restore_required_env,enum=collect_more_evidence,enum=none"`
}

// DiagnosisRecord is the immutable, policy-bound result persisted by the
// operation. The hash is over every other field in this record.
type DiagnosisRecord struct {
	Candidate          DiagnosisCandidate `json:"candidate"`
	ClaimPolicyVersion string             `json:"claim_policy_version"`
	ClaimPolicyHash    string             `json:"claim_policy_hash"`
	EvidenceIDs        []string           `json:"evidence_ids"`
	DiagnosisHash      string             `json:"diagnosis_hash"`
}

// ToolObservation is the normalized result of one authorized read. Adapters
// provide typed facts and provenance; raw external text is never promoted to
// Evidence by the step operation.
type ToolObservation struct {
	Status          CollectionStatus  `json:"status"`
	SourceSystem    string            `json:"source_system"`
	CollectionPath  string            `json:"collection_path"`
	TemplateVersion string            `json:"template_version"`
	Summary         string            `json:"summary"`
	Facts           []EvidenceFact    `json:"facts"`
	Truncated       bool              `json:"truncated"`
	Provenance      map[string]string `json:"provenance,omitempty"`
	SafeDeepLink    string            `json:"safe_deep_link,omitempty"`
	ContentHash     string            `json:"content_hash"`
}

// InvestigationToolRequest carries the server-resolved Incident scope for one
// approved action. The model sees only Action.ScopeRef; cluster, namespace and
// workload identity are injected by the task-fenced operation after it has
// validated that opaque reference against the current Incident cycle.
type InvestigationToolRequest struct {
	Action           ProposedAction      `json:"action"`
	IncidentPublicID string              `json:"incident_id"`
	CycleNo          uint64              `json:"cycle_no"`
	Correlation      CorrelationSnapshot `json:"correlation"`
	Window           QueryWindow         `json:"window"`
}

// InvestigationReadTool is the fixed read-only tool boundary used by one
// investigation step. The action has already passed the reducer allowlist and
// the request contains only server-owned, current-cycle scope.
type InvestigationReadTool interface {
	Execute(context.Context, InvestigationToolRequest) (ToolObservation, error)
}
