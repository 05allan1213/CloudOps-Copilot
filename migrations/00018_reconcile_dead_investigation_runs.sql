-- +goose Up
-- +goose NO TRANSACTION

-- Repair V3 AgentRuns left active by an investigation.step task that reached
-- dead before older workers could persist the corresponding Run failure. A
-- current technical replay prevents reconciliation and remains authoritative.
INSERT IGNORE INTO incident_events
    (public_id, incident_id, domain_schema_version, cycle_no, event_schema_version,
     event_type, idempotency_key, migrated_legacy_context, migrated_legacy,
     actor_type, actor_id, summary, metadata_json, occurred_at, created_at)
SELECT UUID(), r.incident_id, 3, r.cycle_no, 1,
       'agent_run_failed',
       SHA2(CONCAT('agent-run-dead-reconcile:', r.public_id, ':', dead.public_id), 256),
       r.migrated_legacy_context, r.migrated_legacy,
       'system', 'migration-00018',
       'investigation AgentRun reconciled after terminal task',
       JSON_OBJECT(
           'agent_run_id', r.public_id,
           'task_public_id', dead.public_id,
           'reason', COALESCE(NULLIF(dead.last_error_code, ''), 'investigation_task_dead'),
           'reconciled_by', 'migration-00018'
       ),
       COALESCE(dead.dead_at, NOW(6)), NOW(6)
FROM agent_runs r
JOIN async_tasks dead
  ON dead.id = (
      SELECT candidate.id
      FROM async_tasks candidate
      WHERE candidate.subject_type = 'agent_run'
        AND candidate.subject_id = r.id
        AND candidate.transition = 'investigation.step'
        AND candidate.status = 'dead'
        AND candidate.expected_subject_version = r.row_version
      ORDER BY candidate.replay_generation DESC, candidate.id DESC
      LIMIT 1
  )
WHERE r.domain_schema_version = 3
  AND r.status IN ('PENDING', 'RUNNING')
  AND r.v3_status IN ('pending', 'running')
  AND NOT EXISTS (
      SELECT 1
      FROM async_tasks live
      WHERE live.subject_type = 'agent_run'
        AND live.subject_id = r.id
        AND live.transition = 'investigation.step'
        AND live.status IN ('ready', 'running')
        AND live.expected_subject_version = r.row_version
  );

UPDATE agent_runs r
JOIN async_tasks dead
  ON dead.id = (
      SELECT candidate.id
      FROM async_tasks candidate
      WHERE candidate.subject_type = 'agent_run'
        AND candidate.subject_id = r.id
        AND candidate.transition = 'investigation.step'
        AND candidate.status = 'dead'
        AND candidate.expected_subject_version = r.row_version
      ORDER BY candidate.replay_generation DESC, candidate.id DESC
      LIMIT 1
  )
SET r.status = 'FAILED',
    r.v3_status = 'failed',
    r.failure_code = COALESCE(NULLIF(dead.last_error_code, ''), 'investigation_task_dead'),
    r.failure_summary = COALESCE(
        NULLIF(dead.last_error_summary, ''),
        'investigation task entered dead state before the AgentRun reached a terminal state'
    ),
    r.completed_at = COALESCE(r.completed_at, dead.dead_at, NOW(6)),
    r.row_version = r.row_version + 1,
    r.updated_at = NOW(6)
WHERE r.domain_schema_version = 3
  AND r.status IN ('PENDING', 'RUNNING')
  AND r.v3_status IN ('pending', 'running')
  AND NOT EXISTS (
      SELECT 1
      FROM async_tasks live
      WHERE live.subject_type = 'agent_run'
        AND live.subject_id = r.id
        AND live.transition = 'investigation.step'
        AND live.status IN ('ready', 'running')
        AND live.expected_subject_version = r.row_version
  );
