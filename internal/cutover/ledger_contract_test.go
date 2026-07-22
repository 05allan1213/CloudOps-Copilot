package cutover

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

func TestLedgerCompletionNeverPassesFailedCutoverInvariants(t *testing.T) {
	hash := canonicalHashSet([]string{strings.Repeat("a", 64)})
	valid := LedgerCompletion{SourceCount: 1, TargetCount: 1, SourceHash: hash, TargetHash: hash, RequireParity: true}
	if passed, reason := valid.passing(); !passed || reason != "" {
		t.Fatalf("valid ledger rejected: passed=%t reason=%s", passed, reason)
	}
	tests := []struct {
		name string
		edit func(*LedgerCompletion)
	}{
		{"count mismatch", func(value *LedgerCompletion) { value.TargetCount++ }},
		{"hash mismatch", func(value *LedgerCompletion) { value.TargetHash = strings.Repeat("b", 64) }},
		{"rejected row", func(value *LedgerCompletion) { value.RejectedCount = 1 }},
		{"conversion failure", func(value *LedgerCompletion) { value.ConversionFailures = 1 }},
		{"unknown external write", func(value *LedgerCompletion) { value.UnknownExternalWrites = 1 }},
		{"active legacy lease", func(value *LedgerCompletion) { value.ActiveLegacyLeases = 1 }},
		{"ingress writer", func(value *LedgerCompletion) { value.ObservedIngressWriters = 1 }},
		{"mutation writer", func(value *LedgerCompletion) { value.ObservedMutationWriters = 1 }},
		{"legacy worker", func(value *LedgerCompletion) { value.ObservedLegacyWorkers = 1 }},
		{"missing archive", func(value *LedgerCompletion) { value.MissingArchiveRows = 1 }},
		{"duplicate task", func(value *LedgerCompletion) { value.DuplicateTasks = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.edit(&value)
			if passed, _ := value.passing(); passed {
				t.Fatalf("ledger passed with %+v", value)
			}
		})
	}
}

func TestConversionTaskDedupeBindsEveryLegacyIdentityComponent(t *testing.T) {
	base := conversionTaskSpec{IncidentID: 1, CycleNo: 2, TaskType: asyncjob.TaskInvestigationAdvance,
		SubjectType: "agent_run", SubjectID: 3, Transition: "investigation.step", ExpectedSubjectVersion: 4,
		Payload: json.RawMessage(`{"mode":"decide"}`), LegacySubjectType: "agent_run", LegacySubjectID: 3,
		LegacySourceVersion: 4, ConverterVersion: AgentCheckpointConverterVersion,
		MigratedLegacy: true, MigratedLegacyContext: true}
	dedupe, logical := conversionTaskKeys(base)
	if !isSHA256(dedupe) || !isSHA256(logical) {
		t.Fatalf("invalid conversion task keys: %s %s", dedupe, logical)
	}
	mutations := []func(*conversionTaskSpec){
		func(value *conversionTaskSpec) { value.LegacySubjectType = "verification_run" },
		func(value *conversionTaskSpec) { value.LegacySubjectID++ },
		func(value *conversionTaskSpec) { value.LegacySourceVersion++ },
		func(value *conversionTaskSpec) { value.CycleNo++ },
		func(value *conversionTaskSpec) { value.Transition = "investigation.start" },
		func(value *conversionTaskSpec) { value.ConverterVersion = "agent-checkpoint/v3" },
	}
	for index, mutate := range mutations {
		changed := base
		mutate(&changed)
		changedDedupe, _ := conversionTaskKeys(changed)
		if changedDedupe == dedupe {
			t.Fatalf("dedupe mutation %d did not change the key", index)
		}
	}
}

func TestTerminalLegacyChildrenCannotCreateConversionTasks(t *testing.T) {
	for _, status := range []string{"COMPLETED", "FAILED", "CANCELLED"} {
		if legacyAgentStatusActive(status) {
			t.Fatalf("terminal AgentRun status %s was active", status)
		}
	}
	for _, status := range []string{"passed", "failed", "timed_out", "inconclusive", "cancelled"} {
		if legacyVerificationStatusActive(verification.RunStatus(status)) {
			t.Fatalf("terminal VerificationRun status %s was active", status)
		}
	}
	for _, status := range []string{"delivered", "failed", "delivery_cancelled", "pr_closed", "rollout_failed"} {
		if legacyChangeStatusActive(status) {
			t.Fatalf("terminal ChangeRequest status %s was active", status)
		}
	}
}

func TestOutboxRegistryFixturesAreVersionedAndFullRowHashIsSensitive(t *testing.T) {
	entries := OutboxRegistry()
	if len(entries) == 0 {
		t.Fatal("outbox registry is empty")
	}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		key := fmt.Sprintf("%s/%d", entry.EventType, entry.SchemaVersion)
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate outbox registry entry %s/v%d", entry.EventType, entry.SchemaVersion)
		}
		seen[key] = struct{}{}
		if entry.SchemaVersion == 0 || entry.AggregateType == "" || entry.ArchiveMapper == "" || !json.Valid(entry.Fixture) || !isSHA256(entry.FixtureHash) {
			t.Fatalf("invalid outbox registry entry: %+v", entry)
		}
		hash, _, err := canonicalHashJSON(entry.Fixture)
		if err != nil || hash != entry.FixtureHash {
			t.Fatalf("fixture hash drift for %s/v%d: %s/%s err=%v", entry.EventType, entry.SchemaVersion, hash, entry.FixtureHash, err)
		}
	}
	now := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	base := LegacyOutboxRow{ID: 1, EventID: "event-1", AggregateType: "incident", AggregateID: "incident-1",
		EventType: "incident.created", SchemaVersion: 1, Payload: json.RawMessage(`{"value":1}`), OccurredAt: now, CreatedAt: now}
	first, err := ValidateOutboxArchive(base, false)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Attempts = 1
	second, err := ValidateOutboxArchive(changed, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.RowHash == second.RowHash {
		t.Fatal("outbox row hash ignored attempts metadata")
	}
	changed = base
	changed.LastError = "delivery timeout"
	third, err := ValidateOutboxArchive(changed, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.RowHash == third.RowHash {
		t.Fatal("outbox row hash ignored last_error metadata")
	}
}

func TestPhase7ACategoryLedgerNamesExcludeOutboxDerivedTask(t *testing.T) {
	operations := []string{OutboxArchivedPublishedOperation, OutboxArchivedUnpublishedOperation,
		ExistingTargetTaskOperation, SubjectDerivedOperation, ConversionFailedOperation,
		AntiJoinSkippedOperation, TaskCreatedOperation}
	for _, operation := range operations {
		if strings.TrimSpace(operation) == "" || strings.Contains(operation, "outbox-derived-task") {
			t.Fatalf("invalid Phase 7A category operation %q", operation)
		}
	}
}

func TestValidatePhase7ACountsFailsEveryReleaseAInvariantClosed(t *testing.T) {
	base := map[string]uint64{
		"outbox_source": 2, "outbox_archived": 2, "outbox_archived_published": 1, "outbox_archived_unpublished": 1,
		"signals_archived": 1, "signals_backfilled": 1, "events_archived": 1, "events_backfilled": 1,
		"evidence_archived": 1, "evidence_backfilled": 1, "agent_steps_archived": 1, "agent_steps_backfilled": 1,
		"change_candidates_archived": 1, "change_candidates_backfilled": 1, "change_assessments_archived": 1,
		"required_conversion_subjects": 3, "latest_conversion_records": 3, "subject_derived": 3,
		"task_created": 1, "existing_target_task": 1, "anti_join_skipped": 1, "conversion_not_applicable": 0,
		"postmortems_source": 1, "postmortems_archived": 1,
	}
	request := PrepareRequest{PlanVersion: 7, SourceExactSHA: strings.Repeat("a", 40), BinaryImageDigest: "sha256:" + strings.Repeat("b", 64)}
	if err := validatePhase7ACounts(base, request); err != nil {
		t.Fatalf("valid Phase 7A counts rejected: %v", err)
	}
	mutations := map[string]func(map[string]uint64){
		"outbox parity":            func(value map[string]uint64) { value["outbox_archived"]++ },
		"backfill parity":          func(value map[string]uint64) { value["signals_backfilled"]-- },
		"assessment parity":        func(value map[string]uint64) { value["change_assessments_archived"]-- },
		"conversion parity":        func(value map[string]uint64) { value["latest_conversion_records"]-- },
		"anti join classification": func(value map[string]uint64) { value["anti_join_skipped"]-- },
		"postmortem parity":        func(value map[string]uint64) { value["postmortems_archived"]-- },
		"missing archive":          func(value map[string]uint64) { value["missing_archive_rows"] = 1 },
		"archive hash":             func(value map[string]uint64) { value["archive_hash_mismatches"] = 1 },
		"missing conversion":       func(value map[string]uint64) { value["missing_conversion_records"] = 1 },
		"conversion failure":       func(value map[string]uint64) { value["unsettled_conversion_failures"] = 1 },
		"duplicate task":           func(value map[string]uint64) { value["task_duplicates"] = 1 },
		"terminal child task":      func(value map[string]uint64) { value["terminal_child_tasks"] = 1 },
		"unknown external write":   func(value map[string]uint64) { value["unknown_external_writes"] = 1 },
		"active legacy lease":      func(value map[string]uint64) { value["active_legacy_leases"] = 1 },
		"running v3 task":          func(value map[string]uint64) { value["running_v3_tasks"] = 1 },
		"legacy row remaining":     func(value map[string]uint64) { value["legacy_rows_remaining"] = 1 },
		"external observation":     func(value map[string]uint64) { value["observed_ingress_writers"] = 1 },
		"premature cutover marker": func(value map[string]uint64) { value["marker_count"] = 1 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			counts := make(map[string]uint64, len(base))
			for key, value := range base {
				counts[key] = value
			}
			mutate(counts)
			if err := validatePhase7ACounts(counts, request); err == nil {
				t.Fatalf("invalid Phase 7A counts passed: %+v", counts)
			}
		})
	}
}

func TestPassedBackfillLedgerCannotMaskSourceDrift(t *testing.T) {
	identity := ReleaseIdentity{PlanVersion: 7, SourceExactSHA: strings.Repeat("a", 40),
		BinaryImageDigest: "sha256:" + strings.Repeat("b", 64), SourceSchema: 16, TargetSchema: 17}
	unit := backfillUnit{name: "evidence", sourceTable: "evidence_items", targetTable: "evidence_items+legacy_evidence_archive"}
	idMin, idMax := uint64(10), uint64(11)
	hashes := []string{strings.Repeat("c", 64), strings.Repeat("d", 64)}
	batch := LedgerBatch{Operation: BackfillOperationPrefix + unit.name, Stage: "backfill", Status: "passed", BatchNo: 1,
		SourceSchema: identity.SourceSchema, TargetSchema: identity.TargetSchema, SourceTable: unit.sourceTable,
		TargetTable: unit.targetTable, IDMin: &idMin, IDMax: &idMax, SourceCount: 2, TargetCount: 2,
		SourceHash: canonicalHashSet(hashes), TargetHash: canonicalHashSet(hashes), ConverterVersion: BackfillConverterVersion,
		ReleaseHash:    releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema),
		SourceExactSHA: identity.SourceExactSHA, ImageDigest: identity.BinaryImageDigest}
	if err := validatePassedBackfillBatch(batch, identity, unit, idMin, idMax, hashes); err != nil {
		t.Fatalf("valid passed ledger rejected: %v", err)
	}
	drifted := append([]string(nil), hashes...)
	drifted[1] = strings.Repeat("e", 64)
	if err := validatePassedBackfillBatch(batch, identity, unit, idMin, idMax, drifted); err == nil {
		t.Fatal("passed ledger masked changed source hash")
	}
}
