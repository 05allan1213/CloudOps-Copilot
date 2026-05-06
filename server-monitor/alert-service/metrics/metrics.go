package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry      *prometheus.Registry
	kafkaMessages *prometheus.CounterVec
	alertEvents   *prometheus.CounterVec
	kafkaReady    prometheus.Gauge
}

func New() *Metrics {
	metrics := &Metrics{
		registry: prometheus.NewRegistry(),
		kafkaMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "alert_service_kafka_messages_total",
			Help: "Total number of Kafka messages handled by alert-service.",
		}, []string{"result"}),
		alertEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "alert_service_alert_events_total",
			Help: "Total number of alert events processed by alert-service.",
		}, []string{"status", "result"}),
		kafkaReady: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "alert_service_kafka_ready",
			Help: "Whether alert-service Kafka consumer is ready. 1 means ready, 0 means not ready.",
		}),
	}

	metrics.registry.MustRegister(metrics.kafkaMessages, metrics.alertEvents, metrics.kafkaReady)
	return metrics
}

func (m *Metrics) HTTPHandler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveKafkaMessage(result string) {
	if m == nil {
		return
	}
	m.kafkaMessages.WithLabelValues(result).Inc()
}

func (m *Metrics) ObserveAlertEvent(status, result string) {
	if m == nil {
		return
	}
	m.alertEvents.WithLabelValues(status, result).Inc()
}

func (m *Metrics) SetKafkaReady(ready bool) {
	if m == nil {
		return
	}
	if ready {
		m.kafkaReady.Set(1)
		return
	}
	m.kafkaReady.Set(0)
}
