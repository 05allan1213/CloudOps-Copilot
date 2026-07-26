package incidentstore

import (
	"testing"
	"time"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

func TestDecideNoChangeWorkflowHonorsExternalWriteBoundary(t *testing.T) {
	tests := []struct {
		name       string
		state      noChangeWorkflowState
		eligible   bool
		cancelPlan bool
		cancelCR   bool
		reason     string
	}{
		{
			name: "investigating read-only work",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusInvestigating,
				Tasks: []noChangeTaskState{{TaskType: "investigation.advance", SubjectType: "agent_run", Status: "running"}}},
			eligible: true, reason: "cancel_read_only_work",
		},
		{
			name: "awaiting approval plan",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusAwaitingApproval,
				Plan: &noChangePlanState{ID: 1, Status: "awaiting_approval"}},
			eligible: true, cancelPlan: true, reason: "cancel_unconsumed_plan",
		},
		{
			name: "approved plan before request",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Plan: &noChangePlanState{ID: 1, Status: "approved"}},
			eligible: true, cancelPlan: true, reason: "cancel_approved_plan_before_change_request",
		},
		{
			name: "pending request before write",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Change: &noChangeRequestState{ID: 9, Status: "pending", PlanStatus: "consumed"},
				Tasks:  []noChangeTaskState{{TaskType: "change.ensure_pr", SubjectType: "change_request", SubjectID: 9, Status: "ready"}}},
			eligible: true, cancelCR: true, reason: "cancel_pending_change_before_write",
		},
		{
			name: "running request task",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Change: &noChangeRequestState{ID: 9, Status: "pending", PlanStatus: "consumed"},
				Tasks:  []noChangeTaskState{{TaskType: "change.ensure_pr", SubjectType: "change_request", SubjectID: 9, Status: "running"}}},
			reason: "external_write_may_be_in_flight",
		},
		{
			name: "request task identity mismatch",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Change: &noChangeRequestState{ID: 9, Status: "pending", PlanStatus: "consumed"},
				Tasks:  []noChangeTaskState{{TaskType: "change.ensure_pr", SubjectType: "change_request", SubjectID: 10, Status: "ready"}}},
			reason: "change_task_without_active_request",
		},
		{
			name: "persisted write marker",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Change: &noChangeRequestState{ID: 9, Status: "pending", PlanStatus: "consumed", ExternalWriteStarted: true}},
			reason: "external_write_intent_exists",
		},
		{
			name: "append-only write event",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Change: &noChangeRequestState{ID: 9, Status: "pending", PlanStatus: "consumed", ExternalWriteEvent: true}},
			reason: "external_write_intent_exists",
		},
		{
			name: "delivery reconciliation",
			state: noChangeWorkflowState{IncidentStatus: domain.StatusDelivering,
				Tasks: []noChangeTaskState{{TaskType: "delivery.observe", SubjectType: "change_request", Status: "ready"}}},
			reason: "delivery_reconciliation_active",
		},
		{
			name:   "active verification",
			state:  noChangeWorkflowState{IncidentStatus: domain.StatusInvestigating, ActiveVerification: true},
			reason: "verification_already_active",
		},
		{
			name:   "verifying status",
			state:  noChangeWorkflowState{IncidentStatus: domain.StatusVerifying},
			reason: "verification_already_active",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := decideNoChangeWorkflow(test.state)
			if decision.Eligible != test.eligible || decision.CancelPlan != test.cancelPlan ||
				decision.CancelChange != test.cancelCR || decision.Reason != test.reason {
				t.Fatalf("decision=%+v", decision)
			}
			if test.cancelCR && decision.ChangeID != test.state.Change.ID {
				t.Fatalf("change id=%d want=%d", decision.ChangeID, test.state.Change.ID)
			}
		})
	}
}

func TestNoChangeTriggerSignalSelectsLatestResolvedCurrentCycle(t *testing.T) {
	incident := incidentRow{id: 7, cycleNo: 3}
	base := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	signals := []insertedSignal{
		{id: 10, new: true, incidentID: 7, cycleNo: 3, input: SignalInput{Status: domain.SignalStatusResolved, OccurredAt: base}},
		{id: 11, new: true, incidentID: 8, cycleNo: 3, input: SignalInput{Status: domain.SignalStatusResolved, OccurredAt: base.Add(time.Minute)}},
		{id: 12, new: false, incidentID: 7, cycleNo: 3, input: SignalInput{Status: domain.SignalStatusResolved, OccurredAt: base.Add(2 * time.Minute)}},
		{id: 13, new: true, incidentID: 7, cycleNo: 3, input: SignalInput{Status: domain.SignalStatusFiring, OccurredAt: base.Add(3 * time.Minute)}},
		{id: 14, new: true, incidentID: 7, cycleNo: 3, input: SignalInput{Status: domain.SignalStatusResolved, OccurredAt: base.Add(4 * time.Minute)}},
	}
	selected, ok := noChangeTriggerSignal(signals, incident)
	if !ok || selected != 14 {
		t.Fatalf("selected=%d ok=%v", selected, ok)
	}
}
