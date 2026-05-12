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
