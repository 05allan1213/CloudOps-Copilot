// Package application implements the repository-backed Incident Agent API surface.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

const checkpointSchemaVersion = 1

type Observer interface {
	ObserveAgentRun(status string, seconds float64)
	SetAgentActive(status string, count float64)
}

type Config struct {
	Enabled       bool
	Model         string
	PromptVersion string
	Limits        agent.Limits
	Observer      Observer
}

type Service struct {
	store agent.Store
	cfg   Config
	now   func() time.Time
}

func New(store agent.Store, cfg Config) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", agent.ErrInvalidArgument)
	}
	if cfg.Limits.MaxSteps <= 0 || cfg.Limits.MaxToolCalls <= 0 || cfg.Limits.MaxModelCalls <= 0 || cfg.Limits.TokenBudget <= 0 || cfg.Limits.MaxEvidenceItems <= 0 || cfg.Limits.MaxRuntime <= 0 || cfg.Limits.ToolTimeout <= 0 || cfg.Limits.MaxEvidenceBytes < 256 || cfg.Limits.MaxCheckpointSize < 1024 || cfg.Limits.MaxStepRetries < 0 {
		return nil, fmt.Errorf("%w: invalid agent application limits", agent.ErrInvalidArgument)
	}
	return &Service{store: store, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) CreateRun(ctx context.Context, incidentID, idempotencyKey string) (*agent.Run, error) {
	if !s.cfg.Enabled {
		return nil, agent.ErrUnavailable
	}
	now := s.now()
	state := agent.GraphState{
		SchemaVersion:    checkpointSchemaVersion,
		IncidentPublicID: incidentID,
		NextNode:         agent.NodeLoadIncident,
		Limits:           s.cfg.Limits,
		StartedAt:        now,
		DeadlineAt:       now.Add(s.cfg.Limits.MaxRuntime),
	}
	checkpoint, err := encodeCheckpoint(&state, s.cfg.Limits.MaxCheckpointSize)
	if err != nil {
		return nil, err
	}
	run, err := s.store.CreateRun(ctx, agent.CreateRunRequest{
		IncidentPublicID: incidentID,
		IdempotencyKey:   strings.TrimSpace(idempotencyKey),
		Model:            s.cfg.Model,
		PromptVersion:    s.cfg.PromptVersion,
		Limits:           s.cfg.Limits,
		Checkpoint:       checkpoint,
		At:               now,
	})
	if err == nil && s.cfg.Observer != nil {
		s.cfg.Observer.ObserveAgentRun("started", 0)
		s.cfg.Observer.SetAgentActive("pending", 1)
	}
	return run, err
}

func (s *Service) GetRun(ctx context.Context, id string) (*agent.Run, error) {
	return s.store.GetRun(ctx, id)
}

func (s *Service) ListRuns(ctx context.Context, incidentID string, page, pageSize int) (agent.Page, error) {
	return s.store.ListRunsByIncident(ctx, incidentID, page, pageSize)
}

func (s *Service) ListSteps(ctx context.Context, runID string, limit int) ([]agent.Step, error) {
	return s.store.ListSteps(ctx, runID, limit)
}

func (s *Service) ListEvidence(ctx context.Context, runID string, limit int) ([]agent.EvidenceRecord, error) {
	return s.store.ListEvidence(ctx, runID, limit)
}

func (s *Service) Cancel(ctx context.Context, runID string) error {
	return s.store.RequestCancel(ctx, runID, s.now())
}

func encodeCheckpoint(state *agent.GraphState, max int) (json.RawMessage, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, agent.NewRuntimeError(agent.ErrorBudgetExceeded, "checkpoint size budget exhausted", agent.ErrBudgetExceeded)
	}
	return data, nil
}
