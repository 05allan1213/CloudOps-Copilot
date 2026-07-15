package change

import (
	"sort"
	"strings"
	"time"
)

type RuntimeContext struct {
	IncidentPublicID string    `json:"incident_id"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	Cluster          string    `json:"cluster"`
	Environment      string    `json:"environment"`
	Namespace        string    `json:"namespace"`
	ServiceName      string    `json:"service_name"`
	WorkloadKind     string    `json:"workload_kind"`
	WorkloadName     string    `json:"workload_name"`
	Image            string    `json:"image"`
	ImageDigest      string    `json:"image_digest"`
	Revision         string    `json:"revision"`
	ArgoApplication  string    `json:"argocd_application"`
}

type CorrelationCandidate struct {
	ChangeID         string   `json:"change_id"`
	Score            int      `json:"score"`
	Category         Category `json:"category"`
	Reasons          []string `json:"reasons"`
	TimeDeltaSeconds int64    `json:"time_delta_seconds"`
	RevisionMatch    bool     `json:"revision_match"`
	ImageMatch       bool     `json:"image_match"`
	ServiceMatch     bool     `json:"service_match"`
	Excluded         bool     `json:"excluded"`
}

type CorrelationResult struct {
	IncidentID     string                 `json:"incident_id"`
	EvaluatedAt    time.Time              `json:"evaluated_at"`
	LookbackStart  time.Time              `json:"lookback_start"`
	LookbackEnd    time.Time              `json:"lookback_end"`
	CurrentRuntime RuntimeContext         `json:"current_runtime"`
	Candidates     []CorrelationCandidate `json:"candidates"`
	Unknowns       []string               `json:"unknowns"`
	Degraded       bool                   `json:"degraded"`
}

func Correlate(runtime RuntimeContext, candidates []Change, lookback time.Duration, evaluatedAt time.Time) CorrelationResult {
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}
	runtime.FirstSeenAt = runtime.FirstSeenAt.UTC()
	result := CorrelationResult{IncidentID: runtime.IncidentPublicID, EvaluatedAt: evaluatedAt.UTC(), LookbackStart: runtime.FirstSeenAt.Add(-lookback), LookbackEnd: runtime.FirstSeenAt, CurrentRuntime: runtime}
	if runtime.ImageDigest == "" {
		result.Unknowns = append(result.Unknowns, "runtime image digest unavailable")
	}
	if runtime.Revision == "" {
		result.Unknowns = append(result.Unknowns, "runtime revision unavailable")
	}
	for index := range candidates {
		candidate := evaluateCandidate(runtime, &candidates[index], result.LookbackStart)
		result.Candidates = append(result.Candidates, candidate)
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].Excluded != result.Candidates[j].Excluded {
			return !result.Candidates[i].Excluded
		}
		if result.Candidates[i].Score != result.Candidates[j].Score {
			return result.Candidates[i].Score > result.Candidates[j].Score
		}
		return result.Candidates[i].ChangeID < result.Candidates[j].ChangeID
	})
	if len(candidates) == 0 {
		result.Unknowns = append(result.Unknowns, "no change candidates available")
	}
	return result
}

func evaluateCandidate(runtime RuntimeContext, item *Change, lookbackStart time.Time) CorrelationCandidate {
	result := CorrelationCandidate{ChangeID: item.PublicID}
	deployedAt := changeTime(*item)
	if deployedAt == nil {
		return exclude(result, "deployment time unavailable")
	}
	deployed := deployedAt.UTC()
	result.TimeDeltaSeconds = int64(runtime.FirstSeenAt.Sub(deployed).Seconds())
	if deployed.After(runtime.FirstSeenAt) {
		return exclude(result, "deployment occurred after incident")
	}
	if deployed.Before(lookbackStart) {
		return exclude(result, "deployment outside lookback window")
	}
	if !same(runtime.ServiceName, item.ServiceName) && !same(runtime.WorkloadName, item.WorkloadName) {
		return exclude(result, "service or workload mismatch")
	}
	result.ServiceMatch = true
	result.Score += 15
	result.Reasons = append(result.Reasons, "service_or_workload_match")
	if !same(runtime.Namespace, item.Namespace) {
		return exclude(result, "namespace mismatch")
	}
	result.Score += 10
	result.Reasons = append(result.Reasons, "namespace_match")
	if runtime.Environment != "" && item.Environment != "" && !same(runtime.Environment, item.Environment) {
		return exclude(result, "environment mismatch")
	}
	if runtime.Cluster != "" && item.Cluster != "" && !same(runtime.Cluster, item.Cluster) {
		return exclude(result, "cluster mismatch")
	}
	result.Score += 5
	result.Reasons = append(result.Reasons, "environment_cluster_match")
	if runtime.ArgoApplication != "" && item.ArgoCDApplication != "" && !same(runtime.ArgoApplication, item.ArgoCDApplication) {
		return exclude(result, "argocd application mismatch")
	}
	if strings.EqualFold(item.WorkflowConclusion, "failure") && item.ArgoCDDeployedRevision == "" {
		return exclude(result, "ci failed and revision was not deployed")
	}
	if runtime.Revision != "" {
		candidateRevision := firstNonEmpty(item.ArgoCDDeployedRevision, item.ImageRevision, item.CommitSHA)
		if candidateRevision != "" && sameRevision(runtime.Revision, candidateRevision) {
			result.RevisionMatch = true
			result.Score += 30
			result.Reasons = append(result.Reasons, "revision_exact_match")
		} else if candidateRevision != "" {
			return exclude(result, "revision mismatch")
		}
	}
	if runtime.ImageDigest != "" && item.ImageDigest != "" {
		if !same(runtime.ImageDigest, item.ImageDigest) {
			return exclude(result, "image digest mismatch")
		}
		result.ImageMatch = true
		result.Score += 25
		result.Reasons = append(result.Reasons, "image_digest_exact_match")
	} else if mutableTag(runtime.Image, item.ImageTag) {
		result.Reasons = append(result.Reasons, "mutable_tag_not_authoritative")
	}
	if item.ArgoCDDeployedRevision != "" {
		result.Score += 10
		result.Reasons = append(result.Reasons, "argocd_history_confirmed")
	}
	result.Score += 5
	result.Reasons = append(result.Reasons, "deployment_precedes_incident_within_lookback")
	if result.Score > 100 {
		result.Score = 100
	}
	switch {
	case result.Score >= 90 && result.RevisionMatch && result.ImageMatch:
		result.Category = CategoryConfirmed
	case result.Score >= 70:
		result.Category = CategoryHigh
	default:
		result.Category = CategoryLow
	}
	return result
}

func exclude(result CorrelationCandidate, reason string) CorrelationCandidate {
	result.Excluded = true
	result.Score = 0
	result.Category = CategoryExcluded
	result.Reasons = append(result.Reasons, reason)
	return result
}

func changeTime(item Change) *time.Time {
	if item.DeployedAt != nil {
		return item.DeployedAt
	}
	if item.CompletedAt != nil {
		return item.CompletedAt
	}
	return item.StartedAt
}

func same(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func sameRevision(left, right string) bool {
	left, right = strings.ToLower(strings.TrimSpace(left)), strings.ToLower(strings.TrimSpace(right))
	return left != "" && left == right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mutableTag(image, tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	return tag == "latest" || strings.HasSuffix(strings.ToLower(image), ":latest")
}
