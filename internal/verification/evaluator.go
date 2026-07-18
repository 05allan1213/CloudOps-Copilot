package verification

import (
	"encoding/json"
	"math"
	"time"
)

// EvaluateObservation is deterministic: provider descriptions never influence
// the verdict, and absence is successful only for the explicitly trusted
// log_error_absent check.
func EvaluateObservation(check Check, observation Observation, now time.Time) Sample {
	bounded, err := json.Marshal(observation)
	if err != nil || len(bounded) > 16*1024 {
		return Sample{Status: SampleInvalid, Observed: json.RawMessage(`{"status":"malformed"}`), ReasonCode: "malformed_observation"}
	}
	sample := Sample{Observed: bounded, SourceReference: observation.SourceReference}
	switch observation.Status {
	case ObservationUnavailable:
		sample.Status, sample.ReasonCode = SampleUnavailable, nonEmptyReason(observation.ReasonCode, "provider_unavailable")
		return sample
	case ObservationMalformed:
		sample.Status, sample.ReasonCode = SampleInvalid, "malformed_observation"
		return sample
	case ObservationNoData:
		if check.Type == CheckLogErrorAbsent && check.Comparison == CompareAbsent {
			sample.Status = SamplePassed
		} else {
			sample.Status, sample.ReasonCode = SampleUnavailable, "no_data"
		}
		return sample
	case ObservationAvailable:
	default:
		sample.Status, sample.ReasonCode = SampleInvalid, "malformed_observation"
		return sample
	}
	if math.IsNaN(observation.Value) || math.IsInf(observation.Value, 0) || observation.SampleCount <= 0 || observation.SampledAt.IsZero() || now.UTC().Sub(observation.SampledAt.UTC()) > check.Lookback || observation.SampledAt.After(now.UTC().Add(time.Second)) {
		sample.Status, sample.ReasonCode = SampleInvalid, "invalid_or_stale_sample"
		return sample
	}
	if check.Comparison == CompareAbsent {
		if observation.MatchedCount == 0 {
			sample.Status = SamplePassed
		} else {
			sample.Status, sample.ReasonCode = SamplePending, "matches_present"
		}
		return sample
	}
	passed := false
	switch check.Comparison {
	case CompareLT:
		passed = observation.Value < check.Threshold
	case CompareLTE:
		passed = observation.Value <= check.Threshold
	case CompareGT:
		passed = observation.Value > check.Threshold
	case CompareGTE:
		passed = observation.Value >= check.Threshold
	default:
		sample.Status, sample.ReasonCode = SampleInvalid, "invalid_comparison"
		return sample
	}
	if passed {
		sample.Status = SamplePassed
	} else {
		sample.Status, sample.ReasonCode = SamplePending, "threshold_not_satisfied"
	}
	return sample
}

func nonEmptyReason(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
