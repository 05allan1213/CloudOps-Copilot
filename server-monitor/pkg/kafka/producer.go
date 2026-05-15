// Package kafka provides shared Kafka event types and producer helpers.
package kafka

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

// AlertEvent is the normalized alert payload sent through Kafka.
type AlertEvent struct {
	// Type identifies the event category. Empty values default to "alert".
	Type string `json:"type"`
	// Fingerprint is the stable alert deduplication key.
	Fingerprint string `json:"fingerprint"`
	// Status is the alert lifecycle state, such as firing or resolved.
	Status string `json:"status"`
	// Labels contains Alertmanager labels.
	Labels map[string]string `json:"labels"`
	// Annotations contains Alertmanager annotations.
	Annotations map[string]string `json:"annotations"`
	// StartsAt is the time the alert started.
	StartsAt time.Time `json:"startsAt"`
	// EndsAt is the time the alert resolved.
	EndsAt time.Time `json:"endsAt"`
	// GeneratorURL points to the alert source when available.
	GeneratorURL string `json:"generatorURL,omitempty"`
	// ReceivedAt is the time this service received the alert.
	ReceivedAt time.Time `json:"receivedAt"`
}

type asyncProducer interface {
	Input() chan<- *sarama.ProducerMessage
	Successes() <-chan *sarama.ProducerMessage
	Errors() <-chan *sarama.ProducerError
	Close() error
}

// ProducerObserver observes asynchronous Kafka producer outcomes.
type ProducerObserver interface {
	ObserveKafkaAlertEvent(result string)
}

const (
	// AlertEventQueued indicates a message was queued for the Kafka producer.
	AlertEventQueued = "queued"
	// AlertEventDropped indicates a message was dropped because the producer input channel was full.
	AlertEventDropped = "dropped"
	// AlertEventSendSuccess indicates Kafka acknowledged a produced message.
	AlertEventSendSuccess = "send_success"
	// AlertEventSendError indicates Kafka returned an asynchronous producer error.
	AlertEventSendError = "send_error"
)

// Producer wraps a Sarama async producer for alert and operation events.
type Producer struct {
	producer   asyncProducer
	observer   ProducerObserver
	observerMu sync.RWMutex
	wg         sync.WaitGroup
}

// NewProducer creates a Kafka async producer for the provided brokers.
func NewProducer(brokers []string) (*Producer, error) {
	if len(brokers) == 0 {
		return nil, errors.New("kafka brokers is empty")
	}

	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForLocal
	config.Producer.Retry.Max = 3
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.Partitioner = sarama.NewHashPartitioner
	config.Producer.Flush.Messages = 100
	config.Producer.Flush.Frequency = 500 * time.Millisecond

	producer, err := sarama.NewAsyncProducer(brokers, config)
	if err != nil {
		return nil, err
	}

	return newProducer(producer), nil
}

func newProducer(producer asyncProducer) *Producer {
	p := &Producer{producer: producer}
	p.wg.Add(2)
	go p.handleSuccesses()
	go p.handleErrors()
	return p
}

// SetObserver sets the observer used for producer metrics.
func (p *Producer) SetObserver(observer ProducerObserver) {
	if p == nil {
		return
	}
	p.observerMu.Lock()
	defer p.observerMu.Unlock()
	p.observer = observer
}

// SendAlertEvent queues an alert event on the alert-events topic.
func (p *Producer) SendAlertEvent(event AlertEvent) error {
	if p == nil || p.producer == nil {
		return errors.New("kafka producer is not initialized")
	}
	if event.Type == "" {
		event.Type = "alert"
	}

	return p.sendJSON(TopicAlertEvents, event.Fingerprint, event, "alert event")
}

// SendOperationEvent queues an operation event on the operation-events topic.
func (p *Producer) SendOperationEvent(key string, event interface{}) error {
	if p == nil || p.producer == nil {
		return errors.New("kafka producer is not initialized")
	}
	if key == "" {
		key = "operation"
	}
	return p.sendJSON(TopicOperationEvents, key, event, "operation event")
}

func (p *Producer) sendJSON(topic, key string, value interface{}, label string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(data),
	}

	select {
	case p.producer.Input() <- msg:
		p.observe(AlertEventQueued)
		return nil
	default:
		p.observe(AlertEventDropped)
		return fmt.Errorf("kafka producer channel full, dropping %s", label)
	}
}

// Close closes the underlying producer and waits for callbacks to finish.
func (p *Producer) Close() error {
	if p == nil || p.producer == nil {
		return nil
	}

	err := p.producer.Close()
	p.wg.Wait()
	return err
}

func (p *Producer) handleSuccesses() {
	defer p.wg.Done()
	for msg := range p.producer.Successes() {
		zap.L().Debug("kafka message sent",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", msg.Partition),
			zap.Int64("offset", msg.Offset),
		)
		p.observe(AlertEventSendSuccess)
	}
}

func (p *Producer) handleErrors() {
	defer p.wg.Done()
	for producerErr := range p.producer.Errors() {
		fields := []zap.Field{zap.Error(producerErr.Err)}
		if producerErr.Msg != nil {
			fields = append(fields, zap.String("topic", producerErr.Msg.Topic))
		}
		zap.L().Warn("kafka produce failed", fields...)
		p.observe(AlertEventSendError)
	}
}

func (p *Producer) observe(result string) {
	if p == nil {
		return
	}
	p.observerMu.RLock()
	observer := p.observer
	p.observerMu.RUnlock()
	if observer == nil {
		return
	}
	observer.ObserveKafkaAlertEvent(result)
}
