package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxDiagnosisSummaryBytes = 4096
	maxDiagnosisItems        = 20
)

var (
	prohibitedInstruction = regexp.MustCompile(`(?i)(\bkubectl\s+(apply|patch|delete|scale)\b|\brestart\s+deployment\b|\bscale\s+deployment\b|\bcreate\s+(a\s+)?pull\s+request\b|\b(argocd|argo\s+cd)\s+(sync|rollback|app\s+set|app\s+patch)\b|\bexecute\s+shell\b|直接(重启|扩容|缩容|修改|执行|创建拉取请求|同步))`)
	changeClaimPattern    = regexp.MustCompile(`(?i)(deploy|release|revision|commit|pull request|\bpr\b|image|发布|版本|提交|镜像|变更)`)
	verifiedRevision      = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	verifiedDigest        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// ValidateDiagnosis deterministically validates a model diagnosis against valid Evidence ownership.
func ValidateDiagnosis(d Diagnosis, evidence map[string]EvidenceRecord) []string {
	var problems []string
	if strings.TrimSpace(d.Summary) == "" || len(d.Summary) > maxDiagnosisSummaryBytes {
		problems = append(problems, "summary is required and must be bounded")
	}
	if d.Confidence < 0 || d.Confidence > 1 {
		problems = append(problems, "confidence must be between 0 and 1")
	}
	if len(d.Hypotheses) > maxDiagnosisItems || len(d.ConfirmedFacts) > maxDiagnosisItems || len(d.Unknowns) > maxDiagnosisItems || len(d.RecommendedNextActions) > maxDiagnosisItems {
		problems = append(problems, "diagnosis collection exceeds item limit")
	}
	validateIDs := func(path, statement string, ids []string, strong bool) {
		if strong && len(ids) == 0 {
			problems = append(problems, path+" requires evidence")
		}
		validSupport := false
		confirmedChangeSupport := false
		for _, id := range ids {
			item, ok := evidence[id]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s references unknown evidence %q", path, id))
				continue
			}
			if item.Valid && !item.Truncated {
				validSupport = true
			}
			if item.Valid && !item.Truncated && item.ToolName == "change.list_recent" && factsContainConfirmedChange(item.Facts) {
				confirmedChangeSupport = true
			}
		}
		if strong && len(ids) > 0 && !validSupport {
			problems = append(problems, path+" lacks valid non-truncated support")
		}
		if strong && changeClaimPattern.MatchString(statement) && !confirmedChangeSupport {
			problems = append(problems, path+" lacks deterministically correlated deployed-change support")
		}
	}
	for i, hypothesis := range d.Hypotheses {
		if strings.TrimSpace(hypothesis.Statement) == "" || hypothesis.Confidence < 0 || hypothesis.Confidence > 1 {
			problems = append(problems, fmt.Sprintf("hypotheses[%d] is malformed", i))
		}
		validateIDs(fmt.Sprintf("hypotheses[%d]", i), hypothesis.Statement, hypothesis.EvidenceIDs, hypothesis.Confidence >= 0.7)
	}
	for i, claim := range d.ConfirmedFacts {
		if strings.TrimSpace(claim.Statement) == "" {
			problems = append(problems, fmt.Sprintf("confirmed_facts[%d] is empty", i))
		}
		validateIDs(fmt.Sprintf("confirmed_facts[%d]", i), claim.Statement, claim.EvidenceIDs, true)
	}
	for i, action := range d.RecommendedNextActions {
		if len(action) > 1024 || prohibitedInstruction.MatchString(action) {
			problems = append(problems, fmt.Sprintf("recommended_next_actions[%d] contains prohibited or oversized instruction", i))
		}
	}
	data, err := json.Marshal(d)
	if err != nil || len(data) > 32*1024 {
		problems = append(problems, "diagnosis JSON exceeds size limit")
	}
	sort.Strings(problems)
	return problems
}

func factsContainConfirmedChange(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return walkConfirmedChange(value) && walkTrustedImageResolution(value)
}

func walkTrustedImageResolution(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		status, _ := typed["status"].(string)
		valid, _ := typed["valid"].(bool)
		truncated, _ := typed["truncated"].(bool)
		degraded, _ := typed["degraded"].(bool)
		registry, registryOK := typed["registry_metadata"].(map[string]any)
		if status == "confirmed" && valid && !truncated && !degraded && registryOK {
			digest, _ := typed["digest"].(string)
			revision, _ := typed["revision"].(string)
			source, _ := typed["source"].(string)
			version, _ := typed["version"].(string)
			registryValid, _ := registry["valid"].(bool)
			registryTruncated, _ := registry["truncated"].(bool)
			registryDegraded, _ := registry["degraded"].(bool)
			integrity, _ := registry["integrity_status"].(string)
			manifestDigest, _ := registry["manifest_digest"].(string)
			configDigest, _ := registry["config_digest"].(string)
			resultHash, _ := registry["result_hash"].(string)
			registryID, _ := registry["registry_id"].(string)
			repository, _ := registry["repository"].(string)
			registryRevision, _ := registry["revision"].(string)
			registrySource, _ := registry["source"].(string)
			registryVersion, _ := registry["version"].(string)
			redaction, redactionOK := registry["redaction"].(map[string]any)
			authOmitted, _ := redaction["auth_material_omitted"].(bool)
			responsesOmitted, _ := redaction["responses_omitted"].(bool)
			if registryValid && !registryTruncated && !registryDegraded && integrity == "verified" && verifiedDigest.MatchString(digest) && manifestDigest == digest && verifiedDigest.MatchString(configDigest) && verifiedRevision.MatchString(revision) && version == revision && registryRevision == revision && registrySource == source && registryVersion == version && registryID != "" && repository != "" && len(resultHash) == 64 && redactionOK && authOmitted && responsesOmitted {
				return true
			}
		}
		for _, child := range typed {
			if walkTrustedImageResolution(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if walkTrustedImageResolution(child) {
				return true
			}
		}
	}
	return false
}

func walkConfirmedChange(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		category, _ := typed["category"].(string)
		status, _ := typed["status"].(string)
		if (category == "confirmed_match" || category == "high_confidence") && (status == "matched" || status == "") {
			return true
		}
		for _, child := range typed {
			if walkConfirmedChange(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if walkConfirmedChange(child) {
				return true
			}
		}
	}
	return false
}

// DegradedDiagnosis is the deterministic fallback used when model correction fails.
func DegradedDiagnosis(state GraphState, reasons []string) Diagnosis {
	unknowns := append([]string{"Root cause could not be confirmed from valid evidence."}, reasons...)
	if len(unknowns) > maxDiagnosisItems {
		unknowns = unknowns[:maxDiagnosisItems]
	}
	return Diagnosis{
		Summary:                "Investigation completed with insufficient validated evidence.",
		Unknowns:               unknowns,
		Confidence:             0,
		AffectedResources:      compactStrings([]string{state.Incident.TargetKind + "/" + state.Incident.TargetName}),
		RecommendedNextActions: []string{"Review the bounded evidence and collect additional read-only observations."},
		Degraded:               true,
		BudgetSummary:          fmt.Sprintf("steps=%d/%d tools=%d/%d model_calls=%d/%d tokens=%d/%d", state.Usage.Steps, state.Limits.MaxSteps, state.Usage.ToolCalls, state.Limits.MaxToolCalls, state.Usage.ModelCalls, state.Limits.MaxModelCalls, state.Usage.TotalTokens(), state.Limits.TokenBudget),
		CoverageSummary:        state.Coverage.Reason,
	}
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 1024 {
			limit := 1024
			for limit > 0 && !utf8.ValidString(value[:limit]) {
				limit--
			}
			value = value[:limit]
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
