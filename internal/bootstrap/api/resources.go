package api

import (
	"context"
	"errors"

	"github.com/05allan1213/CloudOps-Copilot/internal/di"
)

func closeInfra(ctx context.Context, infra *di.Infra) error {
	if infra == nil {
		return nil
	}
	var result error
	if infra.ShutdownTracer != nil {
		result = errors.Join(result, infra.ShutdownTracer(ctx))
	}
	if infra.KafkaProducer != nil {
		result = errors.Join(result, infra.KafkaProducer.Close())
	}
	if infra.RedisClient != nil {
		result = errors.Join(result, infra.RedisClient.Close())
	}
	if infra.MySQL != nil {
		result = errors.Join(result, infra.MySQL.Close())
	}
	return result
}
