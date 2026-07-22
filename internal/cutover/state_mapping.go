package cutover

import (
	"fmt"
	"strings"
)

const IncidentStateConverterVersion = "incident-state/v2"

type LegacyIncidentStateInput struct {
	IncidentID                    uint64
	IncidentPublicID              string
	SourceStatus                  string
	SourceVersion                 uint64
	CycleNo                       uint64
	ActiveVerification            bool
	CompatibleActiveVerification  bool
	CompatiblePassingVerification bool
	VerificationTriggerValid      bool
	VerificationRevisionsValid    bool
	ObservedExternalChange        bool
	PlanApprovalWithoutWrite      bool
}

type IncidentStateConversion struct {
	ConverterVersion   string
	SourceStatus       string
	TargetLegacyStatus string
	TargetV3Status     string
	NeedsAttention     bool
	ReasonCode         string
	InputHash          string
	OutputHash         string
	PreserveResolved   bool
}

func ConvertIncidentState(input LegacyIncidentStateInput) IncidentStateConversion {
	source := strings.ToUpper(strings.TrimSpace(input.SourceStatus))
	result := IncidentStateConversion{
		ConverterVersion: IncidentStateConverterVersion,
		SourceStatus:     source,
	}
	result.InputHash = canonicalHashFields(
		IncidentStateConverterVersion, fmt.Sprint(input.IncidentID), input.IncidentPublicID, source,
		fmt.Sprint(input.SourceVersion), fmt.Sprint(input.CycleNo), fmt.Sprint(input.ActiveVerification),
		fmt.Sprint(input.CompatibleActiveVerification), fmt.Sprint(input.CompatiblePassingVerification),
		fmt.Sprint(input.VerificationTriggerValid), fmt.Sprint(input.VerificationRevisionsValid),
		fmt.Sprint(input.ObservedExternalChange), fmt.Sprint(input.PlanApprovalWithoutWrite),
	)
	set := func(legacy, v3, reason string) IncidentStateConversion {
		result.TargetLegacyStatus, result.TargetV3Status, result.ReasonCode = legacy, v3, reason
		result.OutputHash = canonicalHashFields(IncidentStateConverterVersion, source, legacy, v3, reason, fmt.Sprint(result.NeedsAttention))
		return result
	}
	if input.IncidentID == 0 || input.IncidentPublicID == "" || input.SourceVersion == 0 || input.CycleNo == 0 {
		result.NeedsAttention = true
		return set("DIAGNOSING", "investigating", "legacy_state_identity_invalid")
	}
	switch source {
	case "DETECTED":
		result.NeedsAttention = false
		return set("DETECTED", "detected", "legacy_detected_migrated")
	case "CORRELATING", "DIAGNOSING", "DIAGNOSIS_COMPLETED", "PLANNING_REMEDIATION":
		result.NeedsAttention = false
		return set("DIAGNOSING", "investigating", "legacy_investigation_migrated")
	case "AWAITING_APPROVAL":
		if input.PlanApprovalWithoutWrite {
			result.NeedsAttention = true
			return set("DIAGNOSING", "investigating", "legacy_approval_incomplete")
		}
		result.NeedsAttention = true
		return set("AWAITING_APPROVAL", "awaiting_approval", "legacy_approval_non_authoritative")
	case "APPLYING_CHANGE":
		if input.ObservedExternalChange {
			result.NeedsAttention = false
			return set("APPLYING_CHANGE", "delivering", "legacy_delivery_reconcile_required")
		}
		result.NeedsAttention = true
		return set("DIAGNOSING", "investigating", "legacy_change_not_observed")
	case "VERIFYING":
		if input.ActiveVerification && input.CompatibleActiveVerification {
			result.NeedsAttention = false
			return set("VERIFYING", "verifying", "legacy_verification_migrated")
		}
		result.NeedsAttention = true
		return set("DIAGNOSING", "investigating", "legacy_verification_incompatible")
	case "RESOLVED":
		if input.CompatiblePassingVerification && input.VerificationTriggerValid && input.VerificationRevisionsValid {
			result.NeedsAttention = false
			result.PreserveResolved = true
			return set("RESOLVED", "resolved", "legacy_resolution_verified")
		}
		result.NeedsAttention = true
		return set("DIAGNOSING", "investigating", "legacy_resolution_unverified")
	case "CLOSED_NO_ACTION":
		result.NeedsAttention = false
		return set("CLOSED_NO_ACTION", "closed", "legacy_closed_migrated")
	case "FAILED":
		result.NeedsAttention = true
		switch {
		case input.ActiveVerification:
			return set("VERIFYING", "verifying", "legacy_failed_blocked")
		case input.ObservedExternalChange:
			return set("APPLYING_CHANGE", "delivering", "legacy_failed_blocked")
		case input.PlanApprovalWithoutWrite:
			return set("DIAGNOSING", "investigating", "legacy_approval_incomplete")
		default:
			return set("DIAGNOSING", "investigating", "legacy_failed_blocked")
		}
	default:
		result.NeedsAttention = true
		return set("DIAGNOSING", "investigating", "legacy_status_unknown")
	}
}
