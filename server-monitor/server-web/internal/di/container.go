package di

import (
	"context"
	"time"

	"gorm.io/gorm"

	"server-web/internal/handler"
	promclient "server-web/internal/infra/prometheus"
	rediscache "server-web/internal/infra/redis"
	"server-web/internal/infra/pubsub"
	ws "server-web/internal/infra/websocket"
	"server-web/internal/middleware"
	"server-web/internal/router"
	appalert "server-web/internal/service/alert"
	appcache "server-web/internal/service/cache"
	apphost "server-web/internal/service/host"

	eventbus "server-monitor/pkg/kafka"
)

type Infra struct {
	RedisClient    *rediscache.Client
	DB             *gorm.DB
	KafkaProducer  *eventbus.Producer
	PromClient     *promclient.Client
	WSHub          *ws.Hub
	AlertHub       *pubsub.Hub
	ShutdownTracer func(context.Context) error
}

type Container struct {
	Infra
	CacheService *appcache.Service
	HostService  *apphost.Service
	AlertService *appalert.Service
	AuthService  handler.AuthService
	Metrics      *middleware.Metrics
	Handler      *handler.Handler
	K8sHandler   *handler.K8sHandler
}

type Config struct {
	HostsTTL       time.Duration
	DashboardTTL   time.Duration
	RequestTimeout time.Duration
	CacheTimeout   time.Duration
}

func NewContainer(cfg Config, infra *Infra) *Container {
	cacheService := appcache.NewService(infra.RedisClient, appcache.Options{
		HostsTTL:     cfg.HostsTTL,
		DashboardTTL: cfg.DashboardTTL,
	})
	hostService := apphost.NewService(infra.PromClient, cacheService, apphost.Options{
		RequestTimeout: cfg.RequestTimeout,
		CacheTimeout:   cfg.CacheTimeout,
	})
	return &Container{
		Infra:        *infra,
		CacheService: cacheService,
		HostService:  hostService,
	}
}

func (c *Container) Dependencies() router.Dependencies {
	return router.Dependencies{
		Metrics:     c.Metrics,
		CacheClient: c.RedisClient,
		Handler:     c.Handler,
		K8sHandler:  c.K8sHandler,
		AuthService: c.AuthService,
	}
}

func (c *Container) Cache() *appcache.Service {
	return c.CacheService
}

func (c *Container) Host() *apphost.Service {
	return c.HostService
}