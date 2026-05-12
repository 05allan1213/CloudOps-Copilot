package diagnosis

import (
	"context"
	"fmt"
	"strings"
)

type RuleAnalyzer interface {
	Analyze(ctx context.Context, alert AlertContext, evidence EvidenceBundle) RuleAnalysis
}

type defaultRuleAnalyzer struct{}

func NewRuleAnalyzer() RuleAnalyzer {
	return defaultRuleAnalyzer{}
}

func (defaultRuleAnalyzer) Analyze(ctx context.Context, alert AlertContext, evidence EvidenceBundle) RuleAnalysis {
	_ = ctx
	results := make([]RuleResult, 0, 8)
	nextSteps := []string{"查看告警详情和最近变更", "确认目标实例当前健康状态"}

	add := func(rule string, passed bool, detail string, refs ...string) {
		results = append(results, RuleResult{
			Rule:         rule,
			Passed:       passed,
			Detail:       detail,
			EvidenceRefs: refs,
		})
	}

	cpu := findMetric(evidence.Metrics, "cpu")
	load := findMetric(evidence.Metrics, "load")
	memory := findMetric(evidence.Metrics, "memory")
	disk := findMetric(evidence.Metrics, "disk")
	up := findMetric(evidence.Metrics, "up")

	alertName := strings.ToLower(alert.AlertName)
	if strings.Contains(alertName, "cpu") {
		passed := cpu != nil && cpu.Avg >= 80 && cpu.Max >= 90
		add("cpu_sustained_high", passed, metricDetail("CPU", cpu, "15m avg >= 80 and max >= 90"), "metrics.cpu_usage")
		if passed {
			nextSteps = append(nextSteps, "查看 CPU Top 进程", "确认近期发布或流量变化")
		}

		loadPassed := load != nil && load.Avg >= 1
		add("load_correlated", loadPassed, metricDetail("load1", load, "load 与 CPU 同步升高"), "metrics.load1")
	}
	if strings.Contains(alertName, "memory") || strings.Contains(alertName, "内存") {
		passed := memory != nil && memory.Avg >= 80
		add("memory_sustained_high", passed, metricDetail("memory", memory, "memory avg >= 80"), "metrics.memory_usage")
		if passed {
			nextSteps = append(nextSteps, "检查进程内存占用和缓存增长")
		}
	}
	if strings.Contains(alertName, "disk") || strings.Contains(alertName, "磁盘") {
		passed := disk != nil && disk.Last >= 80
		add("disk_usage_high", passed, metricDetail("disk", disk, "disk last >= 80"), "metrics.disk_usage")
		if passed {
			nextSteps = append(nextSteps, "检查挂载点容量和大文件增长")
		}
	}
	if strings.Contains(alertName, "down") || strings.Contains(alertName, "hostdown") {
		passed := alert.Status == "firing" || (up != nil && up.Last < 1)
		add("host_unreachable", passed, "告警仍处于 firing 或 up 指标为 0", "alert_context.status", "metrics.up")
		if passed {
			nextSteps = append(nextSteps, "检查主机网络连通性和 exporter 状态")
		}
	}
	if evidence.K8s.Enabled {
		if deployment := firstDeployment(evidence.K8s); deployment != nil {
			passed := deployment.ReadyReplicas < deployment.Replicas
			add("k8s_deployment_not_ready", passed, fmt.Sprintf("Deployment ready=%d desired=%d updated=%d", deployment.ReadyReplicas, deployment.Replicas, deployment.UpdatedReplicas), "k8s.deployments")
			if passed {
				nextSteps = append(nextSteps, "检查 Deployment rollout、Pod 状态和 Warning Events")
			}
		}
		if restarts := podRestartCount(evidence.K8s); restarts > 0 {
			add("k8s_pod_restarts", true, fmt.Sprintf("关联 Pod 累计重启 %d 次", restarts), "k8s.pods")
			nextSteps = append(nextSteps, "查看最近 Pod 日志和 BackOff 事件")
		}
		if reason := warningEventReason(evidence.K8s); reason != "" {
			add("k8s_warning_event", true, fmt.Sprintf("发现 Warning Event: %s", reason), "k8s.events")
		}
	}

	recurring := repeatedHistory(evidence.History, alert)
	add("history_recurring", recurring >= 2, fmt.Sprintf("24 小时内同类历史记录 %d 条", recurring), "history")

	incomplete := len(evidence.CollectionErrors) > 0 || len(evidence.Metrics) == 0
	add("evidence_incomplete", incomplete, incompleteDetail(evidence), "collection_errors")

	confidence := confidenceScore(alert, evidence, incomplete)
	return RuleAnalysis{
		Summary:         buildRuleSummary(alert, results),
		Confidence:      confidence,
		ConfidenceLevel: ConfidenceLevel(confidence),
		Results:         results,
		NextSteps:       compactStrings(nextSteps),
	}
}

func findMetric(metrics []MetricEvidence, keyword string) *MetricEvidence {
	for i := range metrics {
		name := strings.ToLower(metrics[i].Name)
		if strings.Contains(name, keyword) {
			return &metrics[i]
		}
	}
	return nil
}

func metricDetail(label string, metric *MetricEvidence, threshold string) string {
	if metric == nil {
		return fmt.Sprintf("%s evidence missing; threshold: %s", label, threshold)
	}
	return fmt.Sprintf("%s avg=%.2f max=%.2f last=%.2f window=%s", label, metric.Avg, metric.Max, metric.Last, metric.Window)
}

func repeatedHistory(history []HistoryEvidence, alert AlertContext) int {
	count := 0
	for _, item := range history {
		if item.AlertName == alert.AlertName && item.Instance == alert.Instance {
			count++
		}
	}
	return count
}

func firstDeployment(evidence K8sEvidence) *k8sDeploymentView {
	if len(evidence.Deployments) == 0 {
		return nil
	}
	deployment := evidence.Deployments[0]
	return &k8sDeploymentView{Replicas: deployment.Replicas, ReadyReplicas: deployment.ReadyReplicas, UpdatedReplicas: deployment.UpdatedReplicas}
}

type k8sDeploymentView struct {
	Replicas        int32
	ReadyReplicas   int32
	UpdatedReplicas int32
}

func podRestartCount(evidence K8sEvidence) int32 {
	var total int32
	for _, pod := range evidence.Pods {
		total += pod.RestartCount
	}
	return total
}

func warningEventReason(evidence K8sEvidence) string {
	for _, event := range evidence.Events {
		if strings.EqualFold(event.Type, "Warning") {
			return firstNonEmpty(event.Reason, event.Message, event.Name)
		}
	}
	return ""
}

func incompleteDetail(evidence EvidenceBundle) string {
	if len(evidence.CollectionErrors) == 0 && len(evidence.Metrics) > 0 {
		return "核心证据完整"
	}
	if len(evidence.CollectionErrors) == 0 {
		return "指标证据为空"
	}
	parts := make([]string, 0, len(evidence.CollectionErrors))
	for _, item := range evidence.CollectionErrors {
		parts = append(parts, item.Source+": "+item.Error)
	}
	return strings.Join(parts, "; ")
}

func confidenceScore(alert AlertContext, evidence EvidenceBundle, incomplete bool) float64 {
	score := 0.0
	if alert.Fingerprint != "" && alert.AlertName != "" && alert.Instance != "" {
		score += 0.3
	}
	if len(evidence.Metrics) > 0 {
		score += 0.3
	}
	if len(evidence.History) > 0 {
		score += 0.2
	}
	score += runbookEvidenceScore(evidence) * 0.2
	if incomplete {
		score -= 0.2
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func runbookEvidenceScore(evidence EvidenceBundle) float64 {
	for _, item := range evidence.Runbooks {
		if strings.TrimSpace(item.Snippet) != "" {
			return 1.0
		}
	}
	for _, item := range evidence.CollectionErrors {
		if item.Source == ToolRunbookSearch {
			return 0.3
		}
	}
	return 0.4
}

func buildRuleSummary(alert AlertContext, results []RuleResult) string {
	passed := make([]string, 0, len(results))
	for _, result := range results {
		if result.Passed && result.Rule != "evidence_incomplete" {
			passed = append(passed, result.Rule)
		}
	}
	if len(passed) == 0 {
		return fmt.Sprintf("%s on %s 缺少足够指标证据，建议先补齐现场数据。", alert.AlertName, alert.Instance)
	}
	return fmt.Sprintf("%s on %s 命中规则：%s。", alert.AlertName, alert.Instance, strings.Join(passed, ", "))
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
