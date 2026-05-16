package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const taskKeyPrefix = "diagnosis:task:"

type TaskStore interface {
	TryStart(ctx context.Context, fingerprint string, ttl time.Duration) (bool, error)
	MarkRunning(ctx context.Context, fingerprint string, reportID uint64, ttl time.Duration) error
	MarkCompleted(ctx context.Context, fingerprint string, reportID uint64, ttl time.Duration) error
	MarkFailed(ctx context.Context, fingerprint string, errText string, ttl time.Duration) error
}

type TaskState struct {
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	ReportID    uint64    `json:"report_id"`
	TriggerType string    `json:"trigger_type"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Error       string    `json:"error"`
}

type taskRedis interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type RedisTaskStore struct {
	redis taskRedis
	now   func() time.Time
}

func NewRedisTaskStore(redis taskRedis, now func() time.Time) *RedisTaskStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &RedisTaskStore{redis: redis, now: now}
}

func TaskKey(fingerprint string) string {
	return taskKeyPrefix + strings.TrimSpace(fingerprint)
}

func (s *RedisTaskStore) TryStart(ctx context.Context, fingerprint string, ttl time.Duration) (bool, error) {
	if s == nil || s.redis == nil {
		return false, ErrUnavailable
	}
	now := s.now().UTC()
	value, err := marshalTaskState(TaskState{
		Fingerprint: strings.TrimSpace(fingerprint),
		Status:      StatusPending,
		TriggerType: TriggerAuto,
		StartedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return false, err
	}
	return s.redis.SetNX(ctx, TaskKey(fingerprint), value, ttl)
}

func (s *RedisTaskStore) MarkRunning(ctx context.Context, fingerprint string, reportID uint64, ttl time.Duration) error {
	return s.setState(ctx, fingerprint, reportID, StatusRunning, "", ttl)
}

func (s *RedisTaskStore) MarkCompleted(ctx context.Context, fingerprint string, reportID uint64, ttl time.Duration) error {
	return s.setState(ctx, fingerprint, reportID, StatusCompleted, "", ttl)
}

func (s *RedisTaskStore) MarkFailed(ctx context.Context, fingerprint string, errText string, ttl time.Duration) error {
	return s.setState(ctx, fingerprint, 0, StatusFailed, errText, ttl)
}

func (s *RedisTaskStore) setState(ctx context.Context, fingerprint string, reportID uint64, status string, errText string, ttl time.Duration) error {
	if s == nil || s.redis == nil {
		return ErrUnavailable
	}
	now := s.now().UTC()
	startedAt := now
	if value, ok := s.redis.Get(ctx, TaskKey(fingerprint)); ok {
		var existing TaskState
		if err := json.Unmarshal(value, &existing); err == nil && !existing.StartedAt.IsZero() {
			startedAt = existing.StartedAt.UTC()
		}
	}
	value, err := marshalTaskState(TaskState{
		Fingerprint: strings.TrimSpace(fingerprint),
		Status:      status,
		ReportID:    reportID,
		TriggerType: TriggerAuto,
		StartedAt:   startedAt,
		UpdatedAt:   now,
		Error:       strings.TrimSpace(errText),
	})
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, TaskKey(fingerprint), value, ttl)
}

func marshalTaskState(state TaskState) ([]byte, error) {
	if strings.TrimSpace(state.Fingerprint) == "" {
		return nil, fmt.Errorf("%w: fingerprint 不能为空", ErrInvalidRequest)
	}
	return json.Marshal(state)
}
