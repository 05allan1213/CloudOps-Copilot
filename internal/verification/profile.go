package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	GoldenRequiredEnvProfileID = "golden-required-env/v1"
	NoChangeProfileID          = "no-change/v1"
	CommonStabilityWindow      = 60 * time.Second
	RunDeadline                = 300 * time.Second
)

var (
	exactRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	imageDigestPattern   = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type CheckTemplate struct {
	Type            CheckType     `json:"type"`
	TemplateID      string        `json:"template_id"`
	TemplateVersion string        `json:"template_version"`
	Comparison      Comparison    `json:"comparison,omitempty"`
	Threshold       float64       `json:"threshold,omitempty"`
	InitialDelay    time.Duration `json:"initial_delay"`
	Lookback        time.Duration `json:"lookback"`
	PollInterval    time.Duration `json:"poll_interval"`
	Timeout         time.Duration `json:"timeout"`
	StabilityWindow time.Duration `json:"stability_window"`
	MinSamples      int           `json:"min_samples"`
	SampleUnit      string        `json:"sample_unit"`
	FailureMode     FailureMode   `json:"failure_mode"`
	SourceIdentity  string        `json:"source_identity"`
	Required        bool          `json:"required"`
}

type ProfileDefinition struct {
	ID       string          `json:"id"`
	Version  int             `json:"version"`
	Deadline time.Duration   `json:"deadline"`
	Checks   []CheckTemplate `json:"checks"`
}

type CompileInput struct {
	TriggerType     string
	Repository      string
	PullRequest     int64
	TargetRevision  string
	SourceRevision  string
	ImageDigest     string
	GitOpsRevision  string
	ArgoApplication string
	ArgoProject     string
	Cluster         string
	Environment     string
	Namespace       string
	Service         string
	WorkloadName    string
	AlertNames      []string
}

func GoldenRequiredEnvProfile() ProfileDefinition {
	return ProfileDefinition{ID: GoldenRequiredEnvProfileID, Version: 1, Deadline: RunDeadline, Checks: []CheckTemplate{
		checkTemplate(CheckArgoExactRevision, 0, 0, 5*time.Second, 180*time.Second, 1, "observation", FailureImmediate, "argocd_read", "", 0),
		checkTemplate(CheckArgoSyncSucceeded, 0, 0, 5*time.Second, 180*time.Second, 1, "observation", FailureImmediate, "argocd_read", "", 0),
		checkTemplate(CheckDeploymentObserved, 0, 0, 5*time.Second, 180*time.Second, 1, "observation", FailureResets, "kubernetes_read", "", 0),
		checkTemplate(CheckDeploymentRolloutComplete, 0, 0, 5*time.Second, 180*time.Second, 1, "observation", FailureImmediate, "kubernetes_read", "", 0),
		checkTemplate(CheckWorkloadReady, 0, 0, 5*time.Second, 240*time.Second, 2, "pods", FailureResets, "kubernetes_read", "", 0),
		checkTemplate(CheckIncidentAlertsResolved, 0, 30*time.Second, 10*time.Second, 240*time.Second, 3, "polls", FailureResets, "incident_signal+prometheus_read", "", 0),
		checkTemplate(CheckMetricErrorRateBelow, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 50, "requests", FailureResets, "prometheus_read", CompareLT, .01),
		checkTemplate(CheckMetricAvailabilityAbove, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 50, "requests", FailureResets, "prometheus_read", CompareGTE, .99),
		checkTemplate(CheckLogRequiredEnvAbsent, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 1, "valid_queries", FailureResets, "elasticsearch_read", CompareAbsent, 0),
		checkTemplate(CheckTraceErrorRateBelow, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 20, "spans", FailureResets, "tempo_read", CompareLT, .01),
	}}
}

func NoChangeProfile() ProfileDefinition {
	return ProfileDefinition{ID: NoChangeProfileID, Version: 1, Deadline: RunDeadline, Checks: []CheckTemplate{
		checkTemplate(CheckDeploymentIdentity, 0, 0, 5*time.Second, 240*time.Second, 1, "observation", FailureImmediate, "argocd+kubernetes+registry_read", "", 0),
		checkTemplate(CheckDeploymentRolloutComplete, 0, 0, 5*time.Second, 180*time.Second, 1, "observation", FailureImmediate, "kubernetes_read", "", 0),
		checkTemplate(CheckWorkloadReady, 0, 0, 5*time.Second, 240*time.Second, 2, "pods", FailureResets, "kubernetes_read", "", 0),
		checkTemplate(CheckIncidentAlertsResolved, 0, 30*time.Second, 10*time.Second, 240*time.Second, 3, "polls", FailureResets, "incident_signal+prometheus_read", "", 0),
		checkTemplate(CheckMetricErrorRateBelow, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 50, "requests", FailureResets, "prometheus_read", CompareLT, .01),
		checkTemplate(CheckMetricAvailabilityAbove, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 50, "requests", FailureResets, "prometheus_read", CompareGTE, .99),
		checkTemplate(CheckLogRequiredEnvAbsent, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 1, "valid_queries", FailureResets, "elasticsearch_read", CompareAbsent, 0),
		checkTemplate(CheckTraceErrorRateBelow, 30*time.Second, 30*time.Second, 10*time.Second, 300*time.Second, 20, "spans", FailureResets, "tempo_read", CompareLT, .01),
	}}
}

func checkTemplate(checkType CheckType, initial, lookback, poll, timeout time.Duration, minSamples int, unit string, mode FailureMode, source string, comparison Comparison, threshold float64) CheckTemplate {
	return CheckTemplate{Type: checkType, TemplateID: string(checkType) + "/v1", TemplateVersion: "v1", Comparison: comparison, Threshold: threshold, InitialDelay: initial, Lookback: lookback, PollInterval: poll, Timeout: timeout, StabilityWindow: CommonStabilityWindow, MinSamples: minSamples, SampleUnit: unit, FailureMode: mode, SourceIdentity: source, Required: true}
}

func CompilePlan(input CompileInput) (Plan, error) {
	if err := validateCompileInput(input); err != nil {
		return Plan{}, err
	}
	profile := GoldenRequiredEnvProfile()
	if input.TriggerType == "no_change" {
		profile = NoChangeProfile()
	}
	profileHash, err := ProfileHash(profile)
	if err != nil {
		return Plan{}, err
	}
	subject := Subject{
		Repository: input.Repository, PullRequest: input.PullRequest, Revision: strings.ToLower(input.TargetRevision),
		ArgoApplication: input.ArgoApplication, ArgoProject: input.ArgoProject, Cluster: input.Cluster,
		Environment: input.Environment, Namespace: input.Namespace, Service: input.Service,
		WorkloadKind: "Deployment", WorkloadName: input.WorkloadName,
	}
	alertNames := append([]string(nil), input.AlertNames...)
	sort.Strings(alertNames)
	plan := Plan{
		SchemaVersion: 1, TargetRevision: subject.Revision, ProfileID: profile.ID, ProfileVersion: profile.Version,
		ProfileHash: profileHash, TriggerType: input.TriggerType, SourceRevision: strings.ToLower(input.SourceRevision),
		ImageDigest: strings.ToLower(input.ImageDigest), GitOpsRevision: strings.ToLower(input.GitOpsRevision), Deadline: profile.Deadline,
		Checks: make([]CheckSpec, 0, len(profile.Checks)),
	}
	for _, template := range profile.Checks {
		expected, err := expectedForCheck(template.Type, input, alertNames)
		if err != nil {
			return Plan{}, err
		}
		plan.Checks = append(plan.Checks, CheckSpec{
			Type: template.Type, Subject: subject, Expected: expected, Lookback: template.Lookback,
			StabilityWindow: template.StabilityWindow, Timeout: template.Timeout, PollInterval: template.PollInterval,
			Source: template.SourceIdentity, SourceIdentity: template.SourceIdentity, ProfileID: profile.ID,
			TemplateID: template.TemplateID, TemplateVersion: template.TemplateVersion, Comparison: template.Comparison,
			Threshold: template.Threshold, Required: template.Required, InitialDelay: template.InitialDelay,
			MinSamples: template.MinSamples, SampleUnit: template.SampleUnit, FailureMode: template.FailureMode,
		})
	}
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func ProfileHash(profile ProfileDefinition) (string, error) {
	if err := validateProfile(profile); err != nil {
		return "", err
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func ValidatePlan(plan Plan) error {
	if plan.SchemaVersion != 1 || plan.ProfileVersion != 1 || len(plan.ProfileHash) != 64 || plan.Deadline != RunDeadline || !exactRevisionPattern.MatchString(plan.TargetRevision) || !exactRevisionPattern.MatchString(plan.SourceRevision) || !imageDigestPattern.MatchString(plan.ImageDigest) || !exactRevisionPattern.MatchString(plan.GitOpsRevision) {
		return fmt.Errorf("%w: verification plan envelope", ErrInvalidArgument)
	}
	if plan.TriggerType != "post_delivery" && plan.TriggerType != "no_change" {
		return fmt.Errorf("%w: verification trigger type", ErrInvalidArgument)
	}
	expectedProfile := GoldenRequiredEnvProfile()
	if plan.TriggerType == "no_change" {
		expectedProfile = NoChangeProfile()
	}
	hash, _ := ProfileHash(expectedProfile)
	if plan.ProfileID != expectedProfile.ID || plan.ProfileHash != hash || len(plan.Checks) != len(expectedProfile.Checks) {
		return fmt.Errorf("%w: verification profile binding", ErrInvalidArgument)
	}
	seen := make(map[CheckType]struct{}, len(plan.Checks))
	for index, check := range plan.Checks {
		template := expectedProfile.Checks[index]
		if check.Type != template.Type || check.ProfileID != plan.ProfileID || check.TemplateID != template.TemplateID || check.TemplateVersion != template.TemplateVersion || !check.Required || check.StabilityWindow != CommonStabilityWindow || check.PollInterval != template.PollInterval || check.Timeout != template.Timeout || check.InitialDelay != template.InitialDelay || check.Lookback != template.Lookback || check.MinSamples != template.MinSamples || check.SampleUnit != template.SampleUnit || check.FailureMode != template.FailureMode || check.SourceIdentity != template.SourceIdentity || strings.Contains(strings.ToLower(check.SourceIdentity), "loki") || !json.Valid(check.Expected) {
			return fmt.Errorf("%w: verification check contract %s", ErrInvalidArgument, check.Type)
		}
		if _, duplicate := seen[check.Type]; duplicate {
			return fmt.Errorf("%w: duplicate verification check", ErrInvalidArgument)
		}
		seen[check.Type] = struct{}{}
	}
	return nil
}

func validateProfile(profile ProfileDefinition) error {
	if (profile.ID != GoldenRequiredEnvProfileID && profile.ID != NoChangeProfileID) || profile.Version != 1 || profile.Deadline != RunDeadline || len(profile.Checks) == 0 {
		return fmt.Errorf("%w: verification profile", ErrInvalidArgument)
	}
	seen := map[CheckType]struct{}{}
	for _, check := range profile.Checks {
		if check.Type == "" || check.TemplateID == "" || check.TemplateVersion != "v1" || check.PollInterval <= 0 || check.Timeout <= 0 || check.StabilityWindow != CommonStabilityWindow || check.MinSamples <= 0 || check.SampleUnit == "" || (check.FailureMode != FailureResets && check.FailureMode != FailureImmediate) || check.SourceIdentity == "" || !check.Required {
			return fmt.Errorf("%w: verification profile check", ErrInvalidArgument)
		}
		if _, ok := seen[check.Type]; ok {
			return fmt.Errorf("%w: duplicate verification profile check", ErrInvalidArgument)
		}
		seen[check.Type] = struct{}{}
	}
	return nil
}

func validateCompileInput(input CompileInput) error {
	if input.TriggerType != "post_delivery" && input.TriggerType != "no_change" {
		return fmt.Errorf("%w: verification trigger", ErrInvalidArgument)
	}
	if !exactRevisionPattern.MatchString(strings.ToLower(input.TargetRevision)) || !exactRevisionPattern.MatchString(strings.ToLower(input.SourceRevision)) || !imageDigestPattern.MatchString(strings.ToLower(input.ImageDigest)) || !exactRevisionPattern.MatchString(strings.ToLower(input.GitOpsRevision)) || input.ArgoApplication == "" || input.ArgoProject == "" || input.Cluster == "" || input.Environment == "" || input.Namespace == "" || input.Service == "" || input.WorkloadName == "" || len(input.AlertNames) == 0 || len(input.AlertNames) > 20 {
		return fmt.Errorf("%w: verification identity", ErrInvalidArgument)
	}
	if input.TriggerType == "post_delivery" && (input.Repository == "" || input.PullRequest <= 0) {
		return fmt.Errorf("%w: post-delivery GitHub identity", ErrInvalidArgument)
	}
	seen := map[string]struct{}{}
	for _, name := range input.AlertNames {
		if strings.TrimSpace(name) == "" || len(name) > 128 {
			return fmt.Errorf("%w: alert name", ErrInvalidArgument)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("%w: duplicate alert name", ErrInvalidArgument)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func expectedForCheck(checkType CheckType, input CompileInput, alertNames []string) (json.RawMessage, error) {
	var expected any
	switch checkType {
	case CheckArgoExactRevision:
		expected = map[string]any{"sync_revision": strings.ToLower(input.TargetRevision), "sync_result_revision": strings.ToLower(input.TargetRevision)}
	case CheckArgoSyncSucceeded:
		expected = map[string]any{"revision": strings.ToLower(input.TargetRevision), "operation_phase": "Succeeded"}
	case CheckDeploymentIdentity:
		expected = map[string]any{"source_revision": strings.ToLower(input.SourceRevision), "image_digest": strings.ToLower(input.ImageDigest), "gitops_revision": strings.ToLower(input.GitOpsRevision)}
	case CheckDeploymentObserved:
		expected = map[string]any{"observed_generation_equals_generation": true}
	case CheckDeploymentRolloutComplete:
		expected = map[string]any{"desired": 2, "updated": 2, "ready": 2, "available": 2, "unavailable": 0}
	case CheckWorkloadReady:
		expected = map[string]any{"ready_pods": 2, "readyz": true}
	case CheckIncidentAlertsResolved:
		expected = map[string]any{"alert_names": alertNames, "all_cycle_instances_resolved": true, "prometheus_firing_series": 0}
	case CheckMetricErrorRateBelow:
		expected = map[string]any{"comparison": CompareLT, "threshold": .01, "min_requests": 50}
	case CheckMetricAvailabilityAbove:
		expected = map[string]any{"comparison": CompareGTE, "threshold": .99, "min_requests": 50}
	case CheckLogRequiredEnvAbsent:
		expected = map[string]any{"comparison": CompareAbsent, "structured_error": "required_env_missing", "max_count": 0}
	case CheckTraceErrorRateBelow:
		expected = map[string]any{"comparison": CompareLT, "threshold": .01, "min_spans": 20}
	default:
		return nil, fmt.Errorf("%w: unsupported verification check", ErrInvalidArgument)
	}
	return json.Marshal(expected)
}
