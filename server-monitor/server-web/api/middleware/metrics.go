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
	}

	metrics.registry.MustRegister(metrics.requestsTotal, metrics.requestDuration, metrics.websocketConnections, metrics.kafkaAlertEvents)
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
