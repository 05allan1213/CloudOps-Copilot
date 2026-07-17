package di

import (
	"context"

	"gorm.io/gorm"

	"server-web/internal/change"
	"server-web/internal/handler"
	promclient "server-web/internal/infra/prometheus"
	rediscache "server-web/internal/infra/redis"
	"server-web/internal/middleware"
	"server-web/internal/router"
	appalert "server-web/internal/service/alert"
	appincident "server-web/internal/service/incident"
	"server-web/internal/verification"

	eventbus "server-monitor/pkg/kafka"
)

type Infra struct {
	RedisClient    *rediscache.Client
	DB             *gorm.DB
	KafkaProducer  *eventbus.Producer
	PromClient     *promclient.Client
	ShutdownTracer func(context.Context) error
}

type Container struct {
	Infra
	AlertService    *appalert.Service
	IncidentService *appincident.Service
	AuthService     handler.AuthService
	Metrics         *middleware.Metrics
	Handler         *handler.Handler
	ChangeGitHub    change.GitHubReader
	ChangeArgoCD    change.ArgoCDReader
	DeliveryRollout verification.RolloutReader
}

func NewContainer(infra *Infra) *Container {
	return &Container{Infra: *infra}
}

func (c *Container) Dependencies() router.Dependencies {
	return router.Dependencies{
		Metrics:     c.Metrics,
		CacheClient: c.RedisClient,
		Handler:     c.Handler,
		AuthService: c.AuthService,
	}
}
