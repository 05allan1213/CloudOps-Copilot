package agenteval

import (
	"slices"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/investigationread"
)

// EvaluationActionPolicies keeps the eight production tool names and template
// IDs while adding fixture-only fact types required by the frozen multi-cause
// dataset. Live provider claims remain owned by GoldenActionPolicies.
func EvaluationActionPolicies() map[string]agent.ToolActionPolicy {
	policies := investigationread.GoldenActionPolicies()
	appendFacts := func(tool string, facts ...string) {
		policy := policies[tool]
		policy.ExpectedFactTypes = stableStrings(append(policy.ExpectedFactTypes, facts...))
		policies[tool] = policy
	}
	appendFacts(investigationread.ToolInspectWorkload,
		"kubernetes.crash_loop_backoff", "kubernetes.oom_killed", "kubernetes.readiness_failed",
		"kubernetes.service_selector_mismatch", "kubernetes.endpoints_missing")
	appendFacts(investigationread.ToolInspectKubernetesEvents,
		"event.backoff_present", "event.oom_killed_present", "event.readiness_failed_present",
		"event.prompt_injection")
	appendFacts(investigationread.ToolQueryMetrics,
		"metric.memory_limit_pressure", "metric.application_error_rate", "metric.no_data")
	appendFacts(investigationread.ToolQueryLogs,
		"log.container_crash", "log.oom_killed", "log.readiness_failure", "log.application_error",
		"log.prompt_injection", "log.secret_canary")
	appendFacts(investigationread.ToolQueryTraces,
		"trace.application_error", "trace.no_data")
	appendFacts(investigationread.ToolGetDeploymentContext,
		"deployment.revision_conflict", "deployment.mutable_tag", "deployment.recent_change_irrelevant",
		"deployment.prompt_injection")
	appendFacts(investigationread.ToolGetChangeDetail,
		"change.selector_mismatch", "change.readiness_regression", "change.application_error_regression",
		"change.wrong_recent_commit", "change.prompt_injection")
	appendFacts(investigationread.ToolSearchRunbooks,
		"runbook.prompt_injection", "runbook.secret_canary")
	return policies
}

func EvaluationContracts(datasetID string) Contracts {
	policies := EvaluationActionPolicies()
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	slices.Sort(names)
	actions := make([]agent.ModelActionSchema, 0, len(names))
	for _, name := range names {
		policy := policies[name]
		actions = append(actions, agent.ModelActionSchema{
			Tool: name, TemplateIDs: stableStrings(policy.TemplateIDs),
			ParameterKeys: stableStrings(policy.ParameterKeys), ParameterSpecs: stableParameterSpecs(policy.ParameterSpecs),
			ExpectedFactTypes: stableStrings(policy.ExpectedFactTypes),
		})
	}
	return Contracts{SchemaVersion: DatasetSchemaVersion, DatasetID: datasetID, Actions: actions}
}

func stableStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func stableParameterSpecs(values map[string]agent.ParameterSpec) map[string]agent.ParameterSpec {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]agent.ParameterSpec, len(values))
	for key, spec := range values {
		spec.Enum = stableStrings(spec.Enum)
		result[key] = spec
	}
	return result
}
