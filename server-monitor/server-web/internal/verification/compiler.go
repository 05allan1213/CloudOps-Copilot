package verification

import (
	"encoding/json"
	"fmt"
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
		result.Checks = append(result.Checks, CheckSpec{Type: item.typeName, Subject: subject, Expected: expected, Lookback: item.lookback, StabilityWindow: cfg.StabilityWindow, Timeout: cfg.Timeout, PollInterval: cfg.PollInterval, Source: item.source, Required: true})
	}
	return result, nil
}

func ValidatePlan(plan Plan) error {
	if plan.SchemaVersion != 1 || !exactRevisionPattern.MatchString(plan.TargetRevision) || len(plan.Checks) == 0 || len(plan.Checks) > 16 {
		return fmt.Errorf("%w: plan envelope", ErrInvalidArgument)
	}
	seen := map[CheckType]struct{}{}
	for _, check := range plan.Checks {
		switch check.Type {
		case CheckArgoRevision, CheckArgoSync, CheckArgoHealth, CheckDeploymentRollout, CheckWorkloadReady, CheckAlertResolved:
		default:
			return fmt.Errorf("%w: unsupported check type %s", ErrInvalidArgument, check.Type)
		}
		if _, duplicate := seen[check.Type]; duplicate || !check.Required || check.Subject.Revision != plan.TargetRevision || check.PollInterval < time.Second || check.StabilityWindow < check.PollInterval || check.Timeout < check.StabilityWindow || !json.Valid(check.Expected) || len(check.Expected) > 2048 {
			return fmt.Errorf("%w: invalid check %s", ErrInvalidArgument, check.Type)
		}
		seen[check.Type] = struct{}{}
	}
	return nil
}
