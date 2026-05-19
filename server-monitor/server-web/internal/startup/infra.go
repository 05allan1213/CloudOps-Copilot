package startup

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"server-web/internal/config"
	"server-web/internal/di"
	"server-web/internal/infra/database"
	promclient "server-web/internal/infra/prometheus"
	"server-web/internal/infra/pubsub"
	rediscache "server-web/internal/infra/redis"
	ws "server-web/internal/infra/websocket"

	eventbus "server-monitor/pkg/kafka"
	"server-monitor/pkg/tracer"
)

func InitInfra(ctx context.Context, cfg config.Config) (*di.Infra, error) {
	shutdownTracer := initTracer(ctx, cfg)
	gin.SetMode(cfg.GinMode)
	redisClient := rediscache.NewClient(rediscache.Options{
		Addr:            cfg.RedisAddr,
		Password:        cfg.RedisPassword,
		DB:              cfg.RedisDB,
		DialTimeout:     cfg.RedisDialTimeout,
		ReadTimeout:     cfg.RedisReadTimeout,
		WriteTimeout:    cfg.RedisWriteTimeout,
		ConnMaxLifetime: cfg.RedisConnMaxLifetime,
		ConnMaxIdleTime: cfg.RedisConnMaxIdleTime,
	})
	mysqlClient, err := initMySQL(cfg)
	if err != nil {
		return nil, err
	}
	if mysqlClient != nil {
		zap.L().Info("mysql initialized",
			zap.String("host", cfg.MySQLHost),
			zap.String("port", cfg.MySQLPort),
			zap.String("database", cfg.MySQLDatabase),
		)
	}
	return &di.Infra{
		ShutdownTracer: shutdownTracer,
		PromClient:     promclient.NewClient(cfg.PrometheusURL, cfg.RequestTimeout),
		RedisClient:    redisClient,
		DB:             dbFromMySQL(mysqlClient),
		KafkaProducer:  initKafkaProducer(cfg),
		WSHub:          ws.NewHub(cfg.WSMaxConnections, cfg.CORSOrigins),
		AlertHub:       pubsub.NewHub(64),
	}, nil
}

func initTracer(ctx context.Context, cfg config.Config) func(context.Context) error {
	shutdownTracer, err := tracer.Init(ctx, tracer.Config{
		ServiceName:  "server-web",
		OTLPEndpoint: cfg.TraceOTLPEndpoint,
		SampleRate:   cfg.TraceSampleRate,
	})
	if err != nil {
		zap.L().Warn("tracer init failed; tracing disabled",
			zap.String("endpoint", cfg.TraceOTLPEndpoint),
			zap.Error(err),
		)
		return func(context.Context) error { return nil }
	}
	if cfg.TraceOTLPEndpoint != "" {
		zap.L().Info("tracer initialized",
			zap.String("endpoint", cfg.TraceOTLPEndpoint),
			zap.Float64("sample_rate", cfg.TraceSampleRate),
		)
	}
	return shutdownTracer
}

func initMySQL(cfg config.Config) (*database.MySQL, error) {
	mysqlInitCtx, mysqlInitCancel := context.WithTimeout(context.Background(), cfg.MySQLStartupTimeout)
	mysqlClient, err := database.OpenMySQL(mysqlInitCtx, database.MySQLConfig{
		Host:        cfg.MySQLHost,
		Port:        cfg.MySQLPort,
		User:        cfg.MySQLUser,
		Password:    cfg.MySQLPassword,
		Database:    cfg.MySQLDatabase,
		PingTimeout: cfg.MySQLPingTimeout,
	})
	mysqlInitCancel()
	if err != nil {
		return nil, fmt.Errorf("mysql init failed: %w", err)
	}
	if mysqlClient != nil {
		if err := database.Migrate(mysqlClient.DB()); err != nil {
			return nil, fmt.Errorf("mysql migration failed: %w", err)
		}
	}
	return mysqlClient, nil
}

func initKafkaProducer(cfg config.Config) *eventbus.Producer {
	var kafkaProducer *eventbus.Producer
	if len(cfg.KafkaBrokers) > 0 {
		producer, err := eventbus.NewProducer(cfg.KafkaBrokers)
		if err != nil {
			zap.L().Warn("kafka producer init failed; kafka events disabled",
				zap.Strings("brokers", cfg.KafkaBrokers),
				zap.Error(err),
			)
		} else {
			kafkaProducer = producer
			zap.L().Info("kafka producer initialized", zap.Strings("brokers", cfg.KafkaBrokers))
		}
	}
	return kafkaProducer
}

func dbFromMySQL(mysqlClient *database.MySQL) *gorm.DB {
	if mysqlClient == nil {
		return nil
	}
	return mysqlClient.DB()
}
