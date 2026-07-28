package taskhandler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

var errApprovedEvidenceSuperseded = fmt.Errorf("%w: approved Evidence has a newer current-cycle superseder", asyncjob.ErrPolicyViolation)

type evidenceAuthorityQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type approvedEvidenceAuthority struct {
	ContentHash string
	Valid       bool
	Truncated   bool
	Superseded  bool
}

const approvedEvidenceAuthoritySQL = `
SELECT e.content_hash, e.valid, e.truncated,
       EXISTS (
           SELECT 1
           FROM evidence_supersessions s
           WHERE s.superseded_evidence_id = e.id
             AND s.incident_id = e.incident_id
             AND s.cycle_no = e.cycle_no
       ) AS superseded
FROM evidence_items e
WHERE e.public_id = ?
  AND e.incident_id = ?
  AND e.cycle_no = ?`

const approvedEvidenceAuthorityForUpdateSQL = approvedEvidenceAuthoritySQL + "\nFOR UPDATE"

// validateApprovedEvidenceCurrent is the shared approval-authority gate. A
// binding is usable only while its immutable content still matches and no
// Evidence in the same Incident cycle supersedes it.
func validateApprovedEvidenceCurrent(ctx context.Context, queryer evidenceAuthorityQueryer, incidentID, cycleNo uint64, bindings []remediation.EvidenceBinding) error {
	return validateApprovedEvidenceAuthorityQuery(ctx, queryer, incidentID, cycleNo, bindings, false)
}

func validateApprovedEvidenceCurrentForUpdate(ctx context.Context, queryer evidenceAuthorityQueryer, incidentID, cycleNo uint64, bindings []remediation.EvidenceBinding) error {
	return validateApprovedEvidenceAuthorityQuery(ctx, queryer, incidentID, cycleNo, bindings, true)
}

func validateApprovedEvidenceAuthorityQuery(ctx context.Context, queryer evidenceAuthorityQueryer, incidentID, cycleNo uint64, bindings []remediation.EvidenceBinding, lock bool) error {
	if queryer == nil || incidentID == 0 || cycleNo == 0 || len(bindings) == 0 {
		return fmt.Errorf("%w: approved Evidence bindings are incomplete", asyncjob.ErrPolicyViolation)
	}
	ordered := append([]remediation.EvidenceBinding(nil), bindings...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].ID < ordered[right].ID })
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range ordered {
		id := strings.TrimSpace(binding.ID)
		contentHash := strings.TrimSpace(binding.ContentHash)
		if id == "" || len(contentHash) != 64 {
			return fmt.Errorf("%w: approved Evidence binding is malformed", asyncjob.ErrPolicyViolation)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("%w: approved Evidence binding is duplicated", asyncjob.ErrPolicyViolation)
		}
		seen[id] = struct{}{}

		var authority approvedEvidenceAuthority
		query := approvedEvidenceAuthoritySQL
		if lock {
			query = approvedEvidenceAuthorityForUpdateSQL
		}
		err := queryer.QueryRowContext(ctx, query, id, incidentID, cycleNo).Scan(
			&authority.ContentHash, &authority.Valid, &authority.Truncated, &authority.Superseded,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: approved Evidence is absent from the current Incident cycle", asyncjob.ErrPolicyViolation)
		}
		if err != nil {
			return err
		}
		if err := validateApprovedEvidenceAuthority(binding, authority); err != nil {
			return err
		}
	}
	return nil
}

func validateApprovedEvidenceAuthority(binding remediation.EvidenceBinding, authority approvedEvidenceAuthority) error {
	if authority.Superseded {
		return errApprovedEvidenceSuperseded
	}
	if authority.ContentHash != binding.ContentHash || !authority.Valid || authority.Truncated {
		return fmt.Errorf("%w: approved Evidence is stale or unusable", asyncjob.ErrPolicyViolation)
	}
	return nil
}
