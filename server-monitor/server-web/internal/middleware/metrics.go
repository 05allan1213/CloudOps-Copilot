package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry               *prometheus.Registry
	requestsTotal          *prometheus.CounterVec
	requestDuration        *prometheus.HistogramVec
	websocketConnections   prometheus.Gauge
	kafkaAlertEvents       *prometheus.CounterVec
	kafkaMessages          *prometheus.CounterVec
	actionEvents           *prometheus.CounterVec
	actionDuration         *prometheus.HistogramVec
	copilotToolEvents      *prometheus.CounterVec
	copilotToolDuration    *prometheus.HistogramVec
	diagnosisConfidence    prometheus.Histogram
	diagnosisLLMTotal      *prometheus.CounterVec
	diagnosisDuration      *prometheus.HistogramVec
	llmRequestTotal        *prometheus.CounterVec
	llmRequestDuration     *prometheus.HistogramVec
	llmTokensTotal         *prometheus.CounterVec
	nluClassifyTotal       *prometheus.CounterVec
	nluClassifyDuration    *prometheus.HistogramVec
	ragSearchTotal         *prometheus.CounterVec
	ragSearchScore         prometheus.Histogram
	ragSearchDuration      prometheus.Histogram
	feedbackTotal          *prometheus.CounterVec
	feedbackCommentTotal   prometheus.Counter
	incidentSignals        *prometheus.CounterVec
	incidentsCreated       prometheus.Counter
	incidentsUpdated       prometheus.Counter
	incidentTransitions    *prometheus.CounterVec
	incidentErrors         *prometheus.CounterVec
	incidentDuration       *prometheus.HistogramVec
	outboxPending          prometheus.Gauge
	agentRuns              *prometheus.CounterVec
	agentRunDuration       *prometheus.HistogramVec
	agentSteps             *prometheus.CounterVec
	agentStepDuration      *prometheus.HistogramVec
	agentOperations        *prometheus.CounterVec
	agentOperationDuration *prometheus.HistogramVec
	agentRetries           *prometheus.CounterVec
	agentLeases            *prometheus.CounterVec
	agentCheckpoints       *prometheus.CounterVec
	agentBudgets           *prometheus.CounterVec
	agentEvidence          *prometheus.CounterVec
	agentValidation        *prometheus.CounterVec
	agentActiveRuns        *prometheus.GaugeVec
	changeCorrelation      *prometheus.CounterVec
	changeCorrelationTime  prometheus.Histogram
	changeCandidates       *prometheus.CounterVec
	changeEvidence         *prometheus.CounterVec
	githubRequests         *prometheus.CounterVec
	githubRequestTime      *prometheus.HistogramVec
	githubRateLimits       *prometheus.CounterVec
	githubDiffTruncations  *prometheus.CounterVec
	argoRequests           *prometheus.CounterVec
	argoRequestTime        *prometheus.HistogramVec
	argoDiffTruncations    *prometheus.CounterVec
	imageResolution        *prometheus.CounterVec
	registryRequests       *prometheus.CounterVec
	registryRequestTime    *prometheus.HistogramVec
	registryResponseLimits *prometheus.CounterVec
	registryCache          *prometheus.CounterVec
	imageConflicts         *prometheus.CounterVec
	remediationPlans       *prometheus.CounterVec
	changeRequestDelivery  *prometheus.CounterVec
	githubWriteRequests    *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewRegistry(),
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests handled by server-web.",
		}, []string{"method", "path", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"method", "path", "status"}),
		websocketConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "websocket_connections_active",
			Help: "Current number of active WebSocket connections.",
		}),
		kafkaAlertEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_web_kafka_alert_events_total",
			Help: "Total number of alert events handled by the server-web Kafka producer.",
		}, []string{"result"}),
		kafkaMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_web_kafka_diagnosis_messages_total",
			Help: "Total number of Kafka alert-events consumed by the diagnosis worker.",
		}, []string{"result"}),
		actionEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_web_action_events_total",
			Help: "Total number of action approval events handled by server-web.",
		}, []string{"operation", "result"}),
		actionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "server_web_action_execution_duration_seconds",
			Help:    "Action execution duration in seconds.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"action_type", "status"}),
		copilotToolEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "server_web_copilot_tool_calls_total",
			Help: "Total number of Copilot tool executions handled by server-web.",
		}, []string{"tool", "result"}),
		copilotToolDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "server_web_copilot_tool_duration_seconds",
			Help:    "Copilot tool execution duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"tool", "result"}),
		nluClassifyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "copilot_nlu_classify_total",
			Help: "Total number of NLU classifications.",
		}, []string{"intent", "source"}),
		nluClassifyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "copilot_nlu_classify_duration_seconds",
			Help:    "NLU classification duration in seconds.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
		}, []string{"source"}),
		ragSearchTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "copilot_rag_search_total",
			Help: "Total number of RAG searches.",
		}, []string{"has_result"}),
		ragSearchScore: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "copilot_rag_search_score",
			Help:    "RAG search top score distribution.",
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		}),
		ragSearchDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "copilot_rag_search_duration_seconds",
			Help:    "RAG search duration in seconds.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		}),
		diagnosisConfidence: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "copilot_diagnosis_confidence",
			Help:    "Diagnosis confidence distribution.",
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		}),
		diagnosisLLMTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "copilot_diagnosis_llm_total",
			Help: "Total number of diagnosis LLM calls.",
		}, []string{"result"}),
		diagnosisDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "copilot_diagnosis_duration_seconds",
			Help:    "Diagnosis duration in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"source"}),
		llmRequestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "copilot_llm_request_total",
			Help: "Total number of LLM requests.",
		}, []string{"model", "result"}),
		llmRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "copilot_llm_request_duration_seconds",
			Help:    "LLM request duration in seconds.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"model"}),
		llmTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "copilot_llm_tokens_total",
			Help: "Total number of LLM tokens.",
		}, []string{"model", "direction"}),
		feedbackTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "copilot_feedback_total",
			Help: "Total number of diagnosis feedback submissions.",
		}, []string{"rating"}),
		feedbackCommentTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "copilot_feedback_comment_total",
			Help: "Total number of diagnosis feedback with comments.",
		}),
		incidentSignals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cloudops_incident_signals_received_total",
			Help: "Total normalized Incident signals received by source, status and result.",
		}, []string{"source", "status", "result"}),
		incidentsCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cloudops_incidents_created_total",
			Help: "Total Incidents created by V2 ingestion.",
		}),
		incidentsUpdated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cloudops_incidents_updated_total",
			Help: "Total Incidents updated, resolved or reopened by V2 ingestion.",
		}),
		incidentTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cloudops_incident_transition_total",
			Help: "Total Incident state transition attempts.",
		}, []string{"from", "to", "result"}),
		incidentErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cloudops_incident_ingestion_errors_total",
			Help: "Total V2 Incident ingestion errors by bounded reason.",
		}, []string{"reason"}),
		incidentDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cloudops_incident_ingestion_duration_seconds",
			Help:    "V2 Incident webhook ingestion duration.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"result"}),
		outboxPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cloudops_outbox_pending_total",
			Help: "Current number of unpublished transactional outbox records.",
		}),
		agentRuns:              prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_runs_total", Help: "Durable incident Agent runs by bounded terminal or start status."}, []string{"status"}),
		agentRunDuration:       prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cloudops_agent_run_duration_seconds", Help: "Incident Agent run duration.", Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120}}, []string{"status"}),
		agentSteps:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_steps_total", Help: "Durable Agent steps by bounded node and status."}, []string{"node", "status"}),
		agentStepDuration:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cloudops_agent_step_duration_seconds", Help: "Agent step duration.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 15}}, []string{"node", "status"}),
		agentOperations:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_operations_total", Help: "Agent tool and model operations."}, []string{"kind", "name", "result"}),
		agentOperationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cloudops_agent_operation_duration_seconds", Help: "Agent tool and model operation duration.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 15, 30, 60}}, []string{"kind", "name", "result"}),
		agentRetries:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_retries_total", Help: "Persisted Agent retries by bounded reason."}, []string{"reason"}),
		agentLeases:            prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_leases_total", Help: "Agent lease lifecycle events."}, []string{"event"}),
		agentCheckpoints:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_checkpoints_total", Help: "Agent checkpoint lifecycle events."}, []string{"event"}),
		agentBudgets:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_budget_exceeded_total", Help: "Agent budget exhaustion by bounded budget kind."}, []string{"budget"}),
		agentEvidence:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_evidence_total", Help: "Agent evidence persistence results."}, []string{"result"}),
		agentValidation:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cloudops_agent_diagnosis_validation_total", Help: "Deterministic diagnosis validation results."}, []string{"result"}),
		agentActiveRuns:        prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cloudops_agent_runs_active", Help: "Locally observed pending and running Agent runs."}, []string{"status"}),
		changeCorrelation:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_correlation_total", Help: "Deterministic change correlation attempts by bounded result."}, []string{"result"}),
		changeCorrelationTime:  prometheus.NewHistogram(prometheus.HistogramOpts{Name: "change_correlation_duration_seconds", Help: "Deterministic change correlation duration.", Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1}}),
		changeCandidates:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_candidates_total", Help: "Persisted change candidates by bounded source."}, []string{"source"}),
		changeEvidence:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_evidence_total", Help: "Change evidence observations by bounded source and result."}, []string{"source", "result"}),
		githubRequests:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "github_requests_total", Help: "Read-only GitHub API requests by bounded operation and result."}, []string{"operation", "result"}),
		githubRequestTime:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "github_request_duration_seconds", Help: "Read-only GitHub API request duration.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"operation"}),
		githubRateLimits:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "github_rate_limit_events_total", Help: "GitHub rate-limit events by bounded reason."}, []string{"reason"}),
		githubDiffTruncations:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "github_diff_truncations_total", Help: "GitHub diff truncations by bounded reason."}, []string{"reason"}),
		argoRequests:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "argocd_requests_total", Help: "Read-only Argo CD API requests by bounded operation and result."}, []string{"operation", "result"}),
		argoRequestTime:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "argocd_request_duration_seconds", Help: "Read-only Argo CD API request duration.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"operation"}),
		argoDiffTruncations:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "argocd_diff_truncations_total", Help: "Argo CD resource diff truncations by bounded reason."}, []string{"reason"}),
		imageResolution:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "image_revision_resolution_total", Help: "Image revision resolution attempts by bounded result."}, []string{"result"}),
		registryRequests:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "registry_requests_total", Help: "Read-only registry requests by bounded operation and result."}, []string{"operation", "result"}),
		registryRequestTime:    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "registry_request_duration_seconds", Help: "Read-only registry request duration.", Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"operation"}),
		registryResponseLimits: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "registry_response_limit_total", Help: "Registry response byte-limit events by bounded response kind."}, []string{"kind"}),
		registryCache:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "registry_cache_total", Help: "Registry metadata cache events by bounded result."}, []string{"result"}),
		imageConflicts:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "image_resolution_conflicts_total", Help: "Image resolution conflicts by bounded kind."}, []string{"kind"}),
		remediationPlans:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "remediation_plans_total", Help: "Remediation plans by bounded lifecycle status."}, []string{"status"}),
		changeRequestDelivery:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "change_request_delivery_total", Help: "ChangeRequest delivery outcomes."}, []string{"result"}),
		githubWriteRequests:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "github_write_requests_total", Help: "Constrained GitHub write adapter requests."}, []string{"operation", "result"}),
	}

	metrics.registry.MustRegister(
		metrics.requestsTotal,
		metrics.requestDuration,
		metrics.websocketConnections,
		metrics.kafkaAlertEvents,
		metrics.kafkaMessages,
		metrics.actionEvents,
		metrics.actionDuration,
		metrics.copilotToolEvents,
		metrics.copilotToolDuration,
		metrics.nluClassifyTotal,
		metrics.nluClassifyDuration,
		metrics.ragSearchTotal,
		metrics.ragSearchScore,
		metrics.ragSearchDuration,
		metrics.diagnosisConfidence,
		metrics.diagnosisLLMTotal,
		metrics.diagnosisDuration,
		metrics.llmRequestTotal,
		metrics.llmRequestDuration,
		metrics.llmTokensTotal,
		metrics.feedbackTotal,
		metrics.feedbackCommentTotal,
		metrics.incidentSignals,
		metrics.incidentsCreated,
		metrics.incidentsUpdated,
		metrics.incidentTransitions,
		metrics.incidentErrors,
		metrics.incidentDuration,
		metrics.outboxPending,
		metrics.agentRuns,
		metrics.agentRunDuration,
		metrics.agentSteps,
		metrics.agentStepDuration,
		metrics.agentOperations,
		metrics.agentOperationDuration,
		metrics.agentRetries,
		metrics.agentLeases,
		metrics.agentCheckpoints,
		metrics.agentBudgets,
		metrics.agentEvidence,
		metrics.agentValidation,
		metrics.agentActiveRuns,
		metrics.changeCorrelation,
		metrics.changeCorrelationTime,
		metrics.changeCandidates,
		metrics.changeEvidence,
		metrics.githubRequests,
		metrics.githubRequestTime,
		metrics.githubRateLimits,
		metrics.githubDiffTruncations,
		metrics.argoRequests,
		metrics.argoRequestTime,
		metrics.argoDiffTruncations,
		metrics.imageResolution,
		metrics.registryRequests,
		metrics.registryRequestTime,
		metrics.registryResponseLimits,
		metrics.registryCache,
		metrics.imageConflicts,
		metrics.remediationPlans,
		metrics.changeRequestDelivery,
		metrics.githubWriteRequests,
	)
	return metrics
}

func (m *Metrics) ObserveRemediationPlan(status string) {
	if m != nil {
		m.remediationPlans.WithLabelValues(status).Inc()
	}
}

func (m *Metrics) ObserveChangeRequestDelivery(result string) {
	if m != nil {
		m.changeRequestDelivery.WithLabelValues(result).Inc()
	}
}

func (m *Metrics) ObserveGitHubWrite(operation, result string, _ float64) {
	if m != nil {
		m.githubWriteRequests.WithLabelValues(operation, result).Inc()
	}
}

func (m *Metrics) ObserveChangeCorrelation(result string, seconds float64) {
	if m != nil {
		m.changeCorrelation.WithLabelValues(result).Inc()
		m.changeCorrelationTime.Observe(seconds)
	}
}

func (m *Metrics) ObserveChangeCandidate(source string) {
	if m != nil {
		m.changeCandidates.WithLabelValues(source).Inc()
		m.changeEvidence.WithLabelValues(source, "persisted").Inc()
	}
}

func (m *Metrics) ObserveGitHubRequest(operation, result string, seconds float64) {
	if m != nil {
		m.githubRequests.WithLabelValues(operation, result).Inc()
		m.githubRequestTime.WithLabelValues(operation).Observe(seconds)
	}
}

func (m *Metrics) ObserveGitHubRateLimit(reason string) {
	if m != nil {
		m.githubRateLimits.WithLabelValues(reason).Inc()
	}
}

func (m *Metrics) ObserveGitHubDiffTruncation(reason string) {
	if m != nil {
		m.githubDiffTruncations.WithLabelValues(reason).Inc()
	}
}

func (m *Metrics) ObserveArgoCDRequest(operation, result string, seconds float64) {
	if m != nil {
		m.argoRequests.WithLabelValues(operation, result).Inc()
		m.argoRequestTime.WithLabelValues(operation).Observe(seconds)
	}
}

func (m *Metrics) ObserveArgoCDDiffTruncation(reason string) {
	if m != nil {
		m.argoDiffTruncations.WithLabelValues(reason).Inc()
	}
}

func (m *Metrics) ObserveImageResolution(result string) {
	if m != nil {
		m.imageResolution.WithLabelValues(result).Inc()
	}
}

func (m *Metrics) ObserveRegistryRequest(operation, result string, seconds float64) {
	if m != nil {
		m.registryRequests.WithLabelValues(operation, result).Inc()
		m.registryRequestTime.WithLabelValues(operation).Observe(seconds)
	}
}

func (m *Metrics) ObserveRegistryResponseLimit(kind string) {
	if m != nil {
		m.registryResponseLimits.WithLabelValues(kind).Inc()
	}
}

func (m *Metrics) ObserveRegistryCache(result string) {
	if m != nil {
		m.registryCache.WithLabelValues(result).Inc()
	}
}

func (m *Metrics) ObserveImageResolutionConflict(kind string) {
	if m != nil {
		m.imageConflicts.WithLabelValues(kind).Inc()
	}
}

func (m *Metrics) ObserveAgentRun(status string, seconds float64) {
	if m == nil {
		return
	}
	m.agentRuns.WithLabelValues(status).Inc()
	if seconds >= 0 {
		m.agentRunDuration.WithLabelValues(status).Observe(seconds)
	}
}

func (m *Metrics) ObserveAgentStep(node, status string, seconds float64) {
	if m == nil {
		return
	}
	m.agentSteps.WithLabelValues(node, status).Inc()
	m.agentStepDuration.WithLabelValues(node, status).Observe(seconds)
}

func (m *Metrics) ObserveAgentOperation(kind, name, result string, seconds float64) {
	if m == nil {
		return
	}
	m.agentOperations.WithLabelValues(kind, name, result).Inc()
	m.agentOperationDuration.WithLabelValues(kind, name, result).Observe(seconds)
}

func (m *Metrics) ObserveAgentEvent(kind, value string) {
	if m == nil {
		return
	}
	switch kind {
	case "retry":
		m.agentRetries.WithLabelValues(value).Inc()
	case "lease":
		m.agentLeases.WithLabelValues(value).Inc()
	case "checkpoint":
		m.agentCheckpoints.WithLabelValues(value).Inc()
	case "budget":
		m.agentBudgets.WithLabelValues(value).Inc()
	case "evidence":
		m.agentEvidence.WithLabelValues(value).Inc()
	case "validation":
		m.agentValidation.WithLabelValues(value).Inc()
	}
}

func (m *Metrics) SetAgentActive(status string, count float64) {
	if m != nil {
		m.agentActiveRuns.WithLabelValues(status).Set(count)
	}
}

func (m *Metrics) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		status := strconv.Itoa(c.Writer.Status())

		m.requestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		m.requestDuration.WithLabelValues(c.Request.Method, path, status).Observe(time.Since(start).Seconds())
	}
}

func (m *Metrics) HTTPHandler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) SetWebSocketConnections(count int) {
	m.websocketConnections.Set(float64(count))
}

func (m *Metrics) ObserveKafkaAlertEvent(result string) {
	if m == nil {
		return
	}
	m.kafkaAlertEvents.WithLabelValues(result).Inc()
}

func (m *Metrics) ObserveKafkaMessage(result string) {
	if m == nil {
		return
	}
	m.kafkaMessages.WithLabelValues(result).Inc()
}

func (m *Metrics) ObserveActionEvent(operation, result string) {
	if m == nil {
		return
	}
	m.actionEvents.WithLabelValues(operation, result).Inc()
}

func (m *Metrics) ObserveActionExecutionDuration(actionType, status string, seconds float64) {
	if m == nil {
		return
	}
	m.actionDuration.WithLabelValues(actionType, status).Observe(seconds)
}

func (m *Metrics) ObserveToolExecution(name, result string, seconds float64) {
	if m == nil {
		return
	}
	m.copilotToolEvents.WithLabelValues(name, result).Inc()
	m.copilotToolDuration.WithLabelValues(name, result).Observe(seconds)
}

func (m *Metrics) ObserveNLUClassify(intent, source string, durationSeconds float64) {
	if m == nil {
		return
	}
	m.nluClassifyTotal.WithLabelValues(intent, source).Inc()
	m.nluClassifyDuration.WithLabelValues(source).Observe(durationSeconds)
}

func (m *Metrics) ObserveRAGSearch(hasResult string, score, durationSeconds float64) {
	if m == nil {
		return
	}
	m.ragSearchTotal.WithLabelValues(hasResult).Inc()
	m.ragSearchScore.Observe(score)
	m.ragSearchDuration.Observe(durationSeconds)
}

func (m *Metrics) ObserveDiagnosis(confidence float64, result string, source string, durationSeconds float64) {
	if m == nil {
		return
	}
	m.diagnosisConfidence.Observe(confidence)
	m.diagnosisLLMTotal.WithLabelValues(result).Inc()
	m.diagnosisDuration.WithLabelValues(source).Observe(durationSeconds)
}

func (m *Metrics) ObserveLLMRequest(model, result string, durationSeconds float64, inputTokens, outputTokens int) {
	if m == nil {
		return
	}
	m.llmRequestTotal.WithLabelValues(model, result).Inc()
	m.llmRequestDuration.WithLabelValues(model).Observe(durationSeconds)
	if inputTokens > 0 {
		m.llmTokensTotal.WithLabelValues(model, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		m.llmTokensTotal.WithLabelValues(model, "output").Add(float64(outputTokens))
	}
}

func (m *Metrics) ObserveFeedback(rating string, hasComment bool) {
	if m == nil {
		return
	}
	m.feedbackTotal.WithLabelValues(rating).Inc()
	if hasComment {
		m.feedbackCommentTotal.Inc()
	}
}

func (m *Metrics) ObserveSignal(source, status, result string) {
	if m != nil {
		m.incidentSignals.WithLabelValues(source, status, result).Inc()
	}
}

func (m *Metrics) ObserveIncident(operation string) {
	if m == nil {
		return
	}
	if operation == "created" {
		m.incidentsCreated.Inc()
		return
	}
	m.incidentsUpdated.Inc()
}

func (m *Metrics) ObserveTransition(from, to, result string) {
	if m != nil {
		m.incidentTransitions.WithLabelValues(from, to, result).Inc()
	}
}

func (m *Metrics) ObserveIngestionError(reason string) {
	if m != nil {
		m.incidentErrors.WithLabelValues(reason).Inc()
	}
}

func (m *Metrics) ObserveIngestionDuration(result string, seconds float64) {
	if m != nil {
		m.incidentDuration.WithLabelValues(result).Observe(seconds)
	}
}

func (m *Metrics) ObserveOutboxPending(count int64) {
	if m != nil {
		m.outboxPending.Set(float64(count))
	}
}
