package verification

import "time"

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
		return to == RunPassed || to == RunFailed || to == RunTimedOut || to == RunInconclusive || to == RunCancelled
	default:
		return false
	}
}

func TerminalRun(status RunStatus) bool {
	return status == RunPassed || status == RunFailed || status == RunTimedOut || status == RunInconclusive || status == RunCancelled
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

// CommonWindowResult evaluates the shared stability window. Individual
// checks remain running until the maximum of all required success starts has
// stayed continuously valid; a single passed check cannot resolve a Run.
func CommonWindowResult(checks []Check, now time.Time, deadline time.Time) (RunStatus, string, bool, *time.Time) {
	if len(checks) == 0 {
		return RunFailed, "verification_plan_empty", true, nil
	}
	now = now.UTC()
	var commonStart time.Time
	// Classify explicit terminal/unavailable states before checking freshness so
	// the outcome is independent of slice order.
	for _, check := range checks {
		if !check.Required {
			continue
		}
		switch check.Status {
		case CheckFailed, CheckInvalid, CheckCancelled:
			return RunFailed, "required_check_" + string(check.Status), true, nil
		case CheckTimedOut:
			return RunTimedOut, "required_check_timed_out", true, nil
		case CheckUnavailable:
			if !deadline.IsZero() && !now.Before(deadline) {
				return RunInconclusive, "required_check_unavailable", true, nil
			}
			return RunRunning, "required_check_unavailable", false, nil
		}
	}
	for _, check := range checks {
		if !check.Required {
			continue
		}
		if check.ConsecutiveSuccessSince == nil {
			if !deadline.IsZero() && !now.Before(deadline) {
				return RunTimedOut, "common_stability_window_not_met", true, nil
			}
			return RunRunning, "common_stability_window_pending", false, nil
		}
		if commonStart.IsZero() || check.ConsecutiveSuccessSince.After(commonStart) {
			commonStart = *check.ConsecutiveSuccessSince
		}
		if check.LastCheckedAt == nil || now.Sub(check.LastCheckedAt.UTC()) > maxDuration(check.PollInterval*2, time.Second) {
			if !deadline.IsZero() && !now.Before(deadline) {
				return RunInconclusive, "required_check_revalidation_gap", true, nil
			}
			return RunRunning, "required_check_revalidation_pending", false, nil
		}
	}
	if commonStart.IsZero() {
		return RunRunning, "common_stability_window_pending", false, nil
	}
	if now.Sub(commonStart) < CommonStabilityWindow {
		if !deadline.IsZero() && !now.Before(deadline) {
			return RunTimedOut, "common_stability_window_not_met", true, &commonStart
		}
		return RunRunning, "common_stability_window_pending", false, &commonStart
	}
	return RunPassed, "all_required_checks_common_window_passed", true, &commonStart
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func bound(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
