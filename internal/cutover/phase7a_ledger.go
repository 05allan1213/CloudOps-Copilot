package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	OutboxArchivedPublishedOperation   = "outbox-archived-published"
	OutboxArchivedUnpublishedOperation = "outbox-archived-unpublished"
	ExistingTargetTaskOperation        = "existing-target-task"
	SubjectDerivedOperation            = "subject-derived"
	ConversionFailedOperation          = "conversion-failed"
	AntiJoinSkippedOperation           = "anti-join-skipped"
	TaskCreatedOperation               = "task-created"
)

type phase7ALedgerIDs struct {
	Quiesce        string
	Reconciliation string
	ConverterAudit string
}

var phase7AFinalOperations = []string{
	QuiesceOperation,
	ReconciliationOperation,
	ConverterAuditOperation,
	OutboxArchivedPublishedOperation,
	OutboxArchivedUnpublishedOperation,
	SubjectDerivedOperation,
	ConversionFailedOperation,
	TaskCreatedOperation,
	ExistingTargetTaskOperation,
	AntiJoinSkippedOperation,
}

type persistedPhase7ALedger struct {
	publicID, operation, status, sha, digest         string
	sourceTable, targetTable, sourceHash, targetHash string
	converterVersion, releaseIdentityHash            string
	attempt, sourceSchema, targetSchema, batchNo     uint64
	sourceCount, targetCount                         uint64
	completed                                        sql.NullTime
}

type phase7ALedgerExpectation struct {
	sourceTable, targetTable string
	sourceCount, targetCount uint64
	sourceHash, targetHash   string
}

func existingPreparationV2(ctx context.Context, tx *sql.Tx, request PrepareRequest, version uint64) (PrepareReport, bool, error) {
	placeholders := make([]string, len(phase7AFinalOperations))
	args := make([]any, 0, len(phase7AFinalOperations)+1)
	args = append(args, request.PlanVersion)
	for index, operation := range phase7AFinalOperations {
		placeholders[index] = "?"
		args = append(args, operation)
	}
	rows, err := tx.QueryContext(ctx, `SELECT public_id,operation,status,source_exact_sha,binary_image_digest,attempt,
source_schema_version,target_schema_version,source_table,target_table,batch_no,source_count,target_count,
COALESCE(source_hash,''),COALESCE(target_hash,''),converter_version,COALESCE(release_identity_hash,''),completed_at
FROM migration_ledger
WHERE plan_version=? AND operation IN (`+strings.Join(placeholders, ",")+`)
ORDER BY operation,attempt FOR UPDATE`, args...)
	if err != nil {
		return PrepareReport{}, false, fmt.Errorf("inspect existing Phase 7A preparation: %w", err)
	}
	defer rows.Close()
	latest := map[string]persistedPhase7ALedger{}
	for rows.Next() {
		var row persistedPhase7ALedger
		if err := rows.Scan(&row.publicID, &row.operation, &row.status, &row.sha, &row.digest, &row.attempt,
			&row.sourceSchema, &row.targetSchema, &row.sourceTable, &row.targetTable, &row.batchNo,
			&row.sourceCount, &row.targetCount, &row.sourceHash, &row.targetHash, &row.converterVersion,
			&row.releaseIdentityHash, &row.completed); err != nil {
			return PrepareReport{}, false, err
		}
		if row.sha != request.SourceExactSHA || row.digest != request.BinaryImageDigest {
			return PrepareReport{}, false, fmt.Errorf("Phase 7A ledger %s plan=%d has a different release identity", row.operation, request.PlanVersion)
		}
		latest[row.operation] = row
	}
	if err := rows.Err(); err != nil {
		return PrepareReport{}, false, err
	}
	if len(latest) == 0 {
		return PrepareReport{}, false, nil
	}
	for _, operation := range phase7AFinalOperations {
		row, found := latest[operation]
		if !found || row.status != "passed" {
			return PrepareReport{}, false, nil
		}
	}
	var completed sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT completed_at FROM cutover_controls
WHERE control_name=? AND plan_version=? AND source_exact_sha=? AND binary_image_digest=? FOR UPDATE`,
		phase7AControlName, request.PlanVersion, request.SourceExactSHA, request.BinaryImageDigest).Scan(&completed); err != nil {
		return PrepareReport{}, false, fmt.Errorf("read completed Phase 7A control: %w", err)
	}
	if !completed.Valid {
		return PrepareReport{}, false, errors.New("passed Phase 7A ledgers exist without a completed cutover control")
	}
	counts, err := collectCutoverCountsV2(ctx, tx, request)
	if err != nil {
		return PrepareReport{}, false, err
	}
	if err := validatePhase7ACounts(counts, request); err != nil {
		return PrepareReport{}, false, fmt.Errorf("revalidate completed Phase 7A preparation: %w", err)
	}
	expected, err := currentPhase7ALedgerExpectations(ctx, tx, request, counts)
	if err != nil {
		return PrepareReport{}, false, err
	}
	releaseHash := releaseIdentityHash(request.SourceExactSHA, request.BinaryImageDigest, version, version)
	for _, operation := range phase7AFinalOperations {
		row := latest[operation]
		want := expected[operation]
		if row.sourceSchema != version || row.targetSchema != version || row.batchNo != 1 ||
			row.sourceTable != want.sourceTable || row.targetTable != want.targetTable ||
			row.sourceCount != want.sourceCount || row.targetCount != want.targetCount ||
			row.sourceHash != want.sourceHash || row.targetHash != want.targetHash ||
			row.converterVersion != phase7AConverter || row.releaseIdentityHash != releaseHash || !row.completed.Valid {
			return PrepareReport{}, false, fmt.Errorf("completed Phase 7A ledger %s no longer matches current data", operation)
		}
	}
	return PrepareReport{
		PlanVersion: request.PlanVersion, SourceExactSHA: request.SourceExactSHA,
		BinaryImageDigest: request.BinaryImageDigest, SourceSchemaVersion: version, TargetSchemaVersion: version,
		QuiesceLedgerPublicID:        latest[QuiesceOperation].publicID,
		ReconciliationLedgerPublicID: latest[ReconciliationOperation].publicID,
		ConverterAuditLedgerPublicID: latest[ConverterAuditOperation].publicID,
		Counts:                       counts, PreparedAt: completed.Time.UTC(),
	}, true, nil
}

func currentPhase7ALedgerExpectations(ctx context.Context, tx *sql.Tx, request PrepareRequest, counts map[string]uint64) (map[string]phase7ALedgerExpectation, error) {
	outboxHash, err := hashStringColumn(ctx, tx, "SELECT row_hash FROM legacy_outbox_archive ORDER BY source_outbox_id")
	if err != nil {
		return nil, err
	}
	externalArtifactHash, err := hashLegacyExternalArtifacts(ctx, tx)
	if err != nil {
		return nil, err
	}
	reconciliationHash := canonicalHashFields("phase7a-reconciliation/v1", outboxHash, externalArtifactHash)
	publishedHash, err := hashStringColumn(ctx, tx, "SELECT row_hash FROM legacy_outbox_archive WHERE publication_state='published' ORDER BY source_outbox_id")
	if err != nil {
		return nil, err
	}
	unpublishedHash, err := hashStringColumn(ctx, tx, "SELECT row_hash FROM legacy_outbox_archive WHERE publication_state='unpublished' ORDER BY source_outbox_id")
	if err != nil {
		return nil, err
	}
	conversionHash, err := hashLatestConversions(ctx, tx)
	if err != nil {
		return nil, err
	}
	conversionHashes := map[string]string{}
	for key, condition := range map[string]string{
		SubjectDerivedOperation:     "",
		ConversionFailedOperation:   " AND r.status='failed'",
		TaskCreatedOperation:        " AND r.anti_join_result='created'",
		ExistingTargetTaskOperation: " AND r.anti_join_result='existing-target-task'",
		AntiJoinSkippedOperation:    " AND r.anti_join_result='anti-join-skipped'",
	} {
		value, hashErr := hashLatestConversionsWhere(ctx, tx, condition)
		if hashErr != nil {
			return nil, hashErr
		}
		conversionHashes[key] = value
	}
	quiesceHash := canonicalHashFields("quiesce/v2",
		fmt.Sprint(request.ObservedIngressWriters), fmt.Sprint(request.ObservedMutationWriters),
		fmt.Sprint(request.ObservedLegacyWorkers), fmt.Sprint(request.ObservedUnknownExternalWrite),
		fmt.Sprint(counts["active_legacy_leases"]), fmt.Sprint(counts["running_v3_tasks"]),
	)
	return map[string]phase7ALedgerExpectation{
		QuiesceOperation: {sourceTable: "legacy_runtime+external_observations", targetTable: "quiesced_runtime", sourceCount: 6, targetCount: 6, sourceHash: quiesceHash, targetHash: quiesceHash},
		ReconciliationOperation: {sourceTable: "outbox_events+legacy_external_artifacts", targetTable: "legacy_archives+read_only_observation_tasks",
			sourceCount: counts["outbox_source"] + counts["change_requests_archived"],
			targetCount: counts["outbox_archived"] + counts["change_requests_archived"],
			sourceHash:  reconciliationHash, targetHash: reconciliationHash},
		ConverterAuditOperation:            {sourceTable: "legacy_child_subjects", targetTable: "legacy_conversion_records+async_tasks", sourceCount: counts["required_conversion_subjects"], targetCount: counts["latest_conversion_records"], sourceHash: conversionHash, targetHash: conversionHash},
		OutboxArchivedPublishedOperation:   {sourceTable: "outbox_events", targetTable: "legacy_outbox_archive", sourceCount: counts["outbox_archived_published"], targetCount: counts["outbox_archived_published"], sourceHash: publishedHash, targetHash: publishedHash},
		OutboxArchivedUnpublishedOperation: {sourceTable: "outbox_events", targetTable: "legacy_outbox_archive", sourceCount: counts["outbox_archived_unpublished"], targetCount: counts["outbox_archived_unpublished"], sourceHash: unpublishedHash, targetHash: unpublishedHash},
		SubjectDerivedOperation:            {sourceTable: "legacy_child_subjects", targetTable: "legacy_conversion_records", sourceCount: counts["subject_derived"], targetCount: counts["subject_derived"], sourceHash: conversionHashes[SubjectDerivedOperation], targetHash: conversionHashes[SubjectDerivedOperation]},
		ConversionFailedOperation:          {sourceTable: "legacy_conversion_records", targetTable: "safe_terminal_or_fallback_outcomes", sourceCount: counts["conversion_failed"], targetCount: counts["conversion_failed"], sourceHash: conversionHashes[ConversionFailedOperation], targetHash: conversionHashes[ConversionFailedOperation]},
		TaskCreatedOperation:               {sourceTable: "legacy_conversion_records", targetTable: "async_tasks", sourceCount: counts["task_created"], targetCount: counts["task_created"], sourceHash: conversionHashes[TaskCreatedOperation], targetHash: conversionHashes[TaskCreatedOperation]},
		ExistingTargetTaskOperation:        {sourceTable: "async_tasks", targetTable: "legacy_conversion_records", sourceCount: counts["existing_target_task"], targetCount: counts["existing_target_task"], sourceHash: conversionHashes[ExistingTargetTaskOperation], targetHash: conversionHashes[ExistingTargetTaskOperation]},
		AntiJoinSkippedOperation:           {sourceTable: "async_tasks", targetTable: "legacy_conversion_records", sourceCount: counts["anti_join_skipped"], targetCount: counts["anti_join_skipped"], sourceHash: conversionHashes[AntiJoinSkippedOperation], targetHash: conversionHashes[AntiJoinSkippedOperation]},
	}, nil
}

func collectCutoverCountsV2(ctx context.Context, tx *sql.Tx, request PrepareRequest) (map[string]uint64, error) {
	queries := map[string]string{
		"outbox_source":                 "SELECT COUNT(*) FROM outbox_events",
		"outbox_archived":               "SELECT COUNT(*) FROM legacy_outbox_archive",
		"outbox_archived_published":     "SELECT COUNT(*) FROM legacy_outbox_archive WHERE publication_state='published'",
		"outbox_archived_unpublished":   "SELECT COUNT(*) FROM legacy_outbox_archive WHERE publication_state='unpublished'",
		"incidents_archived":            "SELECT COUNT(*) FROM legacy_incident_state_archive",
		"signals_archived":              "SELECT COUNT(*) FROM legacy_signal_archive",
		"signals_backfilled":            "SELECT COALESCE(SUM(source_count),0) FROM migration_ledger WHERE plan_version=? AND operation='BACKFILL-V3/incident-signals' AND status='passed'",
		"events_archived":               "SELECT COUNT(*) FROM legacy_event_archive",
		"events_backfilled":             "SELECT COALESCE(SUM(source_count),0) FROM migration_ledger WHERE plan_version=? AND operation='BACKFILL-V3/incident-events' AND status='passed'",
		"evidence_archived":             "SELECT COUNT(*) FROM legacy_evidence_archive",
		"evidence_backfilled":           "SELECT COALESCE(SUM(source_count),0) FROM migration_ledger WHERE plan_version=? AND operation='BACKFILL-V3/evidence' AND status='passed'",
		"agent_steps_archived":          "SELECT COUNT(*) FROM legacy_agent_step_archive",
		"agent_steps_backfilled":        "SELECT COALESCE(SUM(source_count),0) FROM migration_ledger WHERE plan_version=? AND operation='BACKFILL-V3/agent-steps' AND status='passed'",
		"change_candidates_archived":    "SELECT COUNT(*) FROM legacy_change_candidate_archive",
		"change_candidates_backfilled":  "SELECT COALESCE(SUM(source_count),0) FROM migration_ledger WHERE plan_version=? AND operation='BACKFILL-V3/change-candidates' AND status='passed'",
		"change_assessments_archived":   "SELECT COUNT(*) FROM legacy_change_assessment_archive",
		"plans_archived":                "SELECT COUNT(*) FROM legacy_remediation_plan_archive",
		"approvals_archived":            "SELECT COUNT(*) FROM legacy_approval_archive",
		"agent_checkpoints_archived":    "SELECT COUNT(*) FROM legacy_agent_checkpoint_archive",
		"change_requests_archived":      "SELECT COUNT(*) FROM legacy_change_request_archive",
		"verification_archived":         "SELECT COUNT(*) FROM legacy_verification_archive",
		"postmortems_archived":          "SELECT COUNT(*) FROM legacy_postmortem_archive",
		"postmortems_source":            "SELECT COUNT(*) FROM postmortems",
		"conversion_records":            "SELECT COUNT(*) FROM legacy_conversion_records",
		"latest_conversion_records":     latestConversionCountSQL,
		"required_conversion_subjects":  requiredConversionSubjectsSQL,
		"subject_derived":               latestConversionCountSQL,
		"conversion_failed":             latestConversionCountSQL + " AND r.status='failed'",
		"task_created":                  latestConversionCountSQL + " AND r.anti_join_result='created'",
		"existing_target_task":          latestConversionCountSQL + " AND r.anti_join_result='existing-target-task'",
		"anti_join_skipped":             latestConversionCountSQL + " AND r.anti_join_result='anti-join-skipped'",
		"conversion_not_applicable":     latestConversionCountSQL + " AND r.anti_join_result='not-applicable'",
		"missing_conversion_records":    missingConversionRecordsSQL,
		"unsettled_conversion_failures": unsettledConversionFailuresSQL,
		"task_duplicates":               duplicateConversionTasksSQL,
		"terminal_child_tasks":          terminalChildTasksSQL,
		"missing_archive_rows":          missingArchiveRowsSQL,
		"archive_hash_mismatches":       archiveHashMismatchSQL,
		"unknown_external_writes":       unknownExternalWritesSQL,
		"active_legacy_leases":          legacyActiveLeaseCountSQL,
		"running_v3_tasks":              "SELECT COUNT(*) FROM async_tasks WHERE status='running'",
		"legacy_rows_remaining": `SELECT
  (SELECT COUNT(*) FROM incidents WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM incident_signals WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM incident_events WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM evidence_items WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM agent_runs WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM agent_steps WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM changes WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM remediation_plans WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM remediation_approvals WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM change_requests WHERE domain_schema_version IS NULL) +
  (SELECT COUNT(*) FROM verification_runs WHERE domain_schema_version IS NULL)`,
		"migrated_legacy_incidents":         "SELECT COUNT(*) FROM incidents WHERE migrated_legacy=TRUE",
		"migrated_legacy_signals":           "SELECT COUNT(*) FROM incident_signals WHERE migrated_legacy=TRUE",
		"migrated_legacy_events":            "SELECT COUNT(*) FROM incident_events WHERE migrated_legacy=TRUE",
		"migrated_legacy_agent_runs":        "SELECT COUNT(*) FROM agent_runs WHERE migrated_legacy=TRUE",
		"migrated_legacy_agent_steps":       "SELECT COUNT(*) FROM agent_steps WHERE migrated_legacy=TRUE",
		"migrated_legacy_evidence":          "SELECT COUNT(*) FROM evidence_items WHERE migrated_legacy=TRUE",
		"migrated_legacy_changes":           "SELECT COUNT(*) FROM changes WHERE migrated_legacy=TRUE",
		"migrated_legacy_change_requests":   "SELECT COUNT(*) FROM change_requests WHERE migrated_legacy=TRUE",
		"migrated_legacy_verification_runs": "SELECT COUNT(*) FROM verification_runs WHERE migrated_legacy=TRUE",
		"migrated_legacy_tasks":             "SELECT COUNT(*) FROM async_tasks WHERE migrated_legacy=TRUE",
		"migrated_legacy_context_tasks":     "SELECT COUNT(*) FROM async_tasks WHERE migrated_legacy_context=TRUE",
		"migrated_legacy_context_incidents": "SELECT COUNT(*) FROM incidents WHERE migrated_legacy_context=TRUE",
		"marker_count":                      "SELECT COUNT(*) FROM migration_ledger WHERE operation='CUTOVER-V3'",
	}
	result := make(map[string]uint64, len(queries)+4)
	keys := make([]string, 0, len(queries))
	for key := range queries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var value uint64
		var args []any
		if strings.HasSuffix(key, "_backfilled") {
			args = []any{request.PlanVersion}
		}
		if err := tx.QueryRowContext(ctx, queries[key], args...).Scan(&value); err != nil {
			return nil, fmt.Errorf("collect Phase 7A count %s: %w", key, err)
		}
		result[key] = value
	}
	result["observed_ingress_writers"] = request.ObservedIngressWriters
	result["observed_mutation_writers"] = request.ObservedMutationWriters
	result["observed_legacy_workers"] = request.ObservedLegacyWorkers
	result["observed_unknown_external_writes"] = request.ObservedUnknownExternalWrite
	result["unknown_external_writes"] += request.ObservedUnknownExternalWrite
	return result, nil
}

func validatePhase7ACounts(counts map[string]uint64, request PrepareRequest) error {
	if counts == nil {
		return errors.New("Phase 7A counts are absent")
	}
	if counts["outbox_source"] != counts["outbox_archived"] ||
		counts["outbox_archived"] != counts["outbox_archived_published"]+counts["outbox_archived_unpublished"] {
		return fmt.Errorf("outbox archive parity source=%d archived=%d published=%d unpublished=%d",
			counts["outbox_source"], counts["outbox_archived"], counts["outbox_archived_published"], counts["outbox_archived_unpublished"])
	}
	for _, pair := range [][2]string{
		{"signals_archived", "signals_backfilled"},
		{"events_archived", "events_backfilled"},
		{"evidence_archived", "evidence_backfilled"},
		{"agent_steps_archived", "agent_steps_backfilled"},
		{"change_candidates_archived", "change_candidates_backfilled"},
	} {
		if counts[pair[0]] != counts[pair[1]] {
			return fmt.Errorf("backfill archive parity %s=%d ledger=%d", pair[0], counts[pair[0]], counts[pair[1]])
		}
	}
	if counts["change_candidates_archived"] != counts["change_assessments_archived"] {
		return fmt.Errorf("legacy Change assessment parity candidates=%d assessments=%d",
			counts["change_candidates_archived"], counts["change_assessments_archived"])
	}
	if counts["required_conversion_subjects"] != counts["latest_conversion_records"] ||
		counts["subject_derived"] != counts["latest_conversion_records"] {
		return fmt.Errorf("conversion parity required=%d latest=%d subject_derived=%d",
			counts["required_conversion_subjects"], counts["latest_conversion_records"], counts["subject_derived"])
	}
	classified := counts["task_created"] + counts["existing_target_task"] + counts["anti_join_skipped"] + counts["conversion_not_applicable"]
	if classified != counts["latest_conversion_records"] {
		return fmt.Errorf("conversion anti-join classifications=%d latest=%d", classified, counts["latest_conversion_records"])
	}
	if counts["postmortems_source"] != counts["postmortems_archived"] {
		return fmt.Errorf("postmortem archive parity source=%d archived=%d", counts["postmortems_source"], counts["postmortems_archived"])
	}
	for _, invariant := range []string{
		"missing_archive_rows",
		"archive_hash_mismatches",
		"missing_conversion_records",
		"unsettled_conversion_failures",
		"task_duplicates",
		"terminal_child_tasks",
		"unknown_external_writes",
		"active_legacy_leases",
		"running_v3_tasks",
		"legacy_rows_remaining",
	} {
		if counts[invariant] != 0 {
			return fmt.Errorf("Phase 7A invariant %s=%d", invariant, counts[invariant])
		}
	}
	if request.ObservedIngressWriters != 0 || request.ObservedMutationWriters != 0 ||
		request.ObservedLegacyWorkers != 0 || request.ObservedUnknownExternalWrite != 0 ||
		counts["observed_ingress_writers"] != 0 || counts["observed_mutation_writers"] != 0 ||
		counts["observed_legacy_workers"] != 0 || counts["observed_unknown_external_writes"] != 0 {
		return errors.New("Phase 7A external quiesce observations are not zero")
	}
	if counts["marker_count"] != 0 {
		return fmt.Errorf("CUTOVER-V3 marker count=%d want zero during prepare", counts["marker_count"])
	}
	return nil
}

const latestConversionBaseSQL = ` FROM legacy_conversion_records r
WHERE NOT EXISTS (
  SELECT 1 FROM legacy_conversion_records newer
  WHERE newer.subject_type=r.subject_type AND newer.subject_id=r.subject_id
    AND newer.converter_version=r.converter_version AND newer.attempt>r.attempt
)`

const latestConversionCountSQL = "SELECT COUNT(*)" + latestConversionBaseSQL

const requiredConversionSubjectsSQL = `SELECT
  (SELECT COUNT(*) FROM legacy_agent_checkpoint_archive) +
  (SELECT COUNT(*) FROM legacy_change_request_archive) +
  (SELECT COUNT(*) FROM legacy_verification_archive)`

const missingConversionRecordsSQL = `SELECT COUNT(*) FROM (
SELECT 'agent_run' subject_type,source_agent_run_id subject_id FROM legacy_agent_checkpoint_archive
UNION ALL SELECT 'change_request',source_change_request_id FROM legacy_change_request_archive
UNION ALL SELECT 'verification_run',source_verification_run_id FROM legacy_verification_archive
) subjects LEFT JOIN (` +
	`SELECT r.subject_type,r.subject_id FROM legacy_conversion_records r WHERE NOT EXISTS (
SELECT 1 FROM legacy_conversion_records newer WHERE newer.subject_type=r.subject_type AND newer.subject_id=r.subject_id
AND newer.converter_version=r.converter_version AND newer.attempt>r.attempt)) latest
ON latest.subject_type=subjects.subject_type AND latest.subject_id=subjects.subject_id
WHERE latest.subject_id IS NULL`

const unsettledConversionFailuresSQL = `SELECT COUNT(*)` + latestConversionBaseSQL + ` AND r.status='failed' AND NOT (
  (r.subject_type='agent_run' AND (
    EXISTS (SELECT 1 FROM legacy_agent_checkpoint_archive a WHERE a.source_agent_run_id=r.subject_id
      AND a.source_status NOT IN ('PENDING','RUNNING'))
    OR EXISTS (SELECT 1 FROM async_tasks t WHERE t.id=r.target_task_id AND t.subject_type='incident'
      AND t.task_type='investigation.advance' AND t.transition='investigation.start')
  ))
  OR (r.subject_type='verification_run' AND (
    EXISTS (SELECT 1 FROM legacy_verification_archive v WHERE v.source_verification_run_id=r.subject_id
      AND v.source_status NOT IN ('pending','running'))
    OR EXISTS (SELECT 1 FROM async_tasks t WHERE t.id=r.target_task_id AND t.subject_type='incident'
      AND t.task_type='investigation.advance' AND t.transition='investigation.start')
  ))
  OR (r.subject_type='change_request' AND r.reason_code IN (
    'legacy_partial_external_write','legacy_approval_incomplete','legacy_no_external_write'
  ) AND r.target_task_id IS NULL)
)`

const duplicateConversionTasksSQL = `SELECT COUNT(*) FROM (
SELECT incident_id,cycle_no,task_type,subject_type,subject_id,transition,COUNT(*) n
FROM async_tasks WHERE replay_generation=0 AND migrated_legacy_context=TRUE
GROUP BY incident_id,cycle_no,task_type,subject_type,subject_id,transition HAVING COUNT(*)>1
) duplicated`

const terminalChildTasksSQL = `SELECT COUNT(*) FROM async_tasks t WHERE EXISTS (
  SELECT 1 FROM legacy_conversion_records r WHERE r.target_task_id=t.id
) AND (
  (t.subject_type='agent_run' AND EXISTS (SELECT 1 FROM legacy_agent_checkpoint_archive a
    WHERE a.source_agent_run_id=t.subject_id AND a.source_status NOT IN ('PENDING','RUNNING')))
  OR (t.subject_type='verification_run' AND EXISTS (SELECT 1 FROM legacy_verification_archive v
    WHERE v.source_verification_run_id=t.subject_id AND v.source_status NOT IN ('pending','running')))
)`

const missingArchiveRowsSQL = `SELECT
  (SELECT COUNT(*) FROM legacy_signal_archive a LEFT JOIN incident_signals s ON s.id=a.source_signal_id
    WHERE s.id IS NULL OR COALESCE(s.domain_schema_version,0)<>3 OR COALESCE(s.cycle_no,0)<>1 OR s.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_event_archive a LEFT JOIN incident_events e ON e.id=a.source_event_id
    WHERE e.id IS NULL OR COALESCE(e.domain_schema_version,0)<>3 OR COALESCE(e.cycle_no,0)<>1 OR e.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_evidence_archive a LEFT JOIN evidence_items e ON e.id=a.source_evidence_id
    WHERE e.id IS NULL OR COALESCE(e.domain_schema_version,0)<>3 OR COALESCE(e.cycle_no,0)<>1 OR e.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_agent_step_archive a LEFT JOIN agent_steps s ON s.id=a.source_agent_step_id
    WHERE s.id IS NULL OR COALESCE(s.domain_schema_version,0)<>3 OR COALESCE(s.cycle_no,0)<>1 OR s.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_change_candidate_archive a LEFT JOIN changes c ON c.id=a.source_change_id
    WHERE c.id IS NULL OR COALESCE(c.domain_schema_version,0)<>3 OR COALESCE(c.cycle_no,0)<>1 OR c.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_change_assessment_archive a LEFT JOIN changes c ON c.id=a.source_change_id
    WHERE c.id IS NULL OR COALESCE(c.domain_schema_version,0)<>3 OR COALESCE(c.cycle_no,0)<>1 OR c.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_incident_state_archive a LEFT JOIN incidents i ON i.id=a.source_incident_id
    WHERE i.id IS NULL OR COALESCE(i.domain_schema_version,0)<>3 OR COALESCE(i.cycle_no,0)<>1 OR i.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_agent_checkpoint_archive a LEFT JOIN agent_runs r ON r.id=a.source_agent_run_id
    WHERE r.id IS NULL OR COALESCE(r.domain_schema_version,0)<>3 OR COALESCE(r.cycle_no,0)<>1 OR r.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_remediation_plan_archive a LEFT JOIN remediation_plans p ON p.id=a.source_plan_id
    WHERE p.id IS NULL OR COALESCE(p.domain_schema_version,0)<>3 OR COALESCE(p.cycle_no,0)<>1 OR p.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_approval_archive a LEFT JOIN remediation_approvals p ON p.id=a.source_approval_id
    WHERE p.id IS NULL OR COALESCE(p.domain_schema_version,0)<>3 OR COALESCE(p.cycle_no,0)<>1 OR p.migrated_legacy<>TRUE) +
  (SELECT COUNT(*) FROM legacy_change_request_archive a LEFT JOIN change_requests c ON c.id=a.source_change_request_id
    WHERE c.id IS NULL OR COALESCE(c.domain_schema_version,0)<>3 OR COALESCE(c.cycle_no,0)<>1 OR c.migrated_legacy<>TRUE) +
	  (SELECT COUNT(*) FROM legacy_verification_archive a LEFT JOIN verification_runs v ON v.id=a.source_verification_run_id
	    WHERE v.id IS NULL OR COALESCE(v.domain_schema_version,0)<>3 OR COALESCE(v.cycle_no,0)<>1 OR v.migrated_legacy<>TRUE) +
	  (SELECT COUNT(*) FROM legacy_verification_archive a
	    WHERE a.conversion_status='passed' AND (
	      JSON_LENGTH(a.checks_json)<>(SELECT COUNT(*) FROM verification_checks c
	        WHERE c.verification_run_id=a.source_verification_run_id AND c.domain_schema_version=3 AND c.migrated_legacy=TRUE)
	      OR JSON_LENGTH(a.samples_json)<>(SELECT COUNT(*) FROM verification_samples s
	        WHERE s.verification_run_id=a.source_verification_run_id AND s.domain_schema_version=3 AND s.migrated_legacy=TRUE)
	    )) +
  (SELECT COUNT(*) FROM legacy_postmortem_archive a LEFT JOIN postmortems p ON p.id=a.source_postmortem_id
    WHERE p.id IS NULL) +
  (SELECT COUNT(*) FROM postmortems p LEFT JOIN legacy_postmortem_archive a ON a.source_postmortem_id=p.id
    WHERE a.source_postmortem_id IS NULL) +
  (SELECT COUNT(*) FROM outbox_events o LEFT JOIN legacy_outbox_archive a ON a.source_outbox_id=o.id
    WHERE a.source_outbox_id IS NULL) +
  (SELECT COUNT(*) FROM legacy_outbox_archive a LEFT JOIN outbox_events o ON o.id=a.source_outbox_id
    WHERE o.id IS NULL)`

const archiveHashMismatchSQL = `SELECT
  (SELECT COUNT(*) FROM legacy_signal_archive WHERE conversion_status<>'passed' OR source_hash<>target_hash) +
  (SELECT COUNT(*) FROM legacy_event_archive WHERE conversion_status<>'passed' OR source_hash<>target_hash) +
  (SELECT COUNT(*) FROM legacy_evidence_archive WHERE conversion_status<>'passed' OR source_hash<>target_hash) +
  (SELECT COUNT(*) FROM legacy_agent_step_archive WHERE conversion_status<>'passed' OR source_hash<>target_hash) +
  (SELECT COUNT(*) FROM legacy_change_candidate_archive WHERE conversion_status<>'passed' OR source_hash<>target_hash) +
  (SELECT COUNT(*) FROM legacy_change_assessment_archive
    WHERE conversion_status<>'passed' OR source_change_hash IS NULL OR assessment_hash IS NULL) +
  (SELECT COUNT(*) FROM legacy_incident_state_archive
    WHERE snapshot_hash NOT REGEXP '^[0-9a-f]{64}$' OR target_status='' OR reason_code='') +
  (SELECT COUNT(*) FROM legacy_agent_checkpoint_archive
    WHERE conversion_status='passed' AND (target_checkpoint_hash IS NULL OR target_checkpoint_hash NOT REGEXP '^[0-9a-f]{64}$')) +
  (SELECT COUNT(*) FROM legacy_remediation_plan_archive
    WHERE source_content_hash NOT REGEXP '^[0-9a-f]{64}$' OR converter_result<>'superseded') +
  (SELECT COUNT(*) FROM legacy_approval_archive
    WHERE source_content_hash NOT REGEXP '^[0-9a-f]{64}$' OR converter_result<>'non_authoritative') +
  (SELECT COUNT(*) FROM legacy_change_request_archive
    WHERE snapshot_hash NOT REGEXP '^[0-9a-f]{64}$' OR source_content_hash IS NULL
      OR source_content_hash NOT REGEXP '^[0-9a-f]{64}$') +
	  (SELECT COUNT(*) FROM legacy_verification_archive
	    WHERE profile_hash NOT REGEXP '^[0-9a-f]{64}$'
	      OR source_content_hash IS NULL OR source_content_hash NOT REGEXP '^[0-9a-f]{64}$'
	      OR (conversion_status='passed' AND (output_hash IS NULL OR output_hash NOT REGEXP '^[0-9a-f]{64}$'))) +
  (SELECT COUNT(*) FROM legacy_postmortem_archive WHERE content_hash NOT REGEXP '^[0-9a-f]{64}$') +
  (SELECT COUNT(*) FROM legacy_outbox_archive WHERE conversion_status<>'passed' OR row_hash IS NULL
    OR row_hash NOT REGEXP '^[0-9a-f]{64}$')`

const unknownExternalWritesSQL = `SELECT COUNT(*) FROM outbox_events o
JOIN legacy_outbox_event_registry r ON r.registry_version=2
  AND BINARY r.event_type=BINARY o.event_type AND r.schema_version=o.schema_version
  AND r.external_write_event=TRUE
LEFT JOIN incidents i ON BINARY i.public_id=BINARY o.aggregate_id
  OR BINARY CAST(i.id AS CHAR)=BINARY o.aggregate_id
WHERE i.id IS NULL OR NOT EXISTS (
  SELECT 1 FROM legacy_change_request_archive c
  WHERE c.incident_id=i.id AND c.external_state='observe-existing-pr' AND c.conversion_status='passed'
)`

func persistPhase7ALedgers(ctx context.Context, tx *sql.Tx, identity ReleaseIdentity, request PrepareRequest, counts map[string]uint64, at time.Time) (phase7ALedgerIDs, error) {
	var ids phase7ALedgerIDs
	if err := validatePhase7ACounts(counts, request); err != nil {
		return ids, err
	}
	for _, invariant := range []struct {
		name  string
		value uint64
	}{
		{"missing_archive_rows", counts["missing_archive_rows"]},
		{"archive_hash_mismatches", counts["archive_hash_mismatches"]},
		{"missing_conversion_records", counts["missing_conversion_records"]},
		{"unsettled_conversion_failures", counts["unsettled_conversion_failures"]},
		{"task_duplicates", counts["task_duplicates"]},
		{"terminal_child_tasks", counts["terminal_child_tasks"]},
		{"unknown_external_writes", counts["unknown_external_writes"]},
		{"active_legacy_leases", counts["active_legacy_leases"]},
	} {
		if invariant.value != 0 {
			return ids, fmt.Errorf("Phase 7A invariant %s=%d", invariant.name, invariant.value)
		}
	}
	outboxHash, err := hashStringColumn(ctx, tx, "SELECT row_hash FROM legacy_outbox_archive ORDER BY source_outbox_id")
	if err != nil {
		return ids, err
	}
	externalArtifactHash, err := hashLegacyExternalArtifacts(ctx, tx)
	if err != nil {
		return ids, err
	}
	reconciliationHash := canonicalHashFields("phase7a-reconciliation/v1", outboxHash, externalArtifactHash)
	conversionHash, err := hashLatestConversions(ctx, tx)
	if err != nil {
		return ids, err
	}
	quiesceHash := canonicalHashFields("quiesce/v2",
		fmt.Sprint(request.ObservedIngressWriters), fmt.Sprint(request.ObservedMutationWriters),
		fmt.Sprint(request.ObservedLegacyWorkers), fmt.Sprint(request.ObservedUnknownExternalWrite),
		fmt.Sprint(counts["active_legacy_leases"]), fmt.Sprint(counts["running_v3_tasks"]),
	)
	ids.Quiesce, err = persistFinalLedger(ctx, tx, identity, QuiesceOperation, "quiesce",
		"legacy_runtime+external_observations", "quiesced_runtime", 6, 6, quiesceHash, quiesceHash,
		LedgerCompletion{ActiveLegacyLeases: counts["active_legacy_leases"],
			ObservedIngressWriters: request.ObservedIngressWriters, ObservedMutationWriters: request.ObservedMutationWriters,
			ObservedLegacyWorkers: request.ObservedLegacyWorkers, UnknownExternalWrites: request.ObservedUnknownExternalWrite},
		"ingress, mutations, legacy workers, active leases, running tasks, and unknown external writes are zero", at)
	if err != nil {
		return ids, err
	}
	ids.Reconciliation, err = persistFinalLedger(ctx, tx, identity, ReconciliationOperation, "reconcile",
		"outbox_events+legacy_external_artifacts", "legacy_archives+read_only_observation_tasks",
		counts["outbox_source"]+counts["change_requests_archived"],
		counts["outbox_archived"]+counts["change_requests_archived"], reconciliationHash, reconciliationHash,
		LedgerCompletion{MissingArchiveRows: counts["missing_archive_rows"] + counts["archive_hash_mismatches"],
			UnknownExternalWrites: counts["unknown_external_writes"]},
		"outbox rows were archived one-to-one; external writes were reconciled read-only and no Task came from an outbox payload", at)
	if err != nil {
		return ids, err
	}
	ids.ConverterAudit, err = persistFinalLedger(ctx, tx, identity, ConverterAuditOperation, "convert",
		"legacy_child_subjects", "legacy_conversion_records+async_tasks",
		counts["required_conversion_subjects"], counts["latest_conversion_records"], conversionHash, conversionHash,
		LedgerCompletion{ConversionFailures: counts["unsettled_conversion_failures"],
			MissingArchiveRows: counts["missing_conversion_records"],
			DuplicateTasks:     counts["task_duplicates"] + counts["terminal_child_tasks"]},
		"versioned child conversion, Incident state mapping, Task anti-join, and migrated-legacy provenance passed", at)
	if err != nil {
		return ids, err
	}
	if err := persistPhase7ACategoryLedgers(ctx, tx, identity, counts, at); err != nil {
		return ids, err
	}
	return ids, nil
}

func persistPhase7ACategoryLedgers(ctx context.Context, tx *sql.Tx, identity ReleaseIdentity, counts map[string]uint64, at time.Time) error {
	publishedHash, err := hashStringColumn(ctx, tx, "SELECT row_hash FROM legacy_outbox_archive WHERE publication_state='published' ORDER BY source_outbox_id")
	if err != nil {
		return err
	}
	unpublishedHash, err := hashStringColumn(ctx, tx, "SELECT row_hash FROM legacy_outbox_archive WHERE publication_state='unpublished' ORDER BY source_outbox_id")
	if err != nil {
		return err
	}
	conversionHashes := map[string]string{}
	for key, condition := range map[string]string{
		"subject_derived":      "",
		"conversion_failed":    " AND r.status='failed'",
		"task_created":         " AND r.anti_join_result='created'",
		"existing_target_task": " AND r.anti_join_result='existing-target-task'",
		"anti_join_skipped":    " AND r.anti_join_result='anti-join-skipped'",
	} {
		value, hashErr := hashLatestConversionsWhere(ctx, tx, condition)
		if hashErr != nil {
			return hashErr
		}
		conversionHashes[key] = value
	}
	for _, category := range []struct {
		operation, stage, sourceTable, targetTable, countKey, hash, summary string
	}{
		{OutboxArchivedPublishedOperation, "reconcile", "outbox_events", "legacy_outbox_archive", "outbox_archived_published", publishedHash, "published legacy outbox rows archived one-to-one"},
		{OutboxArchivedUnpublishedOperation, "reconcile", "outbox_events", "legacy_outbox_archive", "outbox_archived_unpublished", unpublishedHash, "unpublished legacy outbox rows archived one-to-one"},
		{SubjectDerivedOperation, "convert", "legacy_child_subjects", "legacy_conversion_records", "subject_derived", conversionHashes["subject_derived"], "legacy child subjects received one latest conversion record"},
		{ConversionFailedOperation, "convert", "legacy_conversion_records", "safe_terminal_or_fallback_outcomes", "conversion_failed", conversionHashes["conversion_failed"], "failed conversions were archived and converged under the fail-closed contract"},
		{TaskCreatedOperation, "convert", "legacy_conversion_records", "async_tasks", "task_created", conversionHashes["task_created"], "conversion-created Tasks were counted independently from outbox archives"},
		{ExistingTargetTaskOperation, "convert", "async_tasks", "legacy_conversion_records", "existing_target_task", conversionHashes["existing_target_task"], "existing valid V3 Tasks were preserved by anti-join"},
		{AntiJoinSkippedOperation, "convert", "async_tasks", "legacy_conversion_records", "anti_join_skipped", conversionHashes["anti_join_skipped"], "idempotent reruns skipped already-derived migrated Tasks"},
	} {
		count := counts[category.countKey]
		if _, err := persistFinalLedger(ctx, tx, identity, category.operation, category.stage,
			category.sourceTable, category.targetTable, count, count, category.hash, category.hash,
			LedgerCompletion{}, category.summary, at); err != nil {
			return err
		}
	}
	return nil
}

func persistFinalLedger(ctx context.Context, tx *sql.Tx, identity ReleaseIdentity, operation, stage, sourceTable, targetTable string,
	sourceCount, targetCount uint64, sourceHash, targetHash string, invariants LedgerCompletion, summary string, at time.Time) (string, error) {
	batch, err := beginLedgerBatch(ctx, tx, identity, stage, operation, sourceTable, targetTable, 1, nil, nil, phase7AConverter, at)
	if errors.Is(err, ErrLedgerAlreadyPassed) {
		if batch.SourceSchema != identity.SourceSchema || batch.TargetSchema != identity.TargetSchema ||
			batch.SourceTable != sourceTable || batch.TargetTable != targetTable || batch.BatchNo != 1 ||
			batch.SourceCount != sourceCount || batch.TargetCount != targetCount ||
			batch.SourceHash != sourceHash || batch.TargetHash != targetHash ||
			batch.ConverterVersion != phase7AConverter ||
			batch.ReleaseHash != releaseIdentityHash(identity.SourceExactSHA, identity.BinaryImageDigest, identity.SourceSchema, identity.TargetSchema) ||
			batch.SourceExactSHA != identity.SourceExactSHA || batch.ImageDigest != identity.BinaryImageDigest {
			return "", fmt.Errorf("passed Phase 7A ledger %s differs from current data", operation)
		}
		return batch.PublicID, nil
	}
	if err != nil {
		return "", err
	}
	invariants.SourceCount, invariants.TargetCount = sourceCount, targetCount
	invariants.SourceHash, invariants.TargetHash = sourceHash, targetHash
	invariants.RequireParity, invariants.Summary = true, summary
	batch, err = finishLedgerBatch(ctx, tx, batch, invariants, at)
	if err != nil {
		return "", err
	}
	return batch.PublicID, nil
}

func hashStringColumn(ctx context.Context, tx *sql.Tx, query string, args ...any) (string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			return "", err
		}
		if !value.Valid || !isSHA256(value.String) {
			return "", errors.New("canonical hash input is missing")
		}
		values = append(values, value.String)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return canonicalHashSet(values), nil
}

func hashLatestConversions(ctx context.Context, tx *sql.Tx) (string, error) {
	return hashLatestConversionsWhere(ctx, tx, "")
}

func hashLegacyExternalArtifacts(ctx context.Context, tx *sql.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT source_change_request_id,source_content_hash,repository,pr_number,pr_url,
base_revision,head_branch,head_revision,pr_state,merged_commit_sha,external_state,conversion_status,reason_code
FROM legacy_change_request_archive ORDER BY source_change_request_id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var sourceID uint64
		var sourceHash, repository, prURL, base, branch, head, state, merged, externalState, status, reason string
		var prNumber int64
		if err := rows.Scan(&sourceID, &sourceHash, &repository, &prNumber, &prURL, &base, &branch, &head,
			&state, &merged, &externalState, &status, &reason); err != nil {
			return "", err
		}
		if !isSHA256(sourceHash) {
			return "", errors.New("legacy external artifact source hash is missing")
		}
		values = append(values, canonicalHashFields("legacy-external-artifact-archive/v1", fmt.Sprint(sourceID),
			sourceHash, repository, fmt.Sprint(prNumber), prURL, base, branch, head, state, merged,
			externalState, status, reason))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return canonicalHashSet(values), nil
}

func hashLatestConversionsWhere(ctx context.Context, tx *sql.Tx, condition string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.subject_type,r.subject_id,r.converter_version,r.input_hash,r.output_hash,
r.status,r.reason_code,COALESCE(r.target_task_id,0),r.anti_join_result`+latestConversionBaseSQL+condition+` ORDER BY r.subject_type,r.subject_id,r.converter_version`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var subjectType, converter, inputHash, outputHash, status, reason, antiJoin string
		var subjectID, taskID uint64
		if err := rows.Scan(&subjectType, &subjectID, &converter, &inputHash, &outputHash, &status, &reason, &taskID, &antiJoin); err != nil {
			return "", err
		}
		values = append(values, canonicalHashFields(subjectType, fmt.Sprint(subjectID), converter, inputHash,
			outputHash, status, reason, fmt.Sprint(taskID), antiJoin))
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return canonicalHashSet(values), nil
}

func recordPreparationFailure(ctx context.Context, db *sql.DB, lockTimeout time.Duration, identity ReleaseIdentity, cause error) error {
	if db == nil || cause == nil {
		return nil
	}
	conn, err := db.Conn(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(context.WithoutCancel(ctx), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	batch, err := beginLedgerBatch(context.WithoutCancel(ctx), tx, identity, "convert", ConverterAuditOperation,
		"legacy_cutover_inputs", "phase7a_preparation", 1, nil, nil, phase7AConverter, time.Now().UTC())
	if errors.Is(err, ErrLedgerAlreadyPassed) {
		batch, err = beginLedgerRetryAfterPassed(context.WithoutCancel(ctx), tx, identity, batch,
			"convert", ConverterAuditOperation, "legacy_cutover_inputs", "phase7a_preparation",
			1, nil, nil, phase7AConverter, time.Now().UTC())
	}
	if err != nil {
		return err
	}
	failedHash := canonicalHashFields("phase7a-prepare-failure/v1", cause.Error())
	_, finishErr := finishLedgerBatch(context.WithoutCancel(ctx), tx, batch, LedgerCompletion{
		SourceCount: 1, TargetCount: 0, RejectedCount: 1, SourceHash: failedHash,
		TargetHash: canonicalHashSet(nil), RequireParity: true, ReasonCode: "phase7a_prepare_failed",
		Summary: boundSummary("Phase 7A preparation failed closed: " + cause.Error()),
	}, time.Now().UTC())
	if commitErr := tx.Commit(); commitErr != nil {
		return errors.Join(finishErr, commitErr)
	}
	return nil
}

func countKeysWithPrefix(values map[string]uint64, prefix string) uint64 {
	var total uint64
	for key, value := range values {
		if strings.HasPrefix(key, prefix) {
			total += value
		}
	}
	return total
}
