package verification

import (
	"encoding/json"
	"fmt"
	"time"
)

var deliveryTransitions = map[string]map[string]struct{}{
	"pr_created":      {"ci_pending": {}, "ci_passed": {}, "ci_failed": {}, "pr_closed": {}, "merge_timeout": {}, "delivery_cancelled": {}},
	"ci_pending":      {"ci_pending": {}, "ci_passed": {}, "ci_failed": {}, "pr_closed": {}, "merge_timeout": {}, "delivery_cancelled": {}},
	"ci_passed":       {"merge_pending": {}, "pr_closed": {}, "merge_timeout": {}, "delivery_cancelled": {}},
	"merge_pending":   {"merge_pending": {}, "merged": {}, "pr_closed": {}, "merge_timeout": {}, "delivery_cancelled": {}},
	"merged":          {"argocd_pending": {}, "revision_mismatch": {}, "argocd_timeout": {}, "delivery_cancelled": {}},
	"argocd_pending":  {"argocd_pending": {}, "syncing": {}, "synced": {}, "revision_mismatch": {}, "argocd_failed": {}, "argocd_timeout": {}, "delivery_cancelled": {}},
	"syncing":         {"syncing": {}, "synced": {}, "revision_mismatch": {}, "argocd_failed": {}, "argocd_timeout": {}, "delivery_cancelled": {}},
	"synced":          {"rollout_pending": {}, "revision_mismatch": {}, "rollout_failed": {}, "delivery_cancelled": {}},
	"rollout_pending": {"rollout_pending": {}, "delivered": {}, "revision_mismatch": {}, "rollout_failed": {}, "delivery_cancelled": {}},
}

func CanTransitionDelivery(from, to string) bool {
	next, ok := deliveryTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

func TerminalDelivery(status string) bool {
	switch status {
	case "delivered", "ci_failed", "pr_closed", "merge_timeout", "revision_mismatch", "argocd_failed", "argocd_timeout", "rollout_failed", "delivery_cancelled":
		return true
	default:
		return false
	}
}

func CanTransitionRun(from, to RunStatus) bool {
	switch from {
	case RunPending:
		return to == RunRunning || to == RunCancelled || to == RunTimedOut
	case RunRunning:
		return to == RunPassed || to == RunFailed || to == RunTimedOut || to == RunCancelled
	default:
		return false
	}
}

func TerminalRun(status RunStatus) bool {
	return status == RunPassed || status == RunFailed || status == RunTimedOut || status == RunCancelled
}

func CanTransitionCheck(from, to CheckStatus) bool {
	switch from {
	case CheckPending:
		return to == CheckRunning || to == CheckPassed || to == CheckFailed || to == CheckTimedOut || to == CheckUnavailable || to == CheckInvalid || to == CheckCancelled
	case CheckRunning, CheckUnavailable:
		return to == CheckRunning || to == CheckPassed || to == CheckFailed || to == CheckTimedOut || to == CheckUnavailable || to == CheckInvalid || to == CheckCancelled
	default:
		return false
	}
}

func ApplySample(check *Check, sample Sample, now time.Time) error {
	if check == nil || now.IsZero() || TerminalCheck(check.Status) {
		return fmt.Errorf("%w: immutable or missing check", ErrInvalidTransition)
	}
	now = now.UTC()
	if check.FirstCheckedAt == nil {
		check.FirstCheckedAt = &now
	}
	check.LastCheckedAt = &now
	check.AttemptCount++
	check.Observed = append(json.RawMessage(nil), sample.Observed...)
	check.SourceReference = bound(sample.SourceReference, 1024)
	check.FailureReason = bound(sample.ReasonCode, 128)
	switch sample.Status {
	case SamplePassed:
		if check.ConsecutiveSuccessSince == nil {
			check.ConsecutiveSuccessSince = &now
		}
		if now.Sub(*check.ConsecutiveSuccessSince) >= check.StabilityWindow {
			check.Status, check.PassedAt, check.FailureReason = CheckPassed, &now, ""
		} else {
			check.Status = CheckRunning
		}
	case SamplePending:
		check.Status, check.ConsecutiveSuccessSince = CheckRunning, nil
	case SampleUnavailable:
		check.Status, check.ConsecutiveSuccessSince = CheckUnavailable, nil
	case SampleFailed:
		check.Status, check.ConsecutiveSuccessSince = CheckFailed, nil
	case SampleInvalid:
		check.Status, check.ConsecutiveSuccessSince = CheckInvalid, nil
	case SampleTimedOut:
		check.Status, check.ConsecutiveSuccessSince = CheckTimedOut, nil
	default:
		return fmt.Errorf("%w: unknown sample", ErrInvalidArgument)
	}
	return nil
}

func TerminalCheck(status CheckStatus) bool {
	return status == CheckPassed || status == CheckFailed || status == CheckTimedOut || status == CheckInvalid || status == CheckCancelled
}

func Aggregate(checks []Check) (RunStatus, string, bool) {
	if len(checks) == 0 {
		return RunFailed, "verification_plan_empty", true
	}
	allRequiredPassed := true
	for _, check := range checks {
		if check.Required {
			switch check.Status {
			case CheckTimedOut:
				return RunTimedOut, "required_check_timed_out", true
			case CheckFailed, CheckInvalid, CheckCancelled:
				return RunFailed, "required_check_" + string(check.Status), true
			case CheckPassed:
			default:
				allRequiredPassed = false
			}
		}
	}
	if allRequiredPassed {
		return RunPassed, "all_required_checks_passed", true
	}
	return RunRunning, "checks_pending", false
}

func bound(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
