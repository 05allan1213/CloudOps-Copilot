package di

import (
	"context"

	"gorm.io/gorm"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
	eventbus "github.com/05allan1213/CloudOps-Copilot/internal/infra/kafka"
	promclient "github.com/05allan1213/CloudOps-Copilot/internal/infra/prometheus"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/notification"
	"github.com/05allan1213/CloudOps-Copilot/internal/observability"
	"github.com/05allan1213/CloudOps-Copilot/internal/router"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
	"github.com/05allan1213/CloudOps-Copilot/internal/telemetry"
)

type Infra struct {
	RedisClient    *rediscache.Client
	MySQL          *database.MySQL
	DB             *gorm.DB
	KafkaProducer  *eventbus.Producer
	PromClient     *promclient.Client
	ShutdownTracer func(context.Context) error
}

type Container struct {
	Infra
	Metrics        *middleware.Metrics
	Handler        *handler.Handler
	Queries        api.QueryPort
	Commands       api.CommandPort
	Alertmanager   *alertmanageringress.Handler
	Alerts         *alert.Service
	Settings       *settings.Service
	Notifications  *notification.Repository
	Infrastructure *infrastructure.Service
	Monitoring     *observability.Service
	Telemetry      *telemetry.Service
	AgentWorkspace *agent.WorkspaceRepository
}

func NewContainer(infra *Infra) *Container {
	return &Container{Infra: *infra}
}

func (c *Container) Dependencies() router.Dependencies {
	return router.Dependencies{
		Metrics:        c.Metrics,
		CacheClient:    c.RedisClient,
		Handler:        c.Handler,
		Queries:        c.Queries,
		Commands:       c.Commands,
		Alertmanager:   c.Alertmanager,
		Alerts:         c.Alerts,
		Settings:       c.Settings,
		Notifications:  c.Notifications,
		Infrastructure: c.Infrastructure,
		Monitoring:     c.Monitoring,
		Telemetry:      c.Telemetry,
		AgentWorkspace: c.AgentWorkspace,
	}
}
