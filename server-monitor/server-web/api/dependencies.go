package api

import (
	"server-web/api/handlers"
	"server-web/api/middleware"
	copilotaction "server-web/copilot/action"
	copilotdiagnosis "server-web/copilot/diagnosis"
	copilotfeedback "server-web/copilot/feedback"
	copilothandler "server-web/copilot/handler"
	rediscache "server-web/redis"

	eventbus "server-monitor/pkg/kafka"
)

type CopilotRuntime struct {
	DiagnosisService *copilotdiagnosis.Service
	KafkaObserver    eventbus.ConsumerObserver
}

type CopilotDeps struct {
	Handler          *copilothandler.Handler
	DiagnosisHandler *copilotdiagnosis.Handler
	FeedbackHandler  *copilotfeedback.Handler
	ActionHandler    *copilotaction.Handler
}

type Dependencies struct {
	Metrics     *middleware.Metrics
	CacheClient *rediscache.Client
	Handler     *handlers.Handler
	AuthService handlers.AuthService
	Copilot     *CopilotDeps
}
