package verification

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

var exactRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

type CompilerConfig struct {
	PollInterval    time.Duration
	Timeout         time.Duration
	StabilityWindow time.Duration
	AlertLookback   time.Duration
}

func CompileTrustedPlan(subject Subject, cfg CompilerConfig) (Plan, error) {
	return compileTrustedPlan(subject, cfg, nil)
}

func CompileTrustedPlanWithProfile(subject Subject, cfg CompilerConfig, profile *Profile) (Plan, error) {
	return compileTrustedPlan(subject, cfg, profile)
}

// CompileControlledDirectPlan omits GitHub and Argo checks for the explicitly
// flagged disposable demo fallback. Kubernetes rollout and resolved Signal
// remain required, deterministic, and persisted.
func CompileControlledDirectPlan(subject Subject, cfg CompilerConfig) (Plan, error) {
	if !exactRevisionPattern.MatchString(strings.ToLower(subject.Revision)) || subject.Repository == "" || subject.Cluster == "" || subject.Environment == "" || subject.Namespace == "" || subject.WorkloadKind != "Deployment" || subject.WorkloadName == "" || subject.AlertFingerprint == "" {
		return Plan{}, fmt.Errorf("%w: incomplete controlled-direct subject", ErrInvalidArgument)
	}
	if cfg.PollInterval < time.Second || cfg.PollInterval > time.Minute || cfg.Timeout < time.Minute || cfg.Timeout > 24*time.Hour || cfg.StabilityWindow < cfg.PollInterval || cfg.StabilityWindow > cfg.Timeout || cfg.AlertLookback < time.Minute || cfg.AlertLookback > 24*time.Hour {
		return Plan{}, fmt.Errorf("%w: compiler bounds", ErrInvalidArgument)
	}
	checks := []struct {
		typeName CheckType
		expected any
		lookback time.Duration
	}{
		{CheckDeploymentRollout, map[string]any{"observed_generation_caught_up": true, "updated_equals_desired": true, "unavailable_max": 0}, 0},
		{CheckWorkloadReady, map[string]any{"available_equals_desired": true, "progressing": true, "available": true}, 0},
		{CheckAlertResolved, map[string]any{"status": "resolved", "fingerprint": subject.AlertFingerprint}, cfg.AlertLookback},
	}
	plan := Plan{SchemaVersion: 1, TargetRevision: strings.ToLower(subject.Revision)}
	for _, item := range checks {
		expected, _ := json.Marshal(item.expected)
		plan.Checks = append(plan.Checks, CheckSpec{Type: item.typeName, Subject: subject, Expected: expected, Lookback: item.lookback, StabilityWindow: cfg.StabilityWindow, Timeout: cfg.Timeout, PollInterval: cfg.PollInterval, Source: "controlled_direct", SourceIdentity: "controlled_direct", Required: true})
	}
	return plan, ValidatePlan(plan)
}

func compileTrustedPlan(subject Subject, cfg CompilerConfig, profile *Profile) (Plan, error) {
	if !exactRevisionPattern.MatchString(strings.ToLower(subject.Revision)) || subject.Repository == "" || subject.PullRequest <= 0 || subject.ArgoApplication == "" || subject.ArgoProject == "" || subject.Cluster == "" || subject.Environment == "" || subject.Namespace == "" || subject.WorkloadKind != "Deployment" || subject.WorkloadName == "" || subject.AlertFingerprint == "" {
		return Plan{}, fmt.Errorf("%w: incomplete trusted subject", ErrInvalidArgument)
	}
	if cfg.PollInterval < time.Second || cfg.PollInterval > time.Minute || cfg.Timeout < time.Minute || cfg.Timeout > 24*time.Hour || cfg.StabilityWindow < cfg.PollInterval || cfg.StabilityWindow > cfg.Timeout || cfg.AlertLookback < time.Minute || cfg.AlertLookback > 24*time.Hour {
		return Plan{}, fmt.Errorf("%w: compiler bounds", ErrInvalidArgument)
	}
	checks := []struct {
		typeName CheckType
		source   string
		expected any
		lookback time.Duration
	}{
		{CheckArgoRevision, "argocd_read", map[string]any{"revision": strings.ToLower(subject.Revision)}, 0},
		{CheckArgoSync, "argocd_read", map[string]any{"sync_status": "Synced", "operation_phase": "Succeeded"}, 0},
		{CheckArgoHealth, "argocd_read", map[string]any{"application_health": "Healthy", "resource_health": "Healthy"}, 0},
		{CheckDeploymentRollout, "kubernetes_read", map[string]any{"observed_generation_caught_up": true, "updated_equals_desired": true, "unavailable_max": 0}, 0},
		{CheckWorkloadReady, "kubernetes_read", map[string]any{"available_equals_desired": true, "progressing": true, "available": true}, 0},
		{CheckAlertResolved, "incident_signal", map[string]any{"status": "resolved", "fingerprint": subject.AlertFingerprint}, cfg.AlertLookback},
	}
	result := Plan{SchemaVersion: 1, TargetRevision: strings.ToLower(subject.Revision), Checks: make([]CheckSpec, 0, len(checks))}
	for _, item := range checks {
		expected, err := json.Marshal(item.expected)
		if err != nil || len(expected) > 2048 {
			return Plan{}, fmt.Errorf("%w: expected condition", ErrInvalidArgument)
		}
		result.Checks = append(result.Checks, CheckSpec{Type: item.typeName, Subject: subject, Expected: expected, Lookback: item.lookback, StabilityWindow: cfg.StabilityWindow, Timeout: cfg.Timeout, PollInterval: cfg.PollInterval, Source: item.source, SourceIdentity: item.source, Required: true})
	}
	if profile != nil {
		if subject.Service == "" || !strings.EqualFold(profile.Service, subject.Service) || !strings.EqualFold(profile.Environment, subject.Environment) || profile.Namespace != subject.Namespace || profile.Workload != subject.WorkloadName {
			return Plan{}, fmt.Errorf("%w: profile mismatch", ErrNotAllowed)
		}
		result.SchemaVersion = 2
		for _, template := range profile.Templates {
			if !supportedObservabilityType(template.Type) || math.IsNaN(template.Threshold) || math.IsInf(template.Threshold, 0) {
				return Plan{}, fmt.Errorf("%w: observability template", ErrInvalidArgument)
			}
			expected, _ := json.Marshal(map[string]any{"comparison": template.Comparison, "threshold": template.Threshold, "template_id": template.ID, "profile_id": profile.ID})
			source := sourceForCheck(template.Type)
			result.Checks = append(result.Checks, CheckSpec{Type: template.Type, Subject: subject, Expected: expected, Lookback: template.Lookback, StabilityWindow: template.Stability, Timeout: template.Timeout, PollInterval: cfg.PollInterval, Source: source, SourceIdentity: source, ProfileID: profile.ID, TemplateID: template.ID, Comparison: template.Comparison, Threshold: template.Threshold, Required: template.Required})
		}
	}
	return result, nil
}

func ValidatePlan(plan Plan) error {
	if (plan.SchemaVersion != 1 && plan.SchemaVersion != 2) || !exactRevisionPattern.MatchString(plan.TargetRevision) || len(plan.Checks) == 0 || len(plan.Checks) > 16 {
		return fmt.Errorf("%w: plan envelope", ErrInvalidArgument)
	}
	seen := map[CheckType]struct{}{}
	for _, check := range plan.Checks {
		switch check.Type {
		case CheckArgoRevision, CheckArgoSync, CheckArgoHealth, CheckDeploymentRollout, CheckWorkloadReady, CheckAlertResolved:
		case CheckMetricErrorRateBelow, CheckMetricAvailabilityAbove, CheckMetricLatencyP95Below, CheckLogErrorAbsent, CheckLogErrorRateBelow, CheckTraceErrorRateBelow, CheckTraceLatencyP95Below:
			if plan.SchemaVersion != 2 || check.TemplateID == "" || check.ProfileID == "" || !comparisonAllowed(check.Type, check.Comparison) || math.IsNaN(check.Threshold) || math.IsInf(check.Threshold, 0) || check.Lookback < time.Minute || check.Lookback > 24*time.Hour {
				return fmt.Errorf("%w: invalid observability check", ErrInvalidArgument)
			}
		default:
			return fmt.Errorf("%w: unsupported check type %s", ErrInvalidArgument, check.Type)
		}
		if _, duplicate := seen[check.Type]; duplicate || check.Subject.Revision != plan.TargetRevision || check.PollInterval < time.Second || check.StabilityWindow < check.PollInterval || check.Timeout < check.StabilityWindow || !json.Valid(check.Expected) || len(check.Expected) > 2048 || check.SourceIdentity == "" {
			return fmt.Errorf("%w: invalid check %s", ErrInvalidArgument, check.Type)
		}
		if !supportedObservabilityType(check.Type) && !check.Required {
			return fmt.Errorf("%w: base check must be required", ErrInvalidArgument)
		}
		seen[check.Type] = struct{}{}
	}
	return nil
}

func sourceForCheck(t CheckType) string {
	switch t {
	case CheckMetricErrorRateBelow, CheckMetricAvailabilityAbove, CheckMetricLatencyP95Below:
		return "prometheus_read"
	case CheckLogErrorAbsent, CheckLogErrorRateBelow:
		return "loki_read"
	case CheckTraceErrorRateBelow, CheckTraceLatencyP95Below:
		return "tempo_read"
	default:
		return ""
	}
}
