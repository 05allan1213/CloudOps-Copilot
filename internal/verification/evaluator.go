package verification

import (
	"encoding/json"
	"math"
	"time"
)

// EvaluateObservation is the bounded evaluator for a frozen profile. A zero
// value is never interpreted as a healthy metric; source/query/retention
// health and minimum sample counts must be explicit.
func EvaluateObservation(check Check, observation Observation, now time.Time) Sample {
	now = now.UTC()
	bounded, err := json.Marshal(observation)
	if err != nil || len(bounded) > 16*1024 {
		return Sample{Status: SampleInvalid, Observed: json.RawMessage(`{"status":"malformed"}`), ReasonCode: "malformed_observation"}
	}
	sample := Sample{Observed: bounded, SourceReference: bound(observation.SourceReference, 1024)}
	if observation.Status == ObservationUnavailable {
		return withReason(sample, SampleUnavailable, nonEmptyReason(observation.ReasonCode, "provider_unavailable"))
	}
	if observation.Status == ObservationMalformed {
		return withReason(sample, SampleInvalid, "malformed_observation")
	}
	if observation.Status == ObservationNoData {
		if check.Type == CheckLogRequiredEnvAbsent && check.Comparison == CompareAbsent && observation.QueryValid && observation.SourceHealthy && observation.RetentionCovered && !observation.Truncated {
			return withReason(sample, SamplePassed, "absence_confirmed")
		}
		return withReason(sample, SampleUnavailable, "no_data")
	}
	if observation.Status != ObservationAvailable || !observation.QueryValid || !observation.SourceHealthy || !observation.RetentionCovered || observation.Truncated {
		return withReason(sample, SampleUnavailable, "observation_not_usable")
	}
	if observation.SampledAt.IsZero() || (!check.LookbackIsZero() && (now.Sub(observation.SampledAt.UTC()) > check.Lookback || observation.SampledAt.After(now.Add(time.Second)))) {
		return withReason(sample, SampleInvalid, "invalid_or_stale_sample")
	}
	if check.MinSamples > 0 && sampleCount(check, observation) < check.MinSamples {
		return withReason(sample, SampleUnavailable, "insufficient_samples")
	}
	if isBooleanObservationCheck(check.Type) {
		if observation.MatchedCount > 0 || observation.Value == 0 {
			if check.FailureMode == FailureImmediate {
				return withReason(sample, SampleFailed, booleanObservationReason(check.Type, false))
			}
			return withReason(sample, SamplePending, booleanObservationReason(check.Type, false))
		}
		return withReason(sample, SamplePassed, booleanObservationReason(check.Type, true))
	}
	if check.Comparison == CompareAbsent {
		if observation.MatchedCount == 0 {
			return withReason(sample, SamplePassed, "absence_confirmed")
		}
		return negativeSample(sample, check, "matches_present")
	}
	if math.IsNaN(observation.Value) || math.IsInf(observation.Value, 0) {
		return withReason(sample, SampleInvalid, "invalid_value")
	}
	passed := compare(observation.Value, check.Comparison, check.Threshold)
	if passed {
		return withReason(sample, SamplePassed, "threshold_satisfied")
	}
	return negativeSample(sample, check, "threshold_not_satisfied")
}

func isBooleanObservationCheck(checkType CheckType) bool {
	switch checkType {
	case CheckArgoExactRevision, CheckArgoSyncSucceeded, CheckDeploymentObserved,
		CheckDeploymentRolloutComplete, CheckWorkloadReady, CheckIncidentAlertsResolved,
		CheckDeploymentIdentity:
		return true
	default:
		return false
	}
}

func booleanObservationReason(checkType CheckType, satisfied bool) string {
	if satisfied {
		switch checkType {
		case CheckArgoExactRevision, CheckDeploymentIdentity:
			return "identity_matches"
		case CheckArgoSyncSucceeded:
			return "sync_succeeded"
		case CheckDeploymentObserved:
			return "generation_observed"
		case CheckDeploymentRolloutComplete:
			return "rollout_complete"
		case CheckWorkloadReady:
			return "workload_ready"
		case CheckIncidentAlertsResolved:
			return "alerts_resolved"
		}
	}
	switch checkType {
	case CheckArgoExactRevision, CheckDeploymentIdentity:
		return "identity_mismatch"
	case CheckArgoSyncSucceeded:
		return "sync_not_succeeded"
	case CheckDeploymentObserved:
		return "generation_not_observed"
	case CheckDeploymentRolloutComplete:
		return "rollout_incomplete"
	case CheckWorkloadReady:
		return "workload_not_ready"
	case CheckIncidentAlertsResolved:
		return "alerts_firing"
	default:
		return "check_not_satisfied"
	}
}

// ApplySample records one bounded sample but deliberately leaves a positive
// check in running state until CommonWindowResult proves the shared window.
func ApplySample(check *Check, sample Sample, now time.Time) error {
	if check == nil || now.IsZero() || TerminalCheck(check.Status) {
		return ErrInvalidTransition
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
		check.Status = CheckRunning
	case SamplePending:
		check.Status, check.ConsecutiveSuccessSince = CheckRunning, nil
	case SampleUnavailable:
		check.Status, check.ConsecutiveSuccessSince = CheckUnavailable, nil
	case SampleFailed:
		if check.FailureMode == FailureImmediate {
			check.Status = CheckFailed
		} else {
			check.Status, check.ConsecutiveSuccessSince = CheckRunning, nil
		}
	case SampleInvalid:
		check.Status, check.ConsecutiveSuccessSince = CheckInvalid, nil
	case SampleTimedOut:
		check.Status, check.ConsecutiveSuccessSince = CheckTimedOut, nil
	default:
		return ErrInvalidArgument
	}
	return nil
}

func negativeSample(sample Sample, check Check, reason string) Sample {
	if check.FailureMode == FailureImmediate {
		return withReason(sample, SampleFailed, reason)
	}
	return withReason(sample, SamplePending, reason)
}

func withReason(sample Sample, status SampleStatus, reason string) Sample {
	sample.Status, sample.ReasonCode = status, bound(reason, 128)
	return sample
}

func compare(value float64, comparison Comparison, threshold float64) bool {
	switch comparison {
	case CompareLT:
		return value < threshold
	case CompareLTE:
		return value <= threshold
	case CompareGT:
		return value > threshold
	case CompareGTE:
		return value >= threshold
	default:
		return false
	}
}

func sampleCount(check Check, observation Observation) int {
	switch check.SampleUnit {
	case "requests":
		if observation.Denominator > 0 {
			return int(observation.Denominator)
		}
	case "spans":
		if observation.Denominator > 0 {
			return int(observation.Denominator)
		}
	case "pods":
		if observation.SeriesCount > 0 {
			return observation.SeriesCount
		}
	}
	return observation.SampleCount
}

func (check Check) LookbackIsZero() bool { return check.Lookback <= 0 }

func nonEmptyReason(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
