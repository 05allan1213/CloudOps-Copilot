package cutover

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const ChangeRequestConverterVersion = "change-request/v2"

type LegacyExternalArtifact struct {
	Repository      string
	PullRequest     int64
	URL             string
	BaseRevision    string
	HeadBranch      string
	HeadRevision    string
	State           string
	MergedCommitSHA string
}

type ReconciledPullRequest struct {
	Repository      string
	PullRequest     int64
	URL             string
	BaseRevision    string
	HeadBranch      string
	HeadRevision    string
	State           string
	Merged          bool
	MergedCommitSHA string
}

type LegacyChangeInput struct {
	SubjectID      uint64
	SubjectVersion uint64
	IncidentID     uint64
	CycleNo        uint64
	SourceStatus   string
	HasLegacyPlan  bool
	HasApproval    bool
	Artifact       LegacyExternalArtifact
	Reconciled     *ReconciledPullRequest
}

type ChangeConversionClass string

const (
	ChangeObserveExistingPR ChangeConversionClass = "observe-existing-pr"
	ChangePartialWrite      ChangeConversionClass = "partial-write"
	ChangeApprovalOnly      ChangeConversionClass = "approval-only"
	ChangeNoExternalWrite   ChangeConversionClass = "no-external-write"
	ChangeAmbiguousExternal ChangeConversionClass = "ambiguous-external-state"
)

type LegacyChangeConversion struct {
	ConverterVersion string
	Class            ChangeConversionClass
	Compatible       bool
	CreateObserve    bool
	ReasonCode       string
	InputHash        string
	OutputHash       string
}

func ConvertLegacyChange(input LegacyChangeInput) LegacyChangeConversion {
	result := LegacyChangeConversion{ConverterVersion: ChangeRequestConverterVersion}
	result.InputHash = canonicalHashFields(
		ChangeRequestConverterVersion, fmt.Sprint(input.SubjectID), fmt.Sprint(input.SubjectVersion),
		fmt.Sprint(input.IncidentID), fmt.Sprint(input.CycleNo), strings.ToLower(strings.TrimSpace(input.SourceStatus)),
		fmt.Sprint(input.HasLegacyPlan), fmt.Sprint(input.HasApproval),
		input.Artifact.Repository, fmt.Sprint(input.Artifact.PullRequest), input.Artifact.URL,
		input.Artifact.BaseRevision, input.Artifact.HeadBranch, input.Artifact.HeadRevision,
		input.Artifact.State, input.Artifact.MergedCommitSHA,
		canonicalComponent(input.Reconciled),
	)
	finish := func(class ChangeConversionClass, compatible, observe bool, reason string) LegacyChangeConversion {
		result.Class, result.Compatible, result.CreateObserve, result.ReasonCode = class, compatible, observe, reason
		result.OutputHash = canonicalHashFields(ChangeRequestConverterVersion, string(class), fmt.Sprint(compatible), fmt.Sprint(observe), reason)
		return result
	}
	if input.SubjectID == 0 || input.SubjectVersion == 0 || input.IncidentID == 0 || input.CycleNo == 0 {
		return finish(ChangeAmbiguousExternal, false, false, "legacy_change_identity_invalid")
	}
	artifact := input.Artifact
	hasPR := artifact.PullRequest > 0 || strings.TrimSpace(artifact.URL) != "" || strings.TrimSpace(artifact.State) != "" || strings.TrimSpace(artifact.MergedCommitSHA) != ""
	hasPartial := strings.TrimSpace(artifact.HeadBranch) != "" || strings.TrimSpace(artifact.HeadRevision) != ""
	if hasPR {
		if input.Reconciled == nil {
			return finish(ChangeAmbiguousExternal, false, false, "legacy_external_state_ambiguous")
		}
		if err := validateReconciledPullRequest(artifact, *input.Reconciled); err != nil {
			return finish(ChangeAmbiguousExternal, false, false, "legacy_external_identity_mismatch")
		}
		if !legacyChangeStatusActive(input.SourceStatus) {
			return finish(ChangeObserveExistingPR, true, false, "legacy_pr_terminal_archived")
		}
		return finish(ChangeObserveExistingPR, true, true, "legacy_pr_read_only_observe")
	}
	if hasPartial {
		return finish(ChangePartialWrite, false, false, "legacy_partial_external_write")
	}
	if input.HasApproval {
		return finish(ChangeApprovalOnly, false, false, "legacy_approval_incomplete")
	}
	return finish(ChangeNoExternalWrite, false, false, "legacy_no_external_write")
}

func legacyChangeStatusActive(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "delivering", "pr_created", "ci_pending", "ci_passed", "merge_pending",
		"merged", "argocd_pending", "syncing", "synced", "rollout_pending":
		return true
	default:
		return false
	}
}

func validateReconciledPullRequest(source LegacyExternalArtifact, observed ReconciledPullRequest) error {
	state := strings.ToLower(strings.TrimSpace(observed.State))
	if state != "open" && state != "closed" && state != "merged" {
		return errors.New("external PR state is not authoritative")
	}
	if source.Repository == "" || source.PullRequest <= 0 || source.URL == "" || source.BaseRevision == "" || source.HeadBranch == "" || source.HeadRevision == "" {
		return errors.New("source PR identity is incomplete")
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("source PR URL is invalid")
	}
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) < 4 || pathParts[len(pathParts)-2] != "pull" ||
		!strings.EqualFold(strings.Join(pathParts[:len(pathParts)-2], "/"), source.Repository) ||
		pathParts[len(pathParts)-1] != fmt.Sprint(source.PullRequest) {
		return errors.New("source PR URL does not bind repository and number")
	}
	if observed.Repository != source.Repository || observed.PullRequest != source.PullRequest || observed.URL != source.URL ||
		!strings.EqualFold(observed.BaseRevision, source.BaseRevision) || observed.HeadBranch != source.HeadBranch ||
		!strings.EqualFold(observed.HeadRevision, source.HeadRevision) {
		return errors.New("external PR identity differs from source archive")
	}
	if source.State != "" {
		sourceState := strings.ToLower(strings.TrimSpace(source.State))
		validTransition := sourceState == state ||
			sourceState == "open" && (state == "closed" || state == "merged") ||
			sourceState == "closed" && state == "merged" && observed.Merged ||
			sourceState == "merged" && state == "closed" && observed.Merged
		if !validTransition {
			return errors.New("external PR state differs from source archive")
		}
	}
	if !exactRevision.MatchString(strings.ToLower(observed.BaseRevision)) || !exactRevision.MatchString(strings.ToLower(observed.HeadRevision)) {
		return errors.New("external PR base/head revision is invalid")
	}
	if observed.Merged || state == "merged" {
		if !exactRevision.MatchString(strings.ToLower(observed.MergedCommitSHA)) ||
			(source.MergedCommitSHA != "" && !strings.EqualFold(source.MergedCommitSHA, observed.MergedCommitSHA)) {
			return errors.New("external PR merge identity is invalid")
		}
	} else if observed.MergedCommitSHA != "" {
		return errors.New("unmerged PR contains a merge commit")
	}
	return nil
}
