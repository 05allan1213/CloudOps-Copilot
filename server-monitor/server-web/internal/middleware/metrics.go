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
	registry             *prometheus.Registry
	requestsTotal        *prometheus.CounterVec
	requestDuration      *prometheus.HistogramVec
	websocketConnections prometheus.Gauge
	kafkaAlertEvents     *prometheus.CounterVec
	kafkaMessages        *prometheus.CounterVec
	actionEvents         *prometheus.CounterVec
	actionDuration       *prometheus.HistogramVec
	copilotToolEvents    *prometheus.CounterVec
	copilotToolDuration  *prometheus.HistogramVec
	diagnosisConfidence  prometheus.Histogram
	diagnosisLLMTotal    *prometheus.CounterVec
	diagnosisDuration    *prometheus.HistogramVec
	llmRequestTotal      *prometheus.CounterVec
	llmRequestDuration   *prometheus.HistogramVec
	llmTokensTotal       *prometheus.CounterVec
	nluClassifyTotal     *prometheus.CounterVec
	nluClassifyDuration  *prometheus.HistogramVec
	ragSearchTotal       *prometheus.CounterVec
	ragSearchScore       prometheus.Histogram
	ragSearchDuration    prometheus.Histogram
	feedbackTotal        *prometheus.CounterVec
	feedbackCommentTotal prometheus.Counter
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
	)
	return metrics
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
