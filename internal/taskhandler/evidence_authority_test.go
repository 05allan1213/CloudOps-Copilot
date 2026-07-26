package taskhandler

import (
	"errors"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
)

func TestApprovedEvidenceAuthorityRejectsSuperseder(t *testing.T) {
	binding := remediation.EvidenceBinding{ID: "33333333-3333-4333-8333-333333333333", ContentHash: strings.Repeat("a", 64)}
	err := validateApprovedEvidenceAuthority(binding, approvedEvidenceAuthority{
		ContentHash: binding.ContentHash,
		Valid:       true,
		Superseded:  true,
	})
	if !errors.Is(err, errApprovedEvidenceSuperseded) || !errors.Is(err, asyncjob.ErrPolicyViolation) {
		t.Fatalf("superseded Evidence error=%v", err)
	}
}

func TestApprovedEvidenceAuthorityRequiresExactUsableContent(t *testing.T) {
	binding := remediation.EvidenceBinding{ID: "33333333-3333-4333-8333-333333333333", ContentHash: strings.Repeat("a", 64)}
	for name, authority := range map[string]approvedEvidenceAuthority{
		"hash drift": {ContentHash: strings.Repeat("b", 64), Valid: true},
		"invalid":    {ContentHash: binding.ContentHash},
		"truncated":  {ContentHash: binding.ContentHash, Valid: true, Truncated: true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateApprovedEvidenceAuthority(binding, authority); !errors.Is(err, asyncjob.ErrPolicyViolation) {
				t.Fatalf("authority error=%v", err)
			}
		})
	}
	if err := validateApprovedEvidenceAuthority(binding, approvedEvidenceAuthority{ContentHash: binding.ContentHash, Valid: true}); err != nil {
		t.Fatalf("current Evidence rejected: %v", err)
	}
}

func TestApprovedEvidenceAuthorityQueryIsCurrentCycleScoped(t *testing.T) {
	for _, required := range []string{
		"FROM evidence_supersessions s",
		"s.superseded_evidence_id = e.id",
		"s.incident_id = e.incident_id",
		"s.cycle_no = e.cycle_no",
		"e.incident_id = ?",
		"e.cycle_no = ?",
	} {
		if !strings.Contains(approvedEvidenceAuthoritySQL, required) {
			t.Fatalf("authority query is missing %q", required)
		}
	}
	if strings.Contains(approvedEvidenceAuthoritySQL, "domain_schema_version") {
		t.Fatal("authority query must not depend on a product-generation discriminator")
	}
	if !strings.HasSuffix(approvedEvidenceAuthorityForUpdateSQL, "FOR UPDATE") {
		t.Fatal("transactional authority query does not lock the referenced Evidence")
	}
}
