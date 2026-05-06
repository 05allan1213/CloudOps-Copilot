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

type AlertEvent struct {
	Type         string            `json:"type"`
	Fingerprint  string            `json:"fingerprint"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	ReceivedAt   time.Time         `json:"receivedAt"`
}

type asyncProducer interface {
	Input() chan<- *sarama.ProducerMessage
	Successes() <-chan *sarama.ProducerMessage
	Errors() <-chan *sarama.ProducerError
	Close() error
}

type ProducerObserver interface {
	ObserveKafkaAlertEvent(result string)
}

const (
	AlertEventQueued      = "queued"
	AlertEventDropped     = "dropped"
	AlertEventSendSuccess = "send_success"
	AlertEventSendError   = "send_error"
)

type Producer struct {
	producer   asyncProducer
	observer   ProducerObserver
	observerMu sync.RWMutex
	wg         sync.WaitGroup
}

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

func (p *Producer) SetObserver(observer ProducerObserver) {
	if p == nil {
		return
	}
	p.observerMu.Lock()
	defer p.observerMu.Unlock()
	p.observer = observer
}

func (p *Producer) SendAlertEvent(event AlertEvent) error {
	if p == nil || p.producer == nil {
		return errors.New("kafka producer is not initialized")
	}
	if event.Type == "" {
		event.Type = "alert"
	}

	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal alert event: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: TopicAlertEvents,
		Key:   sarama.StringEncoder(event.Fingerprint),
		Value: sarama.ByteEncoder(value),
	}

	select {
	case p.producer.Input() <- msg:
		p.observe(AlertEventQueued)
		return nil
	default:
		p.observe(AlertEventDropped)
		return errors.New("kafka producer channel full, dropping alert event")
	}
}

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
