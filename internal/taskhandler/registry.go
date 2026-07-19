// Package taskhandler maps the frozen task dispatch identities to bounded
// application operations. It never claims work; asyncjob.Runner is the sole
// claim owner.
package taskhandler

import (
	"context"
	"fmt"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
)

type Operation func(context.Context, asyncjob.Execution) asyncjob.Result

// DispatchKey is the complete runtime identity. A TaskType alone is not
// sufficient because investigation.advance has distinct start and step modes,
// and change.ensure_pr accepts two different durable subjects.
type DispatchKey struct {
	TaskType    asyncjob.TaskType
	SubjectType string
	Transition  string
}

var (
	investigationStartKey = DispatchKey{
		TaskType: asyncjob.TaskInvestigationAdvance, SubjectType: "incident", Transition: "investigation.start",
	}
	investigationStepKey = DispatchKey{
		TaskType: asyncjob.TaskInvestigationAdvance, SubjectType: "agent_run", Transition: "investigation.step",
	}
	remediationPrepareKey = DispatchKey{
		TaskType: asyncjob.TaskRemediationPrepare, SubjectType: "agent_run", Transition: "remediation.prepare",
	}
	changeEnsurePlanPRKey = DispatchKey{
		TaskType: asyncjob.TaskChangeEnsurePR, SubjectType: "remediation_plan", Transition: "change.ensure_pr",
	}
	changeEnsureRequestPRKey = DispatchKey{
		TaskType: asyncjob.TaskChangeEnsurePR, SubjectType: "change_request", Transition: "change.ensure_pr",
	}
	deliveryObserveKey = DispatchKey{
		TaskType: asyncjob.TaskDeliveryObserve, SubjectType: "change_request", Transition: "delivery.observe",
	}
	verificationAdvanceKey = DispatchKey{
		TaskType: asyncjob.TaskVerificationAdvance, SubjectType: "verification_run", Transition: "verification.advance",
	}
)

var frozenDispatchKeys = [...]DispatchKey{
	investigationStartKey,
	investigationStepKey,
	remediationPrepareKey,
	changeEnsurePlanPRKey,
	changeEnsureRequestPRKey,
	deliveryObserveKey,
	verificationAdvanceKey,
}

func (key DispatchKey) String() string {
	return string(key.TaskType) + "/" + key.SubjectType + "/" + key.Transition
}

func dispatchKey(task asyncjob.Task) DispatchKey {
	return DispatchKey{
		TaskType: task.Type, SubjectType: strings.TrimSpace(task.SubjectType), Transition: strings.TrimSpace(task.Transition),
	}
}

// Config injects the one-step application operations owned by later feature
// phases. Phase 2 provides investigation.start because it is part of the
// Incident/Task convergence transaction.
type Config struct {
	InvestigationStep   Operation
	RemediationPrepare  Operation
	ChangeEnsurePR      Operation
	DeliveryObserve     Operation
	VerificationAdvance Operation
}

// Registry dispatches only exact frozen identities. It adapts those identities
// to the five TaskType handlers required by asyncjob.Runner without weakening
// dispatch to a TaskType-only switch.
type Registry struct {
	operations map[DispatchKey]Operation
}

// NewRuntime returns production handlers only when every subject-bound
// one-step operation is explicitly provided. Missing operations are rejected
// before the Runner can claim a task.
func NewRuntime(config Config) (map[asyncjob.TaskType]asyncjob.Handler, error) {
	registry, err := NewRegistry(config)
	if err != nil {
		return nil, err
	}
	return registry.Handlers(), nil
}

// NewRegistry constructs the production exact-identity registry.
func NewRegistry(config Config) (*Registry, error) {
	missing := missingOperations(config)
	if len(missing) > 0 {
		return nil, fmt.Errorf("async task operations are not migrated: %s", strings.Join(missing, ", "))
	}
	return registry(config), nil
}

// New is the controlled registry used by operation-level integration tests.
// Production code must use NewRuntime. A missing owning operation deliberately
// returns ErrInvalidResult so a fixture cannot masquerade as migrated behavior.
func New(config Config) map[asyncjob.TaskType]asyncjob.Handler {
	notMigrated := func(context.Context, asyncjob.Execution) asyncjob.Result {
		return asyncjob.Result{}
	}
	if config.InvestigationStep == nil {
		config.InvestigationStep = notMigrated
	}
	if config.RemediationPrepare == nil {
		config.RemediationPrepare = notMigrated
	}
	if config.ChangeEnsurePR == nil {
		config.ChangeEnsurePR = notMigrated
	}
	if config.DeliveryObserve == nil {
		config.DeliveryObserve = notMigrated
	}
	if config.VerificationAdvance == nil {
		config.VerificationAdvance = notMigrated
	}
	return registry(config).Handlers()
}

func missingOperations(config Config) []string {
	missing := make([]string, 0, 5)
	if config.InvestigationStep == nil {
		missing = append(missing, "investigation.step")
	}
	if config.RemediationPrepare == nil {
		missing = append(missing, "remediation.prepare")
	}
	if config.ChangeEnsurePR == nil {
		missing = append(missing, "change.ensure_pr")
	}
	if config.DeliveryObserve == nil {
		missing = append(missing, "delivery.observe")
	}
	if config.VerificationAdvance == nil {
		missing = append(missing, "verification.advance")
	}
	return missing
}

func registry(config Config) *Registry {
	return &Registry{operations: map[DispatchKey]Operation{
		investigationStartKey:    investigationStart,
		investigationStepKey:     config.InvestigationStep,
		remediationPrepareKey:    config.RemediationPrepare,
		changeEnsurePlanPRKey:    config.ChangeEnsurePR,
		changeEnsureRequestPRKey: config.ChangeEnsurePR,
		deliveryObserveKey:       config.DeliveryObserve,
		verificationAdvanceKey:   config.VerificationAdvance,
	}}
}

// DispatchKeys returns a defensive copy of the frozen identities.
func (r *Registry) DispatchKeys() []DispatchKey {
	if r == nil {
		return nil
	}
	keys := make([]DispatchKey, len(frozenDispatchKeys))
	copy(keys, frozenDispatchKeys[:])
	return keys
}

// Handlers returns exactly the five adapters required by asyncjob.Runner.
func (r *Registry) Handlers() map[asyncjob.TaskType]asyncjob.Handler {
	handlers := make(map[asyncjob.TaskType]asyncjob.Handler, len(asyncjob.TaskTypes()))
	for _, taskType := range asyncjob.TaskTypes() {
		current := taskType
		handlers[current] = asyncjob.HandlerFunc(func(ctx context.Context, execution asyncjob.Execution) asyncjob.Result {
			expectedQueue, queueErr := asyncjob.QueueForTaskType(current)
			if execution.Task.Type != current || queueErr != nil || execution.Task.Queue != expectedQueue {
				return invalidDispatch(execution.Task)
			}
			operation, ok := r.operations[dispatchKey(execution.Task)]
			if !ok || operation == nil {
				return invalidDispatch(execution.Task)
			}
			return operation(ctx, execution)
		})
	}
	return handlers
}

func invalidDispatch(task asyncjob.Task) asyncjob.Result {
	key := dispatchKey(task)
	return asyncjob.Dead(
		"invalid_task_dispatch",
		fmt.Sprintf("unsupported async task dispatch identity %q", key.String()),
		nil,
	)
}
