package di

import (
	"context"

	"gorm.io/gorm"

	"github.com/05allan1213/CloudOps-Copilot/internal/alertmanageringress"
	"github.com/05allan1213/CloudOps-Copilot/internal/api"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
	eventbus "github.com/05allan1213/CloudOps-Copilot/internal/infra/kafka"
	promclient "github.com/05allan1213/CloudOps-Copilot/internal/infra/prometheus"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/router"
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
	Metrics      *middleware.Metrics
	Handler      *handler.Handler
	Queries      api.QueryPort
	Commands     api.CommandPort
	Alertmanager *alertmanageringress.Handler
}

func NewContainer(infra *Infra) *Container {
	return &Container{Infra: *infra}
}

func (c *Container) Dependencies() router.Dependencies {
	return router.Dependencies{
		Metrics:      c.Metrics,
		CacheClient:  c.RedisClient,
		Handler:      c.Handler,
		Queries:      c.Queries,
		Commands:     c.Commands,
		Alertmanager: c.Alertmanager,
	}
}
