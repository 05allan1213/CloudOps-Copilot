package agent

import "context"

// ModelView is the bounded, provider-neutral input for one V3 investigation
// decision. Provider prompts and raw responses never cross this port.
type ModelView struct {
	State    InvestigationState `json:"state"`
	Facts    []EvidenceFact     `json:"facts"`
	ScopeRef string             `json:"scope_ref"`
}

// DiagnosisView is the bounded input for a diagnosis synthesis step.
// Sufficiency is calculated by project code and is not delegated to the model.
type DiagnosisView struct {
	State       InvestigationState `json:"state"`
	Facts       []EvidenceFact     `json:"facts"`
	Sufficiency SufficiencyResult  `json:"sufficiency"`
}

// InvestigationModel is the V3 model boundary. Each call represents exactly
// one durable graph step.
type InvestigationModel interface {
	ProposeDelta(context.Context, ModelView) (StateDelta, ModelUsage, error)
	SynthesizeDiagnosis(context.Context, DiagnosisView) (DiagnosisCandidate, ModelUsage, error)
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
	Confidence      DiagnosisConfidence `json:"confidence"`
	EvidenceFactIDs []string            `json:"evidence_fact_ids"`
	Unknowns        []string            `json:"unknowns,omitempty"`
	RemediationHint RemediationHint     `json:"remediation_hint"`
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

// InvestigationReadTool is the fixed read-only tool boundary used by one
// investigation step. The action has already passed the reducer allowlist.
type InvestigationReadTool interface {
	Execute(context.Context, ProposedAction) (ToolObservation, error)
}
