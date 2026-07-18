package verification

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const maxObservabilityChecks = 7

type Template struct {
	ID               string        `json:"id"`
	Type             CheckType     `json:"type"`
	Required         bool          `json:"required"`
	Comparison       Comparison    `json:"comparison"`
	Threshold        float64       `json:"threshold"`
	Lookback         time.Duration `json:"-"`
	LookbackSeconds  int64         `json:"lookback_seconds"`
	Timeout          time.Duration `json:"-"`
	TimeoutSeconds   int64         `json:"timeout_seconds"`
	Stability        time.Duration `json:"-"`
	StabilitySeconds int64         `json:"stability_seconds"`
}

type Profile struct {
	ID          string     `json:"id"`
	Service     string     `json:"service"`
	Environment string     `json:"environment"`
	Namespace   string     `json:"namespace"`
	Workload    string     `json:"workload"`
	Templates   []Template `json:"templates"`
}

type Profiles struct {
	Items []Profile `json:"profiles"`
}

func (p *Profiles) Validate() error {
	if p == nil || len(p.Items) > 100 {
		return fmt.Errorf("%w: profile count", ErrInvalidArgument)
	}
	seenProfiles := map[string]struct{}{}
	seenProfileIDs := map[string]struct{}{}
	for pi := range p.Items {
		profile := &p.Items[pi]
		profile.ID = strings.TrimSpace(profile.ID)
		profile.Service = strings.TrimSpace(profile.Service)
		profile.Environment = strings.TrimSpace(profile.Environment)
		profile.Namespace = strings.TrimSpace(profile.Namespace)
		profile.Workload = strings.TrimSpace(profile.Workload)
		if profile.ID == "" || profile.Service == "" || profile.Environment == "" || profile.Namespace == "" || profile.Workload == "" || len(profile.Templates) == 0 || len(profile.Templates) > maxObservabilityChecks {
			return fmt.Errorf("%w: profile identity", ErrInvalidArgument)
		}
		if _, duplicate := seenProfileIDs[profile.ID]; duplicate {
			return fmt.Errorf("%w: duplicate profile id", ErrInvalidArgument)
		}
		seenProfileIDs[profile.ID] = struct{}{}
		identity := strings.ToLower(profile.Environment + "/" + profile.Namespace + "/" + profile.Service + "/" + profile.Workload)
		if _, duplicate := seenProfiles[identity]; duplicate {
			return fmt.Errorf("%w: duplicate profile", ErrInvalidArgument)
		}
		seenProfiles[identity] = struct{}{}
		seenTemplates := map[string]struct{}{}
		seenTypes := map[CheckType]struct{}{}
		for ti := range profile.Templates {
			t := &profile.Templates[ti]
			t.ID = strings.TrimSpace(t.ID)
			t.Lookback = time.Duration(t.LookbackSeconds) * time.Second
			t.Timeout = time.Duration(t.TimeoutSeconds) * time.Second
			t.Stability = time.Duration(t.StabilitySeconds) * time.Second
			if t.ID == "" || !supportedObservabilityType(t.Type) || !comparisonAllowed(t.Type, t.Comparison) || math.IsNaN(t.Threshold) || math.IsInf(t.Threshold, 0) || t.Threshold < 0 || t.Threshold > 1_000_000 || t.Lookback < time.Minute || t.Lookback > 24*time.Hour || t.Timeout < time.Minute || t.Timeout > 24*time.Hour || t.Stability < time.Second || t.Stability > t.Timeout {
				return fmt.Errorf("%w: template %s", ErrInvalidArgument, t.ID)
			}
			if (t.Type == CheckMetricErrorRateBelow || t.Type == CheckMetricAvailabilityAbove || t.Type == CheckLogErrorRateBelow || t.Type == CheckTraceErrorRateBelow) && t.Threshold > 1 {
				return fmt.Errorf("%w: ratio threshold %s", ErrInvalidArgument, t.ID)
			}
			if _, ok := seenTemplates[t.ID]; ok {
				return fmt.Errorf("%w: duplicate template", ErrInvalidArgument)
			}
			if _, ok := seenTypes[t.Type]; ok {
				return fmt.Errorf("%w: duplicate check type", ErrInvalidArgument)
			}
			seenTemplates[t.ID], seenTypes[t.Type] = struct{}{}, struct{}{}
		}
		sort.Slice(profile.Templates, func(i, j int) bool {
			if profile.Templates[i].Type == profile.Templates[j].Type {
				return profile.Templates[i].ID < profile.Templates[j].ID
			}
			return profile.Templates[i].Type < profile.Templates[j].Type
		})
	}
	return nil
}

func (p Profiles) Match(subject Subject) (*Profile, error) {
	for i := range p.Items {
		candidate := &p.Items[i]
		if strings.EqualFold(candidate.Service, subject.Service) && strings.EqualFold(candidate.Environment, subject.Environment) && candidate.Namespace == subject.Namespace && candidate.Workload == subject.WorkloadName {
			return candidate, nil
		}
	}
	return nil, ErrNotAllowed
}

func supportedObservabilityType(t CheckType) bool {
	switch t {
	case CheckMetricErrorRateBelow, CheckMetricAvailabilityAbove, CheckMetricLatencyP95Below, CheckLogErrorAbsent, CheckLogErrorRateBelow, CheckTraceErrorRateBelow, CheckTraceLatencyP95Below:
		return true
	default:
		return false
	}
}

func comparisonAllowed(t CheckType, c Comparison) bool {
	if t == CheckLogErrorAbsent {
		return c == CompareAbsent
	}
	if t == CheckMetricAvailabilityAbove {
		return c == CompareGT || c == CompareGTE
	}
	return c == CompareLT || c == CompareLTE
}
