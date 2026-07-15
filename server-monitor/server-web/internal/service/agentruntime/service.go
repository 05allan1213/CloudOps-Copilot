// Package agentruntime implements the durable bounded incident Agent application service.
package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"server-monitor/pkg/logger"
	"server-web/internal/agent"
	agentgraph "server-web/internal/agent/graph"
)

const checkpointSchemaVersion = 1

type Config struct {
	Enabled          bool
	WorkerID         string
	PollInterval     time.Duration
	LeaseDuration    time.Duration
	HeartbeatPeriod  time.Duration
	Model            string
	PromptVersion    string
	Limits           agent.Limits
	MaxGraphRunSteps int
	Observer         Observer
}

type Observer interface {
	ObserveAgentRun(status string, seconds float64)
	ObserveAgentStep(node, status string, seconds float64)
	ObserveAgentOperation(kind, name, result string, seconds float64)
	ObserveAgentEvent(kind, value string)
	SetAgentActive(status string, count float64)
}

func DefaultConfig() Config {
	return Config{Enabled: false, PollInterval: time.Second, LeaseDuration: 30 * time.Second, HeartbeatPeriod: 10 * time.Second, Model: "configured", PromptVersion: "incident-agent-v2", MaxGraphRunSteps: 96, Limits: agent.Limits{MaxSteps: 12, MaxToolCalls: 6, MaxModelCalls: 8, TokenBudget: 12000, MaxEvidenceItems: 12, MaxRuntime: 2 * time.Minute, ToolTimeout: 15 * time.Second, MaxEvidenceBytes: 16 * 1024, MaxCheckpointSize: 32 * 1024, MaxStepRetries: 1}}
}

type Service struct {
	store    agent.Store
	model    agent.Model
	tools    agent.ToolExecutor
	cfg      Config
	graph    *agentgraph.Executor
	now      func() time.Time
	observer Observer
}

func New(ctx context.Context, store agent.Store, model agent.Model, tools agent.ToolExecutor, cfg Config) (*Service, error) {
	if store == nil || model == nil || tools == nil {
		return nil, fmt.Errorf("%w: store, model, and tools are required", agent.ErrInvalidArgument)
	}
	if cfg.WorkerID == "" {
		cfg.WorkerID = "agent-worker"
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	service := &Service{store: store, model: model, tools: tools, cfg: cfg, observer: cfg.Observer, now: func() time.Time { return time.Now().UTC() }}
	graph, err := agentgraph.New(ctx, service, cfg.MaxGraphRunSteps)
	if err != nil {
		return nil, fmt.Errorf("compile incident agent graph: %w", err)
	}
	service.graph = graph
	return service, nil
}

func validateConfig(cfg Config) error {
	if cfg.PollInterval <= 0 || cfg.LeaseDuration <= 0 || cfg.HeartbeatPeriod <= 0 || cfg.HeartbeatPeriod >= cfg.LeaseDuration || cfg.MaxGraphRunSteps < 1 || cfg.Limits.MaxCheckpointSize < 1024 || cfg.Limits.MaxEvidenceBytes < 256 {
		return fmt.Errorf("%w: invalid agent runtime configuration", agent.ErrInvalidArgument)
	}
	if cfg.Limits.MaxSteps <= 0 || cfg.Limits.MaxToolCalls <= 0 || cfg.Limits.MaxModelCalls <= 0 || cfg.Limits.TokenBudget <= 0 || cfg.Limits.MaxEvidenceItems <= 0 || cfg.Limits.MaxRuntime <= 0 || cfg.Limits.ToolTimeout <= 0 || cfg.Limits.MaxStepRetries < 0 {
		return fmt.Errorf("%w: all runtime budgets must be positive", agent.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) CreateRun(ctx context.Context, incidentID, idempotencyKey string) (*agent.Run, error) {
	if !s.cfg.Enabled {
		return nil, agent.ErrUnavailable
	}
	now := s.now()
	state := agent.GraphState{SchemaVersion: checkpointSchemaVersion, IncidentPublicID: incidentID, NextNode: agent.NodeLoadIncident, Limits: s.cfg.Limits, StartedAt: now, DeadlineAt: now.Add(s.cfg.Limits.MaxRuntime)}
	checkpoint, _, err := encodeCheckpoint(&state, s.cfg.Limits.MaxCheckpointSize)
	if err != nil {
		return nil, err
	}
	run, err := s.store.CreateRun(ctx, agent.CreateRunRequest{IncidentPublicID: incidentID, IdempotencyKey: strings.TrimSpace(idempotencyKey), Model: s.cfg.Model, PromptVersion: s.cfg.PromptVersion, Limits: s.cfg.Limits, Checkpoint: checkpoint, At: now})
	if err == nil && s.observer != nil {
		s.observer.ObserveAgentRun("started", 0)
		s.observer.SetAgentActive("pending", 1)
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

// ProcessNext claims and processes at most one Run.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	if !s.cfg.Enabled {
		return false, nil
	}
	run, err := s.store.ClaimNext(ctx, s.cfg.WorkerID, s.now(), s.cfg.LeaseDuration)
	if errors.Is(err, agent.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if s.observer != nil {
		s.observer.ObserveAgentEvent("lease", "acquired")
		s.observer.ObserveAgentEvent("checkpoint", "resumed")
		s.observer.SetAgentActive("pending", 0)
		s.observer.SetAgentActive("running", 1)
	}
	return true, s.processRun(ctx, run)
}

func (s *Service) processRun(ctx context.Context, run *agent.Run) error {
	ctx, span := otel.Tracer("server-web/internal/service/agentruntime").Start(ctx, "agent.run")
	span.SetAttributes(attribute.String("agent.run_id", run.PublicID), attribute.String("incident.id", run.IncidentPublicID), attribute.String("agent.status", string(run.Status)))
	defer span.End()
	state := &agent.GraphState{}
	if len(run.Checkpoint) > 0 {
		if err := json.Unmarshal(run.Checkpoint, state); err != nil || state.SchemaVersion != checkpointSchemaVersion {
			return s.failCorruptCheckpoint(ctx, run, err)
		}
	}
	state.RunPublicID, state.IncidentPublicID = run.PublicID, run.IncidentPublicID
	state.Limits, state.Usage, state.RowVersion, state.CheckpointVersion = run.Limits, run.Usage, run.RowVersion, run.CheckpointVersion
	if run.StartedAt != nil {
		state.StartedAt = *run.StartedAt
	}
	if run.DeadlineAt != nil {
		state.DeadlineAt = *run.DeadlineAt
	}
	ctx, cancel := context.WithDeadline(ctx, state.DeadlineAt)
	defer cancel()
	ctx = context.WithValue(ctx, runContextKey{}, run)
	heartbeatDone := make(chan struct{})
	go s.heartbeat(ctx, run, heartbeatDone)
	_, err := s.graph.Invoke(ctx, state)
	close(heartbeatDone)
	if errors.Is(err, context.DeadlineExceeded) {
		return s.finishFailure(context.WithoutCancel(ctx), run, agent.ErrorTimeout, "agent runtime deadline exceeded")
	}
	return err
}

func (s *Service) heartbeat(ctx context.Context, run *agent.Run, done <-chan struct{}) {
	ticker := time.NewTicker(s.cfg.HeartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = s.store.Heartbeat(ctx, run.ID, run.LeaseOwner, now.UTC(), s.cfg.LeaseDuration)
		}
	}
}

func (s *Service) ExecuteNode(ctx context.Context, node agent.Node, state *agent.GraphState) (*agent.GraphState, error) {
	run, ok := ctx.Value(runContextKey{}).(*agent.Run)
	if !ok || run == nil {
		return nil, agent.ErrInvalidArgument
	}
	ctx, span := otel.Tracer("server-web/internal/service/agentruntime").Start(ctx, spanName(node))
	span.SetAttributes(attribute.String("agent.run_id", run.PublicID), attribute.String("incident.id", run.IncidentPublicID), attribute.String("agent.node", string(node)), attribute.Int("agent.used_steps", state.Usage.Steps))
	defer span.End()
	latest, err := s.store.GetRun(ctx, run.PublicID)
	if err != nil {
		return nil, err
	}
	if latest.CancelRequestedAt != nil && node != agent.NodeCancelled {
		run.RowVersion = latest.RowVersion
		state.NextNode = agent.NodeCancelled
		return state, nil
	}
	if !state.DeadlineAt.IsZero() && !s.now().Before(state.DeadlineAt) && node != agent.NodeBudgetExceeded {
		state.LastErrorCode, state.LastErrorSummary, state.NextNode = agent.ErrorTimeout, "runtime deadline exceeded", agent.NodeBudgetExceeded
		return state, nil
	}
	if node == agent.NodeCompleteRun || node == agent.NodeTerminalFailure || node == agent.NodeBudgetExceeded || node == agent.NodeCancelled {
		return s.executeTerminal(ctx, run, node, state)
	}
	budgeted := node != agent.NodeLoadIncident && node != agent.NodeBuildObjective && node != agent.NodePersistObservation
	if budgeted {
		if err := state.Usage.CanCharge(agent.Usage{Steps: 1}, state.Limits); err != nil {
			state.LastErrorCode, state.LastErrorSummary, state.NextNode = agent.ErrorBudgetExceeded, err.Error(), agent.NodeBudgetExceeded
			return state, nil
		}
	}
	started := s.now()
	step, err := s.store.BeginStep(ctx, run, agent.StepStart{Node: node, Reason: nodeReason(node), SelectedTool: state.CurrentAction.Tool, Arguments: state.CurrentAction.Arguments, ArgumentsHash: canonicalJSONHash(state.CurrentAction.Arguments), Budgeted: budgeted, At: started})
	if err != nil {
		if errors.Is(err, agent.ErrBudgetExceeded) {
			state.LastErrorCode, state.LastErrorSummary, state.NextNode = agent.ErrorBudgetExceeded, err.Error(), agent.NodeBudgetExceeded
			return state, nil
		}
		return nil, err
	}
	state.Usage, state.RowVersion = run.Usage, run.RowVersion
	delta, resultSummary, nodeErr := s.runNode(ctx, run, node, state)
	if current, currentErr := s.store.GetRun(ctx, run.PublicID); currentErr == nil && current.CancelRequestedAt != nil {
		run.RowVersion = current.RowVersion
		state.NextNode = agent.NodeCancelled
		if nodeErr != nil {
			nodeErr = agent.NewRuntimeError(agent.ErrorCancelled, "cancellation requested during external operation", agent.ErrCancelled)
		}
	}
	if s.observer != nil {
		kind, name := operationKind(node, state)
		if kind != "" {
			result := "success"
			if nodeErr != nil {
				result = "error"
			}
			s.observer.ObserveAgentOperation(kind, name, result, s.now().Sub(started).Seconds())
		}
	}
	if nodeErr != nil {
		return s.persistNodeFailure(ctx, run, step, node, state, delta, started, nodeErr)
	}
	state.LastCompletedNode, state.LastStepPublicID = node, step.PublicID
	checkpoint, hash, err := encodeCheckpoint(state, state.Limits.MaxCheckpointSize)
	if err != nil {
		return s.persistNodeFailure(ctx, run, step, node, state, agent.Usage{}, started, err)
	}
	finish := agent.StepFinish{Status: agent.StepCompleted, ResultSummary: resultSummary, Usage: delta, Checkpoint: checkpoint, CheckpointHash: hash, CheckpointSchema: checkpointSchemaVersion, DurationMS: s.now().Sub(started).Milliseconds(), At: s.now()}
	if node == agent.NodePersistObservation {
		observation := state.PendingObservation
		item := agent.EvidenceRecord{SourceType: "tool_observation", ToolName: observation.Tool, ResourceScope: state.Incident.Cluster + "/" + state.Incident.Namespace, Query: string(state.CurrentAction.Arguments), Summary: observation.Summary, Facts: observation.Facts, ResultHash: observation.ResultHash, Redaction: json.RawMessage(`{}`), Truncated: observation.Truncated, Valid: observation.Valid, IdempotencyKey: hashBytes([]byte(observation.ResultHash + ":" + observation.ArgumentsHash)), CollectedAt: observation.ObservedAt}
		checkpointCtx, checkpointSpan := otel.Tracer("server-web/internal/service/agentruntime").Start(ctx, "agent.checkpoint.persist")
		persisted, persistErr := s.store.PersistEvidence(checkpointCtx, run, step, item, finish)
		checkpointSpan.End()
		if persistErr != nil {
			return nil, persistErr
		}
		state.PendingObservation.EvidencePublicID = persisted.PublicID
		state.Observations[len(state.Observations)-1].EvidencePublicID = persisted.PublicID
		state.EvidenceIDs = appendUnique(state.EvidenceIDs, persisted.PublicID)
		state.Usage, state.RowVersion, state.CheckpointVersion = run.Usage, run.RowVersion, run.CheckpointVersion
		if s.observer != nil {
			s.observer.ObserveAgentEvent("evidence", "created")
			s.observer.ObserveAgentEvent("checkpoint", "persisted")
			s.observer.ObserveAgentStep(string(node), "completed", s.now().Sub(started).Seconds())
		}
		// Persist the Evidence ID in a second structural checkpoint; no external work is repeated.
		checkpoint, hash, err = encodeCheckpoint(state, state.Limits.MaxCheckpointSize)
		if err == nil {
			follow, beginErr := s.store.BeginStep(ctx, run, agent.StepStart{Node: agent.NodePersistObservation, Reason: "bind durable evidence id", At: s.now()})
			if beginErr == nil {
				_ = s.store.FinishStep(ctx, run, follow, agent.StepFinish{Status: agent.StepCompleted, ResultSummary: "evidence id bound to checkpoint", Checkpoint: checkpoint, CheckpointHash: hash, CheckpointSchema: checkpointSchemaVersion, At: s.now()})
			}
		}
		logger.FromContext(ctx).Info("incident agent evidence persisted", zap.String("incident_id", run.IncidentPublicID), zap.String("agent_run_id", run.PublicID), zap.String("agent_step_id", step.PublicID), zap.Int("sequence", step.Sequence), zap.String("tool", state.CurrentAction.Tool), zap.Uint64("checkpoint_version", run.CheckpointVersion))
		return state, nil
	}
	checkpointCtx, checkpointSpan := otel.Tracer("server-web/internal/service/agentruntime").Start(ctx, "agent.checkpoint.persist")
	err = s.store.FinishStep(checkpointCtx, run, step, finish)
	checkpointSpan.End()
	if err != nil {
		return nil, err
	}
	if s.observer != nil {
		s.observer.ObserveAgentEvent("checkpoint", "persisted")
		s.observer.ObserveAgentStep(string(node), "completed", s.now().Sub(started).Seconds())
		if node == agent.NodeRetryableFailure {
			s.observer.ObserveAgentEvent("retry", string(state.LastErrorCode))
		}
		if node == agent.NodeValidateDiagnosis {
			result := "passed"
			if len(state.ValidationErrors) > 0 {
				result = "failed"
			}
			s.observer.ObserveAgentEvent("validation", result)
		}
	}
	logger.FromContext(ctx).Info("incident agent step completed", zap.String("incident_id", run.IncidentPublicID), zap.String("agent_run_id", run.PublicID), zap.String("agent_step_id", step.PublicID), zap.Int("sequence", step.Sequence), zap.String("tool", state.CurrentAction.Tool), zap.Uint64("checkpoint_version", run.CheckpointVersion))
	state.Usage, state.RowVersion, state.CheckpointVersion = run.Usage, run.RowVersion, run.CheckpointVersion
	return state, nil
}

func (s *Service) runNode(ctx context.Context, run *agent.Run, node agent.Node, state *agent.GraphState) (agent.Usage, string, error) {
	switch node {
	case agent.NodeLoadIncident:
		incident, err := s.store.LoadIncident(ctx, run.IncidentID)
		if err != nil {
			return agent.Usage{}, "", err
		}
		state.Incident, state.NextNode = incident, agent.NodeBuildObjective
		return agent.Usage{}, "incident context loaded", nil
	case agent.NodeBuildObjective:
		state.Objective = boundString("Determine the most evidence-supported explanation for "+state.Incident.Summary+" using read-only observations.", 2048)
		state.NextNode = agent.NodePlanInvestigation
		return agent.Usage{}, "bounded objective created", nil
	case agent.NodePlanInvestigation, agent.NodeReplan:
		if err := state.Usage.CanCharge(agent.Usage{ModelCalls: 1}, state.Limits); err != nil {
			return agent.Usage{}, "", err
		}
		plan, usage, err := s.model.Plan(ctx, state.Incident, state.Objective)
		delta := modelDelta(usage)
		if err == nil && state.Usage.CanCharge(delta, state.Limits) != nil {
			err = agent.ErrBudgetExceeded
		}
		if err != nil {
			return agent.Usage{}, "", err
		}
		state.Plan, state.NextNode = plan, agent.NodeSelectAction
		return delta, "investigation plan persisted", nil
	case agent.NodeSelectAction:
		if err := state.Usage.CanCharge(agent.Usage{ModelCalls: 1}, state.Limits); err != nil {
			return agent.Usage{}, "", err
		}
		action, usage, err := s.model.SelectAction(ctx, *state, s.tools.AllowedTools())
		delta := modelDelta(usage)
		if err == nil {
			if !slices.Contains(s.tools.AllowedTools(), action.Tool) {
				err = agent.NewRuntimeError(agent.ErrorPermission, "model selected a non-allowlisted tool", agent.ErrInvalidArgument)
			} else if len(action.Arguments) == 0 || !json.Valid(action.Arguments) {
				err = agent.NewRuntimeError(agent.ErrorMalformedModel, "tool arguments are not valid JSON", agent.ErrInvalidArgument)
			} else if state.Usage.CanCharge(delta, state.Limits) != nil {
				err = agent.ErrBudgetExceeded
			}
		}
		if err != nil {
			return agent.Usage{}, "", err
		}
		state.CurrentAction, state.NextNode = action, agent.NodeExecuteTool
		return delta, "read-only action selected", nil
	case agent.NodeExecuteTool:
		if err := state.Usage.CanCharge(agent.Usage{ToolCalls: 1}, state.Limits); err != nil {
			return agent.Usage{}, "", err
		}
		result, err := s.tools.Execute(ctx, state.CurrentAction.Tool, state.CurrentAction.Arguments, state.Limits.ToolTimeout, state.Limits.MaxEvidenceBytes)
		if err != nil {
			return agent.Usage{}, "", err
		}
		observation := agent.Observation{Tool: state.CurrentAction.Tool, ArgumentsHash: canonicalJSONHash(state.CurrentAction.Arguments), Summary: boundString(result.Summary, 4096), Facts: result.Facts, ResultHash: result.ResultHash, Truncated: result.Truncated, Valid: result.Valid, ObservedAt: s.now()}
		state.PendingObservation, state.NextNode = observation, agent.NodePersistObservation
		return agent.Usage{ToolCalls: 1}, "read-only tool observation captured", nil
	case agent.NodePersistObservation:
		state.Observations = append(state.Observations, state.PendingObservation)
		state.NextNode = agent.NodeEvaluateCoverage
		return agent.Usage{Evidence: 1}, "observation persisted as evidence", nil
	case agent.NodeEvaluateCoverage:
		if err := state.Usage.CanCharge(agent.Usage{ModelCalls: 1}, state.Limits); err != nil {
			return agent.Usage{}, "", err
		}
		coverage, usage, err := s.model.EvaluateCoverage(ctx, *state)
		delta := modelDelta(usage)
		if err == nil && state.Usage.CanCharge(delta, state.Limits) != nil {
			err = agent.ErrBudgetExceeded
		}
		if err != nil {
			return agent.Usage{}, "", err
		}
		state.Coverage = coverage
		if coverage.Sufficient {
			state.NextNode = agent.NodeProduceDiagnosis
		} else {
			state.NextNode = agent.NodeReplan
		}
		return delta, "coverage evaluated from persisted observations", nil
	case agent.NodeProduceDiagnosis:
		if err := state.Usage.CanCharge(agent.Usage{ModelCalls: 1}, state.Limits); err != nil {
			return agent.Usage{}, "", err
		}
		diagnosis, usage, err := s.model.Diagnose(ctx, *state)
		delta := modelDelta(usage)
		if err == nil && state.Usage.CanCharge(delta, state.Limits) != nil {
			err = agent.ErrBudgetExceeded
		}
		if err != nil {
			return agent.Usage{}, "", err
		}
		state.Diagnosis, state.NextNode = diagnosis, agent.NodeValidateDiagnosis
		return delta, "candidate diagnosis produced", nil
	case agent.NodeValidateDiagnosis:
		evidence, err := s.store.ListEvidence(ctx, run.PublicID, state.Limits.MaxEvidenceItems)
		if err != nil {
			return agent.Usage{}, "", err
		}
		index := make(map[string]agent.EvidenceRecord, len(evidence))
		for _, item := range evidence {
			index[item.PublicID] = item
		}
		state.ValidationErrors = agent.ValidateDiagnosis(state.Diagnosis, index)
		if len(state.ValidationErrors) == 0 {
			state.NextNode = agent.NodeCompleteRun
		} else if state.CorrectionAttempts < 1 {
			state.CorrectionAttempts++
			state.NextNode = agent.NodeProduceDiagnosis
		} else {
			state.Diagnosis = agent.DegradedDiagnosis(*state, state.ValidationErrors)
			state.ValidationErrors = agent.ValidateDiagnosis(state.Diagnosis, index)
			state.NextNode = agent.NodeCompleteRun
		}
		return agent.Usage{}, "diagnosis deterministically validated", nil
	case agent.NodeRetryableFailure:
		state.CurrentRetryCount++
		state.NextNode = state.RetryNode
		return agent.Usage{}, "transient operation scheduled for bounded retry", nil
	default:
		return agent.Usage{}, "", agent.NewRuntimeError(agent.ErrorInvariant, "unimplemented graph node", agent.ErrInvalidArgument)
	}
}

func (s *Service) persistNodeFailure(ctx context.Context, run *agent.Run, step *agent.Step, node agent.Node, state *agent.GraphState, delta agent.Usage, started time.Time, cause error) (*agent.GraphState, error) {
	code, retryable := classifyRuntimeError(cause)
	state.LastErrorCode, state.LastErrorSummary = code, boundString(cause.Error(), 1024)
	if errors.Is(cause, agent.ErrBudgetExceeded) || code == agent.ErrorBudgetExceeded {
		state.NextNode = agent.NodeBudgetExceeded
	} else if code == agent.ErrorCancelled {
		state.NextNode = agent.NodeCancelled
	} else if retryable && state.CurrentRetryCount < state.Limits.MaxStepRetries {
		state.RetryNode, state.NextNode = node, agent.NodeRetryableFailure
	} else {
		state.NextNode = agent.NodeTerminalFailure
	}
	checkpoint, hash, encodeErr := encodeCheckpoint(state, state.Limits.MaxCheckpointSize)
	if encodeErr != nil {
		return nil, encodeErr
	}
	stepStatus := agent.StepFailed
	if code == agent.ErrorCancelled {
		stepStatus = agent.StepCancelled
	}
	if err := s.store.FinishStep(ctx, run, step, agent.StepFinish{Status: stepStatus, ResultSummary: state.LastErrorSummary, ErrorCode: code, RetryCount: state.CurrentRetryCount, Usage: delta, Checkpoint: checkpoint, CheckpointHash: hash, CheckpointSchema: checkpointSchemaVersion, DurationMS: s.now().Sub(started).Milliseconds(), At: s.now()}); err != nil {
		return nil, err
	}
	if s.observer != nil {
		s.observer.ObserveAgentStep(string(node), strings.ToLower(string(stepStatus)), s.now().Sub(started).Seconds())
		s.observer.ObserveAgentEvent("checkpoint", "persisted")
		if code == agent.ErrorBudgetExceeded {
			s.observer.ObserveAgentEvent("budget", "runtime")
		}
	}
	logger.FromContext(ctx).Warn("incident agent step failed", zap.String("incident_id", run.IncidentPublicID), zap.String("agent_run_id", run.PublicID), zap.String("agent_step_id", step.PublicID), zap.Int("sequence", step.Sequence), zap.String("failure_code", string(code)))
	state.Usage, state.RowVersion, state.CheckpointVersion = run.Usage, run.RowVersion, run.CheckpointVersion
	return state, nil
}

func (s *Service) executeTerminal(ctx context.Context, run *agent.Run, node agent.Node, state *agent.GraphState) (*agent.GraphState, error) {
	status, code, summary := agent.RunCompleted, agent.ErrorCode(""), ""
	switch node {
	case agent.NodeCancelled:
		status, code, summary = agent.RunCancelled, agent.ErrorCancelled, "cancellation requested"
	case agent.NodeBudgetExceeded:
		status, code, summary = agent.RunFailed, agent.ErrorBudgetExceeded, state.LastErrorSummary
	case agent.NodeTerminalFailure:
		status, code, summary = agent.RunFailed, state.LastErrorCode, state.LastErrorSummary
	}
	if err := s.store.FinishRun(ctx, run, status, state.Diagnosis, code, summary, s.now()); err != nil {
		return nil, err
	}
	if s.observer != nil {
		duration := s.now().Sub(state.StartedAt).Seconds()
		s.observer.ObserveAgentRun(strings.ToLower(string(status)), duration)
		s.observer.SetAgentActive("running", 0)
		if code == agent.ErrorBudgetExceeded {
			s.observer.ObserveAgentEvent("budget", "runtime")
		}
	}
	state.NextNode = agent.NodeEnd
	return state, nil
}

func (s *Service) failCorruptCheckpoint(ctx context.Context, run *agent.Run, cause error) error {
	message := "checkpoint is malformed or has an unsupported schema"
	if cause != nil {
		message += ": " + cause.Error()
	}
	return s.finishFailure(ctx, run, agent.ErrorInvariant, message)
}

func (s *Service) finishFailure(ctx context.Context, run *agent.Run, code agent.ErrorCode, summary string) error {
	return s.store.FinishRun(ctx, run, agent.RunFailed, agent.Diagnosis{}, code, summary, s.now())
}

type Worker struct {
	service *Service
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (s *Service) NewWorker() *Worker {
	return &Worker{service: s, stop: make(chan struct{}), done: make(chan struct{})}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(w.service.cfg.PollInterval)
		defer ticker.Stop()
		for {
			_, _ = w.service.ProcessNext(ctx)
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *Worker) Stop() {
	w.once.Do(func() { close(w.stop) })
	<-w.done
}

type runContextKey struct{}

func encodeCheckpoint(state *agent.GraphState, max int) (json.RawMessage, string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, "", err
	}
	if len(data) > max {
		return nil, "", agent.NewRuntimeError(agent.ErrorBudgetExceeded, "checkpoint size budget exhausted", agent.ErrBudgetExceeded)
	}
	return data, hashBytes(data), nil
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func canonicalJSONHash(value json.RawMessage) string {
	var decoded any
	if len(value) > 0 && json.Unmarshal(value, &decoded) == nil {
		if canonical, err := json.Marshal(decoded); err == nil {
			return hashBytes(canonical)
		}
	}
	return hashBytes(value)
}

func modelDelta(usage agent.ModelUsage) agent.Usage {
	return agent.Usage{ModelCalls: 1, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
}

func classifyRuntimeError(err error) (agent.ErrorCode, bool) {
	var runtimeErr *agent.RuntimeError
	if errors.As(err, &runtimeErr) {
		return runtimeErr.Code, runtimeErr.Retryable
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return agent.ErrorTimeout, true
	case errors.Is(err, agent.ErrBudgetExceeded):
		return agent.ErrorBudgetExceeded, false
	case errors.Is(err, agent.ErrCancelled):
		return agent.ErrorCancelled, false
	default:
		return agent.ErrorInvariant, false
	}
}

func nodeReason(node agent.Node) string { return "execute bounded node " + string(node) }

func spanName(node agent.Node) string {
	switch node {
	case agent.NodeLoadIncident:
		return "agent.load_incident"
	case agent.NodePlanInvestigation:
		return "agent.plan"
	case agent.NodeSelectAction:
		return "agent.select_action"
	case agent.NodeExecuteTool:
		return "agent.tool.execute"
	case agent.NodePersistObservation:
		return "agent.evidence.persist"
	case agent.NodeEvaluateCoverage:
		return "agent.coverage.evaluate"
	case agent.NodeReplan:
		return "agent.replan"
	case agent.NodeProduceDiagnosis:
		return "agent.diagnosis.generate"
	case agent.NodeValidateDiagnosis:
		return "agent.diagnosis.validate"
	case agent.NodeCompleteRun:
		return "agent.run.complete"
	default:
		return "agent." + string(node)
	}
}

func operationKind(node agent.Node, state *agent.GraphState) (string, string) {
	if node == agent.NodeExecuteTool {
		return "tool", state.CurrentAction.Tool
	}
	switch node {
	case agent.NodePlanInvestigation, agent.NodeReplan, agent.NodeSelectAction, agent.NodeEvaluateCoverage, agent.NodeProduceDiagnosis:
		return "model", string(node)
	default:
		return "", ""
	}
}

func appendUnique(values []string, value string) []string {
	if value != "" && !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}
func boundString(value string, max int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= max {
		return value
	}
	for max > 0 && !utf8.ValidString(value[:max]) {
		max--
	}
	return value[:max]
}
