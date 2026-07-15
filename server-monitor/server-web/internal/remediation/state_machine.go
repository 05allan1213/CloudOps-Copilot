package remediation

import "fmt"

var planTransitions = map[PlanStatus]map[PlanStatus]struct{}{
	PlanDraft:            {PlanAwaitingApproval: {}, PlanPolicyRejected: {}, PlanSuperseded: {}},
	PlanAwaitingApproval: {PlanApproved: {}, PlanRejected: {}, PlanCancelled: {}, PlanSuperseded: {}},
	PlanApproved:         {PlanDeliveryPending: {}, PlanCancelled: {}, PlanSuperseded: {}},
	PlanDeliveryPending:  {PlanDelivering: {}},
	PlanDelivering:       {PlanPRCreated: {}, PlanDeliveryPending: {}},
	PlanPRCreated:        {PlanCIPending: {}, PlanCIPassed: {}, PlanCIFailed: {}},
	PlanCIPending:        {PlanCIPassed: {}, PlanCIFailed: {}},
}

func CanTransition(from, to PlanStatus) bool {
	next, ok := planTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

func (p *RemediationPlan) Transition(to PlanStatus) error {
	if p == nil || !CanTransition(p.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, statusOf(p), to)
	}
	p.Status = to
	p.RowVersion++
	return nil
}

func statusOf(p *RemediationPlan) PlanStatus {
	if p == nil {
		return ""
	}
	return p.Status
}
