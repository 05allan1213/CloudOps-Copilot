package startup

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/di"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
	promclient "github.com/05allan1213/CloudOps-Copilot/internal/infra/prometheus"
	rediscache "github.com/05allan1213/CloudOps-Copilot/internal/infra/redis"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/tracer"
	eventbus "github.com/05allan1213/CloudOps-Copilot/internal/infra/kafka"
)

type InfraOptions struct {
	ServiceName string
	EnableKafka bool
}

func InitInfra(ctx context.Context, cfg config.Config, options InfraOptions) (*di.Infra, error) {
	serviceName := strings.TrimSpace(options.ServiceName)
	if serviceName == "" {
		serviceName = "cloudops"
	}
	shutdownTracer := initTracer(ctx, cfg, serviceName)
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
	infra := &di.Infra{
		ShutdownTracer: shutdownTracer,
		PromClient:     promclient.NewClient(cfg.PrometheusURL, cfg.RequestTimeout),
		RedisClient:    redisClient,
		MySQL:          mysqlClient,
		DB:             dbFromMySQL(mysqlClient),
	}
	if options.EnableKafka {
		infra.KafkaProducer = initKafkaProducer(cfg)
	}
	return infra, nil
}

func initTracer(ctx context.Context, cfg config.Config, serviceName string) func(context.Context) error {
	shutdownTracer, err := tracer.Init(ctx, tracer.Config{
		ServiceName:  serviceName,
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
