package router

import (
	copilotaction "server-web/internal/copilot/action"
	copilotdiagnosis "server-web/internal/copilot/diagnosis"
	copilotfeedback "server-web/internal/copilot/feedback"
	copilothandler "server-web/internal/copilot/handler"
	"server-web/internal/handler"
	rediscache "server-web/internal/infra/redis"
	"server-web/internal/middleware"

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
	Handler     *handler.Handler
	K8sHandler  *handler.K8sHandler
	AuthService handler.AuthService
	Copilot     *CopilotDeps
}
