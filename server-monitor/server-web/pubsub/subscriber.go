package pubsub

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"go.uber.org/zap"

	rediscache "server-web/redis"
)

const (
	reconnectInitialDelay = 1 * time.Second
	reconnectMaxDelay     = 30 * time.Second
)

type Subscriber struct {
	redisClient *rediscache.Client
	hub         *Hub
	channel     string
}

func NewSubscriber(redisClient *rediscache.Client, hub *Hub, channel string) *Subscriber {
	return &Subscriber{
		redisClient: redisClient,
		hub:         hub,
		channel:     channel,
	}
}

func (s *Subscriber) Run(ctx context.Context) {
	if s == nil || s.redisClient == nil || !s.redisClient.Enabled() || s.hub == nil || s.channel == "" {
		return
	}

	delay := reconnectInitialDelay

	for {
		err := s.subscribeOnce(ctx)
		if err == nil {
			return
		}

		waitDelay := withJitter(delay)
		zap.L().Warn("subscribe failed, reconnecting",
			zap.String("channel", s.channel),
			zap.Error(err),
			zap.Duration("delay", waitDelay),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(waitDelay):
		}

		delay *= 2
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
	}
}

func (s *Subscriber) subscribeOnce(ctx context.Context) error {
	messages, err := s.redisClient.Subscribe(ctx, s.channel)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-messages:
			if !ok {
				return fmt.Errorf("subscription channel closed")
			}
			if err := s.hub.PublishLocal(ctx, []byte(message)); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("publish local alert: %w", err)
			}
		}
	}
}

func withJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}

	jitter := delay / 5
	if jitter <= 0 {
		return delay
	}

	offset := time.Duration(rand.Int64N(int64(jitter)*2+1)) - jitter
	return delay + offset
}
