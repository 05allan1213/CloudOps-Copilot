package webhook

import "time"

type AlertmanagerWebhookRequest struct {
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	Alerts            []AlertRecord     `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
}

type AlertRecord struct {
	Status       string            `json:"status"`
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
}

type AlertEvent struct {
	Type         string            `json:"type"`
	Status       string            `json:"status"`
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	ReceivedAt   time.Time         `json:"receivedAt"`
}

func NewAlertEvent(alert AlertRecord, receivedAt time.Time) AlertEvent {
	return AlertEvent{
		Type:         "alert",
		Status:       alert.Status,
		Fingerprint:  alert.Fingerprint,
		Labels:       alert.Labels,
		Annotations:  alert.Annotations,
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		GeneratorURL: alert.GeneratorURL,
		ReceivedAt:   receivedAt,
	}
}
