package cutover

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

const VerificationConverterVersion = "verification-profile/v2"

var exactRevision = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

type LegacyVerificationCheck struct {
	Type                    verification.CheckType
	Subject                 verification.Subject
	Expected                json.RawMessage
	ProfileID               string
	TemplateID              string
	TemplateVersion         string
	SourceIdentity          string
	Comparison              verification.Comparison
	Threshold               float64
	InitialDelay            time.Duration
	Lookback                time.Duration
	PollInterval            time.Duration
	Timeout                 time.Duration
	StabilityWindow         time.Duration
	MinSamples              int
	SampleUnit              string
	FailureMode             verification.FailureMode
	Required                bool
	Status                  verification.CheckStatus
	ConsecutiveSuccessSince *time.Time
	Observed                json.RawMessage
	LastCheckedAt           *time.Time
	PassedAt                *time.Time
}

type LegacyVerificationSample struct {
	CheckType   verification.CheckType
	Sequence    uint64
	Status      verification.SampleStatus
	SampleUnit  string
	Count       int
	WindowFrom  *time.Time
	WindowTo    *time.Time
	SampledAt   time.Time
	ContentHash string
	Observed    json.RawMessage
}

type VerificationConversionInput struct {
	SourceSchemaVersion     uint32
	TargetSchemaVersion     uint32
	RunPublicID             string
	IncidentPublicID        string
	OwnershipValid          bool
	CycleNo                 uint64
	IncidentVersion         uint64
	RunVersion              uint64
	Attempt                 uint64
	RunStatus               verification.RunStatus
	TriggerType             string
	TargetRevision          string
	SourceRevision          string
	ImageDigest             string
	GitOpsRevision          string
	ProfileVersion          uint64
	ProfileHash             string
	PlanJSON                json.RawMessage
	Checks                  []LegacyVerificationCheck
	Samples                 []LegacyVerificationSample
	CommonWindow            time.Duration
	CommonSuccessSince      *time.Time
	CommonWindowCompletedAt *time.Time
}

type VerificationConversion struct {
	ConverterVersion string
	Compatible       bool
	ReasonCode       string
	InputHash        string
	OutputHash       string
	Plan             verification.Plan
}

func ConvertVerification(input VerificationConversionInput) VerificationConversion {
	result := VerificationConversion{ConverterVersion: VerificationConverterVersion}
	result.InputHash = canonicalHashFields(
		VerificationConverterVersion, fmt.Sprint(input.SourceSchemaVersion), fmt.Sprint(input.TargetSchemaVersion),
		input.RunPublicID, input.IncidentPublicID, fmt.Sprint(input.OwnershipValid), fmt.Sprint(input.CycleNo), fmt.Sprint(input.IncidentVersion),
		fmt.Sprint(input.RunVersion), fmt.Sprint(input.Attempt), string(input.RunStatus), input.TriggerType,
		input.TargetRevision, input.SourceRevision, input.ImageDigest, input.GitOpsRevision,
		fmt.Sprint(input.ProfileVersion), input.ProfileHash, string(input.PlanJSON),
		canonicalComponent(input.Checks), canonicalComponent(input.Samples), fmt.Sprint(input.CommonWindow),
		canonicalComponent(input.CommonSuccessSince), canonicalComponent(input.CommonWindowCompletedAt),
	)
	fail := func(code string) VerificationConversion {
		result.ReasonCode = code
		result.OutputHash = canonicalHashFields(VerificationConverterVersion, "failed", code, result.InputHash)
		return result
	}
	if input.SourceSchemaVersion != 1 || input.TargetSchemaVersion != 3 {
		return fail("verification_schema_version_unsupported")
	}
	if input.RunPublicID == "" || input.IncidentPublicID == "" || !input.OwnershipValid || input.CycleNo == 0 || input.IncidentVersion == 0 || input.RunVersion == 0 || input.Attempt == 0 {
		return fail("verification_ownership_or_version_invalid")
	}
	switch input.RunStatus {
	case verification.RunPending, verification.RunRunning, verification.RunPassed, verification.RunFailed,
		verification.RunTimedOut, verification.RunInconclusive, verification.RunCancelled:
	default:
		return fail("verification_run_status_invalid")
	}
	if input.TriggerType != "post_delivery" && input.TriggerType != "no_change_signal" {
		return fail("verification_trigger_invalid")
	}
	if !exactRevision.MatchString(strings.ToLower(input.TargetRevision)) || !exactRevision.MatchString(strings.ToLower(input.SourceRevision)) ||
		!imageDigestPattern.MatchString(strings.ToLower(input.ImageDigest)) || !exactRevision.MatchString(strings.ToLower(input.GitOpsRevision)) {
		return fail("verification_revision_missing_or_conflicting")
	}
	if input.CommonWindow != verification.V3CommonStabilityWindow {
		return fail("verification_common_window_invalid")
	}
	if len(input.PlanJSON) == 0 || len(input.PlanJSON) > 16*1024 || !json.Valid(input.PlanJSON) {
		return fail("verification_profile_invalid")
	}
	if strings.Contains(strings.ToLower(string(input.PlanJSON)), "loki") {
		return fail("legacy_loki_profile_incompatible")
	}
	var plan verification.Plan
	if err := strictJSONDecode(input.PlanJSON, &plan); err != nil {
		return fail("verification_profile_fields_invalid")
	}
	if input.TriggerType == "no_change_signal" && plan.TriggerType == "no_change_signal" {
		plan.TriggerType = "no_change"
	}
	if input.TriggerType == "post_delivery" && plan.TriggerType != "post_delivery" {
		return fail("verification_trigger_profile_conflict")
	}
	if input.TriggerType == "no_change_signal" && plan.TriggerType != "no_change" {
		return fail("verification_trigger_profile_conflict")
	}
	if plan.TargetRevision != strings.ToLower(input.TargetRevision) || plan.SourceRevision != strings.ToLower(input.SourceRevision) ||
		plan.ImageDigest != strings.ToLower(input.ImageDigest) || plan.GitOpsRevision != strings.ToLower(input.GitOpsRevision) {
		return fail("verification_revision_missing_or_conflicting")
	}
	if plan.ProfileVersion != int(input.ProfileVersion) || plan.ProfileHash != input.ProfileHash || !isSHA256(input.ProfileHash) {
		return fail("verification_profile_hash_mismatch")
	}
	if err := verification.ValidateV3Plan(plan); err != nil {
		return fail("verification_profile_semantics_invalid")
	}
	if err := validateVerificationChecks(plan, input.Checks); err != nil {
		return fail(err.Error())
	}
	if err := validateVerificationSamples(plan, input.Samples); err != nil {
		return fail(err.Error())
	}
	if (input.CommonSuccessSince == nil) != (input.CommonWindowCompletedAt == nil) {
		return fail("verification_common_window_invalid")
	}
	if input.CommonSuccessSince != nil && input.CommonWindowCompletedAt.Before(*input.CommonSuccessSince) {
		return fail("verification_common_window_invalid")
	}
	if input.RunStatus == verification.RunPassed {
		if err := validatePassingVerification(input); err != nil {
			return fail(err.Error())
		}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return fail("verification_profile_encode_failed")
	}
	canonicalPlan, err := canonicalJSON(encoded)
	if err != nil {
		return fail("verification_profile_encode_failed")
	}
	result.Compatible = true
	result.ReasonCode = "verification_converted"
	result.OutputHash = canonicalHashFields(
		VerificationConverterVersion,
		string(canonicalPlan),
		canonicalComponent(input.Checks),
		canonicalComponent(input.Samples),
		fmt.Sprint(input.CommonWindow),
		canonicalComponent(input.CommonSuccessSince),
		canonicalComponent(input.CommonWindowCompletedAt),
		string(input.RunStatus),
		fmt.Sprint(input.Attempt),
	)
	result.Plan = plan
	return result
}

func validateVerificationChecks(plan verification.Plan, checks []LegacyVerificationCheck) error {
	if len(checks) != len(plan.Checks) {
		return errors.New("verification_check_count_mismatch")
	}
	byType := make(map[verification.CheckType]LegacyVerificationCheck, len(checks))
	for _, check := range checks {
		switch check.Status {
		case verification.CheckPending, verification.CheckRunning, verification.CheckPassed, verification.CheckFailed,
			verification.CheckTimedOut, verification.CheckUnavailable, verification.CheckInvalid, verification.CheckCancelled:
		default:
			return errors.New("verification_check_status_invalid")
		}
		if strings.Contains(strings.ToLower(check.SourceIdentity+" "+check.TemplateID), "loki") {
			return errors.New("legacy_loki_profile_incompatible")
		}
		if _, duplicate := byType[check.Type]; duplicate {
			return errors.New("verification_check_duplicate")
		}
		byType[check.Type] = check
	}
	for _, expected := range plan.Checks {
		actual, ok := byType[expected.Type]
		if !ok || actual.ProfileID != expected.ProfileID || actual.TemplateID != expected.TemplateID ||
			actual.TemplateVersion != expected.TemplateVersion || actual.SourceIdentity != expected.SourceIdentity ||
			actual.Comparison != expected.Comparison || actual.Threshold != expected.Threshold ||
			actual.InitialDelay != expected.InitialDelay || actual.Lookback != expected.Lookback ||
			actual.PollInterval != expected.PollInterval || actual.Timeout != expected.Timeout ||
			actual.StabilityWindow != expected.StabilityWindow || actual.MinSamples != expected.MinSamples ||
			actual.SampleUnit != expected.SampleUnit || actual.FailureMode != expected.FailureMode || actual.Required != expected.Required ||
			canonicalComponent(actual.Subject) != canonicalComponent(expected.Subject) ||
			canonicalComponent(json.RawMessage(actual.Expected)) != canonicalComponent(json.RawMessage(expected.Expected)) {
			return errors.New("verification_check_semantics_invalid")
		}
		if actual.MinSamples <= 0 || strings.TrimSpace(actual.SampleUnit) == "" {
			return errors.New("verification_min_samples_invalid")
		}
		if actual.StabilityWindow != verification.V3CommonStabilityWindow {
			return errors.New("verification_common_window_invalid")
		}
	}
	return nil
}

func validateVerificationSamples(plan verification.Plan, samples []LegacyVerificationSample) error {
	checks := make(map[verification.CheckType]verification.CheckSpec, len(plan.Checks))
	for _, check := range plan.Checks {
		checks[check.Type] = check
	}
	sequences := make(map[verification.CheckType]map[uint64]struct{})
	for _, sample := range samples {
		check, ok := checks[sample.CheckType]
		if !ok || sample.Sequence == 0 || sample.SampledAt.IsZero() || !isSHA256(sample.ContentHash) {
			return errors.New("verification_sample_identity_invalid")
		}
		if sample.SampleUnit != check.SampleUnit || sample.Count < 0 {
			return errors.New("verification_sample_unit_invalid")
		}
		switch sample.Status {
		case verification.SamplePassed, verification.SampleFailed, verification.SamplePending,
			verification.SampleUnavailable, verification.SampleInvalid, verification.SampleTimedOut:
		default:
			return errors.New("verification_sample_status_invalid")
		}
		if sample.Status == verification.SamplePassed && sample.Count < check.MinSamples {
			return errors.New("verification_min_samples_invalid")
		}
		if (sample.WindowFrom == nil) != (sample.WindowTo == nil) || (sample.WindowFrom != nil && (sample.WindowTo.Before(*sample.WindowFrom) || sample.SampledAt.Before(*sample.WindowTo))) {
			return errors.New("verification_sample_window_invalid")
		}
		if sequences[sample.CheckType] == nil {
			sequences[sample.CheckType] = map[uint64]struct{}{}
		}
		if _, duplicate := sequences[sample.CheckType][sample.Sequence]; duplicate {
			return errors.New("verification_sample_duplicate")
		}
		sequences[sample.CheckType][sample.Sequence] = struct{}{}
	}
	for checkType, values := range sequences {
		ordered := make([]uint64, 0, len(values))
		for sequence := range values {
			ordered = append(ordered, sequence)
		}
		slices.Sort(ordered)
		for index, sequence := range ordered {
			if sequence != uint64(index+1) {
				return fmt.Errorf("verification_sample_sequence_gap_%s", checkType)
			}
		}
	}
	return nil
}

func validatePassingVerification(input VerificationConversionInput) error {
	if input.CommonSuccessSince == nil || input.CommonWindowCompletedAt == nil ||
		input.CommonWindowCompletedAt.Before(*input.CommonSuccessSince) ||
		input.CommonWindowCompletedAt.Sub(*input.CommonSuccessSince) < input.CommonWindow {
		return errors.New("verification_common_window_not_established")
	}
	latestStart := time.Time{}
	for _, check := range input.Checks {
		if check.Required && (check.Status != verification.CheckPassed || check.ConsecutiveSuccessSince == nil) {
			return errors.New("verification_required_check_not_passing")
		}
		if check.Required && check.ConsecutiveSuccessSince.After(latestStart) {
			latestStart = check.ConsecutiveSuccessSince.UTC()
		}
	}
	if latestStart.IsZero() || !input.CommonSuccessSince.Equal(latestStart) {
		return errors.New("verification_common_window_not_established")
	}
	byType := make(map[verification.CheckType][]LegacyVerificationSample)
	for _, sample := range input.Samples {
		byType[sample.CheckType] = append(byType[sample.CheckType], sample)
	}
	for _, check := range input.Checks {
		if !check.Required {
			continue
		}
		passing := 0
		for _, sample := range byType[check.Type] {
			if sample.Status == verification.SamplePassed && sample.Count >= check.MinSamples {
				passing++
			}
		}
		if passing == 0 {
			return errors.New("verification_min_samples_invalid")
		}
	}
	return nil
}
