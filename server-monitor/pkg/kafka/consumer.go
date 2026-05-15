// Package kafka provides shared Kafka event types, producers, and consumers.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"server-monitor/pkg/logger"
)

// AlertProcessor handles decoded alert events from Kafka.
type AlertProcessor interface {
	Process(ctx context.Context, event AlertEvent) error
}

// ConsumerObserver observes message processing outcomes.
type ConsumerObserver interface {
	ObserveKafkaMessage(result string)
}

type consumerGroup interface {
	Consume(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error
	Close() error
}

const (
	// MessageProcessed indicates the message was processed and committed.
	MessageProcessed = "processed"
	// MessageSkipped indicates the message was intentionally skipped and committed.
	MessageSkipped = "skipped"
	// MessageInvalidJSON indicates the message body could not be decoded.
	MessageInvalidJSON = "invalid_json"
	// MessagePermanentErr indicates processing failed permanently and the offset was committed.
	MessagePermanentErr = "permanent_error"
	// MessageProcessError indicates processing failed and the offset was not committed unless retries are disabled.
	MessageProcessError = "process_error"
)

type permanentError struct {
	err error
}

type skippedError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

func (e skippedError) Error() string {
	if e.err == nil {
		return "skipped"
	}
	return e.err.Error()
}

func (e skippedError) Unwrap() error {
	return e.err
}

// Permanent marks an error as non-retryable. The consumer commits the offset.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	if IsPermanent(err) {
		return err
	}
	return permanentError{err: err}
}

// Skipped marks a message as intentionally skipped. The consumer commits the offset.
func Skipped(err error) error {
	return skippedError{err: err}
}

// IsPermanent reports whether err was marked by Permanent.
func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

// IsSkipped reports whether err was marked by Skipped.
func IsSkipped(err error) bool {
	var target skippedError
	return errors.As(err, &target)
}

// ConsumerConfig configures a Kafka alert consumer.
type ConsumerConfig struct {
	Brokers      []string
	GroupID      string
	Topics       []string
	RetryBackoff func(int) time.Duration
	StopOnError  bool
}

// Consumer wraps a Sarama consumer group.
//
// Consume runs until the context is canceled. When RetryBackoff is set, consume
// errors mark the consumer not ready and are retried after the configured delay.
// When RetryBackoff is nil, consume errors are returned to the caller.
type Consumer struct {
	group        consumerGroup
	topics       []string
	handler      *consumerGroupHandler
	retryBackoff func(attempt int) time.Duration
	stopOnError  bool
}

// NewConsumer creates a Kafka consumer group for alert events.
func NewConsumer(cfg ConsumerConfig, processor AlertProcessor) (*Consumer, error) {
	if cfg.RetryBackoff != nil && cfg.StopOnError {
		return nil, fmt.Errorf("kafka: RetryBackoff and StopOnError are mutually exclusive")
	}
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers is empty")
	}
	if cfg.GroupID == "" {
		return nil, errors.New("kafka group id is empty")
	}
	if processor == nil {
		return nil, errors.New("alert processor is required")
	}

	group, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, newConsumerConfig())
	if err != nil {
		return nil, fmt.Errorf("create kafka consumer group: %w", err)
	}

	return newConsumer(cfg, processor, group), nil
}

func newConsumer(cfg ConsumerConfig, processor AlertProcessor, group consumerGroup) *Consumer {
	topics := append([]string(nil), cfg.Topics...)
	if len(topics) == 0 {
		topics = []string{TopicAlertEvents}
	}
	return &Consumer{
		group:        group,
		topics:       topics,
		handler:      &consumerGroupHandler{processor: processor},
		retryBackoff: cfg.RetryBackoff,
		stopOnError:  cfg.StopOnError,
	}
}

func newConsumerConfig() *sarama.Config {
	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRange()}
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	return config
}

// DefaultConsumeRetryBackoff returns the bounded exponential backoff used by retrying consumers.
func DefaultConsumeRetryBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := time.Second << attempt
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

// Consume starts consuming configured topics and invokes readiness callbacks.
func (c *Consumer) Consume(ctx context.Context, onReady, onNotReady func()) error {
	if c == nil || c.group == nil {
		return errors.New("kafka consumer is not initialized")
	}

	c.handler.onReady = onReady
	c.handler.onNotReady = onNotReady
	attempt := 0
	for ctx.Err() == nil {
		if err := c.group.Consume(ctx, c.topics, c.handler); err != nil {
			c.handler.notifyNotReady()
			if ctx.Err() != nil {
				break
			}
			if c.stopOnError || c.retryBackoff == nil {
				return fmt.Errorf("consume kafka topics: %w", err)
			}
			logger.FromContext(ctx).Warn("consume kafka topics failed; retrying",
				zap.Int("attempt", attempt+1),
				zap.Error(err),
			)
			delay := c.retryBackoff(attempt)
			attempt++
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
				case <-timer.C:
				}
			}
			continue
		}
		attempt = 0
	}
	c.handler.notifyNotReady()
	return nil
}

// Close closes the underlying consumer group.
func (c *Consumer) Close() error {
	if c == nil || c.group == nil {
		return nil
	}
	return c.group.Close()
}

// SetObserver sets the observer used for message processing metrics.
func (c *Consumer) SetObserver(observer ConsumerObserver) {
	if c == nil || c.handler == nil {
		return
	}
	c.handler.observerMu.Lock()
	defer c.handler.observerMu.Unlock()
	c.handler.observer = observer
}

// SetRetryableErrors controls whether retryable processing errors leave offsets uncommitted.
func (c *Consumer) SetRetryableErrors(enabled bool) {
	if c == nil || c.handler == nil {
		return
	}
	c.handler.commitRetryableErrors = !enabled
}

type consumerGroupHandler struct {
	processor             AlertProcessor
	observer              ConsumerObserver
	observerMu            sync.RWMutex
	commitRetryableErrors bool
	onReady               func()
	onNotReady            func()
}

func (h *consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	if h.onReady != nil {
		h.onReady()
	}
	return nil
}

func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	h.notifyNotReady()
	return nil
}

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		h.processMessage(session.Context(), session, msg)
	}
	return nil
}

type messageMarker interface {
	MarkMessage(*sarama.ConsumerMessage, string)
}

func (h *consumerGroupHandler) processMessage(ctx context.Context, marker messageMarker, msg *sarama.ConsumerMessage) {
	var event AlertEvent
	ctx, span := otel.Tracer("server-monitor/pkg/kafka").Start(ctx, "kafka.consume")
	defer span.End()
	span.SetAttributes(
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination.name", msg.Topic),
		attribute.Int64("messaging.kafka.partition", int64(msg.Partition)),
		attribute.Int64("messaging.kafka.offset", msg.Offset),
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.FromContext(ctx).Error("process alert event panic recovered, skipping offset commit",
				zap.String("topic", msg.Topic),
				zap.Int32("partition", msg.Partition),
				zap.Int64("offset", msg.Offset),
				zap.String("fingerprint", event.Fingerprint),
				zap.String("status", event.Status),
				zap.Any("panic", recovered),
			)
			h.observe(MessageProcessError)
		}
	}()

	if err := json.Unmarshal(msg.Value, &event); err != nil {
		logger.FromContext(ctx).Warn("unmarshal alert event failed",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
			zap.Error(err),
		)
		h.observe(MessageInvalidJSON)
		marker.MarkMessage(msg, "")
		return
	}

	if err := h.processor.Process(ctx, event); err != nil {
		span.SetAttributes(
			attribute.String("alert.fingerprint", event.Fingerprint),
			attribute.String("alert.status", event.Status),
		)
		if IsSkipped(err) {
			h.observe(MessageSkipped)
			marker.MarkMessage(msg, "")
			return
		}
		if IsPermanent(err) {
			logger.FromContext(ctx).Warn("process alert event failed permanently, committing offset",
				zap.String("topic", msg.Topic),
				zap.Int32("partition", msg.Partition),
				zap.Int64("offset", msg.Offset),
				zap.String("fingerprint", event.Fingerprint),
				zap.String("status", event.Status),
				zap.Error(err),
			)
			h.observe(MessagePermanentErr)
			marker.MarkMessage(msg, "")
			return
		}
		if h.commitRetryableErrors {
			logger.FromContext(ctx).Warn("process alert event failed; retries disabled, committing offset",
				zap.String("topic", msg.Topic),
				zap.Int32("partition", msg.Partition),
				zap.Int64("offset", msg.Offset),
				zap.String("fingerprint", event.Fingerprint),
				zap.String("status", event.Status),
				zap.Error(err),
			)
			h.observe(MessageProcessError)
			marker.MarkMessage(msg, "")
			return
		}

		logger.FromContext(ctx).Error("process alert event failed, skipping offset commit",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
			zap.String("fingerprint", event.Fingerprint),
			zap.String("status", event.Status),
			zap.Error(err),
		)
		h.observe(MessageProcessError)
		return
	}

	h.observe(MessageProcessed)
	marker.MarkMessage(msg, "")
}

func (h *consumerGroupHandler) notifyNotReady() {
	if h.onNotReady != nil {
		h.onNotReady()
	}
}

func (h *consumerGroupHandler) observe(result string) {
	h.observerMu.RLock()
	observer := h.observer
	h.observerMu.RUnlock()
	if observer == nil {
		return
	}
	observer.ObserveKafkaMessage(result)
}
