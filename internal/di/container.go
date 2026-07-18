package di

import (
	"context"

	"gorm.io/gorm"

	"github.com/05allan1213/CloudOps-Copilot/internal/change"
	"github.com/05allan1213/CloudOps-Copilot/internal/handler"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
	promclient "github.com/05allan1213/CloudOps-Copilot/internal/infra/prometheus"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"
	"github.com/05allan1213/CloudOps-Copilot/internal/middleware"
	"github.com/05allan1213/CloudOps-Copilot/internal/router"
	appalert "github.com/05allan1213/CloudOps-Copilot/internal/service/alert"
	appincident "github.com/05allan1213/CloudOps-Copilot/internal/service/incident"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"

	eventbus "github.com/05allan1213/CloudOps-Copilot/internal/infra/kafka"
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
