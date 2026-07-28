// Package agenteval contains the frozen, provider-independent Agent Quality
// harness. It deliberately keeps raw provider responses out of every result.
package agenteval

import (
	"fmt"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

const (
	DatasetSchemaVersion        = 1
	OracleSchemaVersion         = 1
	SplitSchemaVersion          = 1
	MetricSchemaVersion         = 1
	LegacyManifestSchemaVersion = 1
	ManifestSchemaVersion       = 2

	OutcomeDiagnosed    = "diagnosed"
	OutcomeInsufficient = "insufficient_evidence"
	ModeModel           = "model"
	ModeGuardrail       = "guardrail"
	SplitCalibration    = "calibration"
	SplitQuality        = "quality"
	SplitModel          = "model"
)

// Dataset is the immutable, content-addressed input to Agent Eval. Files are
// kept separate from the oracle and split so a score cannot silently change
// when expectations are edited.
type Dataset struct {
	SchemaVersion int        `json:"schema_version"`
	DatasetID     string     `json:"dataset_id"`
	Cases         []EvalCase `json:"cases"`
}

type EvalCase struct {
	ID               string                    `json:"id"`
	Mode             string                    `json:"mode"`
	Category         string                    `json:"category"`
	Description      string                    `json:"description"`
	ScopeRef         string                    `json:"scope_ref"`
	Objective        string                    `json:"objective"`
	Correlation      agent.CorrelationSnapshot `json:"correlation"`
	Window           agent.QueryWindow         `json:"window"`
	Policies         []agent.ClaimPolicy       `json:"policies"`
	Limits           agent.Limits              `json:"limits"`
	RequiredSources  []string                  `json:"required_sources,omitempty"`
	Fixtures         []ToolFixture             `json:"fixtures"`
	InitialFacts     []agent.EvidenceFact      `json:"initial_facts,omitempty"`
	SafetyMarkers    []string                  `json:"safety_markers,omitempty"`
	ReplayAfterTools int                       `json:"replay_after_tools,omitempty"`
}

type ToolFixture struct {
	Actions     []agent.ProposedAction `json:"actions"`
	Observation agent.ToolObservation  `json:"observation"`
}

// Oracle is deterministic and contains no model-generated text matching.
// Evidence requirements are expressed as fact-type groups: one cited fact
// from each group is required for full citation recall.
type Oracle struct {
	SchemaVersion int                   `json:"schema_version"`
	DatasetID     string                `json:"dataset_id"`
	Cases         map[string]CaseOracle `json:"cases"`
}

type CaseOracle struct {
	ExpectedOutcome        string     `json:"expected_outcome"`
	AcceptableClaimTypes   []string   `json:"acceptable_claim_types,omitempty"`
	RequiredEvidenceGroups [][]string `json:"required_evidence_groups,omitempty"`
	MaxToolCalls           int        `json:"max_tool_calls"`
	BaselineMaxToolCalls   int        `json:"baseline_max_tool_calls,omitempty"`
	ForbiddenTools         []string   `json:"forbidden_tools,omitempty"`
	SafetyMarkers          []string   `json:"safety_markers,omitempty"`
	RequireReplay          bool       `json:"require_replay,omitempty"`
	Notes                  string     `json:"notes,omitempty"`
}

type Split struct {
	SchemaVersion int      `json:"schema_version"`
	DatasetID     string   `json:"dataset_id"`
	Calibration   []string `json:"calibration"`
	Quality       []string `json:"quality"`
	Guardrail     []string `json:"guardrail"`
	Repetitions   int      `json:"repetitions"`
	Aggregation   string   `json:"aggregation"`
}

type MetricSpec struct {
	SchemaVersion int                `json:"schema_version"`
	DatasetID     string             `json:"dataset_id"`
	Metrics       []MetricDefinition `json:"metrics"`
	Safety        []string           `json:"safety_zero_gate"`
}

type MetricDefinition struct {
	Name        string `json:"name"`
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
	Direction   string `json:"direction"`
}

type Manifest struct {
	SchemaVersion        int    `json:"schema_version"`
	DatasetID            string `json:"dataset_id"`
	DatasetSHA256        string `json:"dataset_sha256"`
	OracleSHA256         string `json:"oracle_sha256"`
	SplitSHA256          string `json:"split_sha256"`
	MetricSpecSHA256     string `json:"metric_spec_sha256"`
	ContractsSHA256      string `json:"contracts_sha256"`
	PromptMaterialSHA256 string `json:"prompt_material_sha256"`
	ReducerSourceSHA256  string `json:"reducer_source_sha256"`
	RunnerSourceSHA256   string `json:"runner_source_sha256"`
	RuntimeSourceSHA256  string `json:"runtime_source_sha256,omitempty"`
	CreatedAt            string `json:"created_at"`
}

func (m Manifest) Validate() error {
	if (m.SchemaVersion != LegacyManifestSchemaVersion && m.SchemaVersion != ManifestSchemaVersion) || m.DatasetID == "" || m.CreatedAt == "" {
		return fmt.Errorf("invalid Agent Eval manifest identity")
	}
	for name, value := range map[string]string{
		"dataset": m.DatasetSHA256, "oracle": m.OracleSHA256, "split": m.SplitSHA256,
		"metric_spec": m.MetricSpecSHA256, "contracts": m.ContractsSHA256, "prompt": m.PromptMaterialSHA256,
		"reducer": m.ReducerSourceSHA256, "runner": m.RunnerSourceSHA256,
	} {
		if len(value) != 64 {
			return fmt.Errorf("invalid Agent Eval %s hash", name)
		}
	}
	if m.SchemaVersion == ManifestSchemaVersion && len(m.RuntimeSourceSHA256) != 64 {
		return fmt.Errorf("invalid Agent Eval runtime source hash")
	}
	if m.SchemaVersion == LegacyManifestSchemaVersion && m.RuntimeSourceSHA256 != "" {
		return fmt.Errorf("legacy Agent Eval manifest contains a runtime source hash")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.CreatedAt); err != nil {
		return fmt.Errorf("invalid Agent Eval manifest time: %w", err)
	}
	return nil
}

type Contracts struct {
	SchemaVersion int                       `json:"schema_version"`
	DatasetID     string                    `json:"dataset_id"`
	Actions       []agent.ModelActionSchema `json:"actions"`
}

type RunResult struct {
	CaseID       string       `json:"case_id"`
	Repetition   int          `json:"repetition"`
	Outcome      string       `json:"outcome"`
	ClaimType    string       `json:"claim_type,omitempty"`
	Confidence   string       `json:"confidence,omitempty"`
	CitedFactIDs []string     `json:"cited_fact_ids,omitempty"`
	ToolCalls    int          `json:"tool_calls"`
	ModelCalls   int          `json:"model_calls"`
	InputTokens  int64        `json:"input_tokens"`
	OutputTokens int64        `json:"output_tokens"`
	ReplayOK     bool         `json:"replay_ok"`
	Safety       SafetyCounts `json:"safety"`
	ErrorCode    string       `json:"error_code,omitempty"`
	ErrorSummary string       `json:"error_summary,omitempty"`
	Trace        []TraceEvent `json:"trace,omitempty"`
}

type TraceEvent struct {
	Kind       string   `json:"kind"`
	Tool       string   `json:"tool,omitempty"`
	Signature  string   `json:"signature,omitempty"`
	FactIDs    []string `json:"fact_ids,omitempty"`
	Checkpoint string   `json:"checkpoint,omitempty"`
}

type SafetyCounts struct {
	WriteTool            int `json:"write_tool"`
	ScopeEscape          int `json:"scope_escape"`
	SecretLeak           int `json:"secret_leak"`
	PromptInjection      int `json:"prompt_injection"`
	ForeignEvidence      int `json:"foreign_evidence"`
	UnsupportedConfirmed int `json:"unsupported_confirmed"`
	InvalidSignature     int `json:"invalid_signature"`
	BudgetOverrun        int `json:"budget_overrun"`
}

func (s SafetyCounts) Total() int {
	return s.WriteTool + s.ScopeEscape + s.SecretLeak + s.PromptInjection +
		s.ForeignEvidence + s.UnsupportedConfirmed + s.InvalidSignature + s.BudgetOverrun
}

type Aggregate struct {
	Runs                  int          `json:"runs"`
	Cases                 int          `json:"cases"`
	ExpectedDiagnosed     int          `json:"expected_diagnosed"`
	ExpectedInsufficient  int          `json:"expected_insufficient"`
	PredictedDiagnosed    int          `json:"predicted_diagnosed"`
	PredictedInsufficient int          `json:"predicted_insufficient"`
	RootCauseCorrect      int          `json:"root_cause_correct"`
	RootCauseAccuracy     float64      `json:"root_cause_accuracy"`
	InsufficientTP        int          `json:"insufficient_true_positive"`
	InsufficientFP        int          `json:"insufficient_false_positive"`
	InsufficientFN        int          `json:"insufficient_false_negative"`
	InsufficientPrecision float64      `json:"insufficient_precision"`
	InsufficientRecall    float64      `json:"insufficient_recall"`
	CitationGroups        int          `json:"citation_groups"`
	CitationGroupsCovered int          `json:"citation_groups_covered"`
	CitationRecall        float64      `json:"citation_recall"`
	AverageToolCalls      float64      `json:"average_tool_calls"`
	MaxToolCalls          int          `json:"max_tool_calls"`
	ReplayPassRate        float64      `json:"replay_pass_rate"`
	Safety                SafetyCounts `json:"safety"`
	Failures              int          `json:"failures"`
}

type Report struct {
	SchemaVersion int         `json:"schema_version"`
	DatasetID     string      `json:"dataset_id"`
	Manifest      Manifest    `json:"manifest"`
	Provider      string      `json:"provider,omitempty"`
	Model         string      `json:"model,omitempty"`
	Repetitions   int         `json:"repetitions"`
	Aggregation   string      `json:"aggregation"`
	Status        string      `json:"status"`
	Aggregate     Aggregate   `json:"aggregate"`
	Runs          []RunResult `json:"runs"`
	GeneratedAt   time.Time   `json:"generated_at"`
	Note          string      `json:"note,omitempty"`
}

type QualityThresholds struct {
	SchemaVersion               int     `json:"schema_version"`
	DatasetID                   string  `json:"dataset_id"`
	Aggregation                 string  `json:"aggregation"`
	MinRootCauseAccuracy        float64 `json:"min_root_cause_accuracy"`
	MinInsufficientPrecision    float64 `json:"min_insufficient_precision"`
	MinInsufficientRecall       float64 `json:"min_insufficient_recall"`
	MinCitationRecall           float64 `json:"min_citation_recall"`
	MaxAverageToolCalls         float64 `json:"max_average_tool_calls"`
	MaxToolCalls                int     `json:"max_tool_calls"`
	MinReplayPassRate           float64 `json:"min_replay_pass_rate"`
	RequireZeroSafetyViolations bool    `json:"require_zero_safety_violations"`
	RequireStrictBaselineWin    bool    `json:"require_strict_baseline_win"`
}

type GateResult struct {
	Status   string   `json:"status"`
	Failures []string `json:"failures,omitempty"`
}

type RunOptions struct {
	Repetitions int
	OnlySplit   string
	CaseIDs     []string
	Timeout     time.Duration
	Provider    string
	Model       string
}

type GuardrailCaseResult struct {
	CaseID  string `json:"case_id"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type GuardrailReport struct {
	SchemaVersion int                   `json:"schema_version"`
	DatasetID     string                `json:"dataset_id"`
	Manifest      Manifest              `json:"manifest"`
	Status        string                `json:"status"`
	Cases         []GuardrailCaseResult `json:"cases"`
	GeneratedAt   time.Time             `json:"generated_at"`
}
