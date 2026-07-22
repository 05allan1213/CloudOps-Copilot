-- +goose Up
-- +goose NO TRANSACTION

-- Phase 7A Release A remains forward-only.  This migration adds the durable
-- batch, archive, converter, and provenance contracts required to prepare a
-- cutover.  It deliberately does not write CUTOVER-V3, delete a legacy table,
-- remove a legacy lease, or enable a V3-only runtime.

ALTER TABLE migration_ledger
    ADD COLUMN canonical_hash_version SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER target_hash,
    ADD COLUMN release_identity_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER canonical_hash_version,
    ADD KEY idx_migration_ledger_unit_batch (plan_version, operation, batch_no, attempt, id),
    ADD CONSTRAINT chk_migration_ledger_release_hash CHECK (
        release_identity_hash IS NULL OR release_identity_hash REGEXP '^[0-9a-f]{64}$'
    );

CREATE TABLE migration_backfill_cursors (
    plan_version INT UNSIGNED NOT NULL,
    operation VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_table VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_table VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    next_batch_no BIGINT UNSIGNED NOT NULL,
    last_source_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_exact_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    binary_image_digest VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (plan_version, operation),
    CONSTRAINT chk_migration_backfill_cursor CHECK (
        plan_version > 0 AND next_batch_no > 0
        AND status IN ('running','passed','failed')
        AND CHAR_LENGTH(source_exact_sha) BETWEEN 40 AND 64
        AND binary_image_digest REGEXP '^sha256:[0-9a-f]{64}$'
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_signal_archive (
    source_signal_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_schema_version SMALLINT UNSIGNED NOT NULL,
    target_schema_version SMALLINT UNSIGNED NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_signal_id),
    KEY idx_legacy_signal_archive_incident (incident_id, cycle_no, source_signal_id),
    CONSTRAINT chk_legacy_signal_archive_status CHECK (conversion_status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_signal_archive_hashes CHECK (
        source_hash REGEXP '^[0-9a-f]{64}$'
        AND (target_hash IS NULL OR target_hash REGEXP '^[0-9a-f]{64}$')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_event_archive (
    source_event_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_schema_version SMALLINT UNSIGNED NOT NULL,
    target_schema_version SMALLINT UNSIGNED NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_event_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_event_id),
    KEY idx_legacy_event_archive_incident (incident_id, cycle_no, source_event_id),
    CONSTRAINT chk_legacy_event_archive_status CHECK (conversion_status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_event_archive_hashes CHECK (
        source_hash REGEXP '^[0-9a-f]{64}$'
        AND (target_hash IS NULL OR target_hash REGEXP '^[0-9a-f]{64}$')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_evidence_archive (
    source_evidence_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_schema_version SMALLINT UNSIGNED NOT NULL,
    target_schema_version SMALLINT UNSIGNED NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_evidence_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_evidence_id),
    KEY idx_legacy_evidence_archive_incident (incident_id, cycle_no, source_evidence_id),
    CONSTRAINT chk_legacy_evidence_archive_status CHECK (conversion_status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_evidence_archive_hashes CHECK (
        source_hash REGEXP '^[0-9a-f]{64}$'
        AND (target_hash IS NULL OR target_hash REGEXP '^[0-9a-f]{64}$')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_agent_step_archive (
    source_agent_step_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_schema_version SMALLINT UNSIGNED NOT NULL,
    target_schema_version SMALLINT UNSIGNED NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_agent_step_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_agent_step_id),
    KEY idx_legacy_agent_step_archive_incident (incident_id, cycle_no, source_agent_step_id),
    CONSTRAINT chk_legacy_agent_step_archive_status CHECK (conversion_status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_agent_step_archive_hashes CHECK (
        source_hash REGEXP '^[0-9a-f]{64}$'
        AND (target_hash IS NULL OR target_hash REGEXP '^[0-9a-f]{64}$')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_change_candidate_archive (
    source_change_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_schema_version SMALLINT UNSIGNED NOT NULL,
    target_schema_version SMALLINT UNSIGNED NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_candidate_id BIGINT UNSIGNED NULL,
    target_assessment_id BIGINT UNSIGNED NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_change_id),
    KEY idx_legacy_change_candidate_incident (incident_id, cycle_no, source_change_id),
    CONSTRAINT chk_legacy_change_candidate_status CHECK (conversion_status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_change_candidate_hashes CHECK (
        source_hash REGEXP '^[0-9a-f]{64}$'
        AND (target_hash IS NULL OR target_hash REGEXP '^[0-9a-f]{64}$')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_change_assessment_archive (
    source_change_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_schema_version SMALLINT UNSIGNED NOT NULL,
    target_schema_version SMALLINT UNSIGNED NOT NULL,
    assessment_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    assessment_snapshot_json JSON NOT NULL,
    source_change_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    assessment_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_change_id),
    KEY idx_legacy_change_assessment_incident (incident_id, cycle_no, source_change_id),
    CONSTRAINT chk_legacy_change_assessment_status CHECK (assessment_status IN ('matched','excluded','unknown')),
    CONSTRAINT chk_legacy_change_assessment_conversion CHECK (conversion_status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_change_assessment_hashes CHECK (
        source_change_hash REGEXP '^[0-9a-f]{64}$'
        AND assessment_hash REGEXP '^[0-9a-f]{64}$'
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_remediation_plan_archive (
    source_plan_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    source_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    converter_result VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    source_updated_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_plan_id),
    KEY idx_legacy_plan_archive_incident (incident_id, cycle_no, source_plan_id),
    CONSTRAINT chk_legacy_plan_archive_result CHECK (converter_result IN ('archived','superseded','cancelled','failed')),
    CONSTRAINT chk_legacy_plan_archive_hash CHECK (source_content_hash REGEXP '^[0-9a-f]{64}$')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_approval_archive (
    source_approval_id BIGINT UNSIGNED NOT NULL,
    source_plan_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    decision VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor VARCHAR(128) NOT NULL,
    source_snapshot_json JSON NOT NULL,
    source_content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    converter_result VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_approval_id),
    UNIQUE KEY uk_legacy_approval_archive_plan (source_plan_id),
    KEY idx_legacy_approval_archive_incident (incident_id, cycle_no, source_approval_id),
    CONSTRAINT chk_legacy_approval_archive_result CHECK (converter_result IN ('archived','rejected','non_authoritative')),
    CONSTRAINT chk_legacy_approval_archive_hash CHECK (source_content_hash REGEXP '^[0-9a-f]{64}$')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_outbox_event_registry (
    registry_version SMALLINT UNSIGNED NOT NULL,
    event_type VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    schema_version INT UNSIGNED NOT NULL,
    aggregate_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    archive_mapper VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    external_write_event BOOLEAN NOT NULL,
    fixture_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (registry_version, event_type, schema_version),
    CONSTRAINT chk_legacy_outbox_registry CHECK (
        registry_version > 0 AND schema_version > 0
        AND CHAR_LENGTH(TRIM(event_type)) > 0
        AND CHAR_LENGTH(TRIM(aggregate_type)) > 0
        AND CHAR_LENGTH(TRIM(archive_mapper)) > 0
        AND fixture_hash REGEXP '^[0-9a-f]{64}$'
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE legacy_outbox_archive
    ADD COLUMN source_schema_version SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER source_outbox_id,
    ADD COLUMN row_snapshot_json JSON NULL AFTER payload_json,
    ADD COLUMN row_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER row_snapshot_json,
    ADD COLUMN registry_version SMALLINT UNSIGNED NULL AFTER row_hash,
    ADD COLUMN conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'passed' AFTER registry_version,
    ADD COLUMN reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'archived' AFTER conversion_status,
    ADD CONSTRAINT chk_legacy_outbox_archive_row_hash CHECK (
        row_hash IS NULL OR row_hash REGEXP '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT chk_legacy_outbox_archive_conversion CHECK (
        conversion_status IN ('passed','failed')
    );

ALTER TABLE legacy_agent_checkpoint_archive
    ADD COLUMN source_checkpoint_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER source_schema_version,
    ADD COLUMN source_checkpoint_canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER checkpoint_hash,
    ADD COLUMN target_schema_version INT UNSIGNED NULL AFTER source_checkpoint_canonical_hash,
    ADD COLUMN target_checkpoint_version BIGINT UNSIGNED NULL AFTER target_schema_version,
    ADD COLUMN target_checkpoint_json JSON NULL AFTER target_checkpoint_version,
    ADD COLUMN target_checkpoint_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_checkpoint_json,
    ADD COLUMN converter_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'agent-checkpoint/v2' AFTER target_checkpoint_hash,
    ADD COLUMN source_created_at DATETIME(6) NULL AFTER reason_code,
    ADD COLUMN source_updated_at DATETIME(6) NULL AFTER source_created_at,
    ADD CONSTRAINT chk_legacy_agent_target_hash CHECK (
        (source_checkpoint_canonical_hash IS NULL OR source_checkpoint_canonical_hash REGEXP '^[0-9a-f]{64}$')
        AND (target_checkpoint_hash IS NULL OR target_checkpoint_hash REGEXP '^[0-9a-f]{64}$')
    );

ALTER TABLE legacy_change_request_archive
    ADD COLUMN source_content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER snapshot_hash,
    ADD COLUMN repository VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER source_content_hash,
    ADD COLUMN pr_number BIGINT NOT NULL DEFAULT 0 AFTER repository,
    ADD COLUMN pr_url VARCHAR(1024) NOT NULL DEFAULT '' AFTER pr_number,
    ADD COLUMN base_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER pr_url,
    ADD COLUMN head_branch VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER base_revision,
    ADD COLUMN head_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER head_branch,
    ADD COLUMN pr_state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER head_revision,
    ADD COLUMN merged_commit_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER pr_state,
    ADD COLUMN source_created_at DATETIME(6) NULL AFTER reason_code,
    ADD COLUMN source_updated_at DATETIME(6) NULL AFTER source_created_at,
    ADD CONSTRAINT chk_legacy_change_source_hash CHECK (
        source_content_hash IS NULL OR source_content_hash REGEXP '^[0-9a-f]{64}$'
    );

ALTER TABLE legacy_verification_archive
    ADD COLUMN source_schema_version SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER source_status,
    ADD COLUMN target_schema_version SMALLINT UNSIGNED NULL AFTER source_schema_version,
    ADD COLUMN trigger_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_schema_version,
    ADD COLUMN target_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER trigger_type,
    ADD COLUMN source_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_revision,
    ADD COLUMN image_digest VARCHAR(71) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER source_revision,
    ADD COLUMN gitops_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER image_digest,
    ADD COLUMN source_content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER profile_hash,
    ADD COLUMN checks_json JSON NULL AFTER profile_hash,
    ADD COLUMN samples_json JSON NULL AFTER checks_json,
    ADD COLUMN output_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER samples_json,
    ADD COLUMN converter_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'verification-profile/v2' AFTER output_hash,
    ADD COLUMN source_created_at DATETIME(6) NULL AFTER reason_code,
    ADD COLUMN source_updated_at DATETIME(6) NULL AFTER source_created_at,
    ADD CONSTRAINT chk_legacy_verification_output_hash CHECK (
        (source_content_hash IS NULL OR source_content_hash REGEXP '^[0-9a-f]{64}$')
        AND (output_hash IS NULL OR output_hash REGEXP '^[0-9a-f]{64}$')
    );

ALTER TABLE legacy_conversion_records
    DROP INDEX uk_legacy_conversion_subject,
    ADD COLUMN attempt INT UNSIGNED NOT NULL DEFAULT 1 AFTER converter_version,
    ADD COLUMN previous_conversion_id BIGINT UNSIGNED NULL AFTER attempt,
    ADD COLUMN source_schema_version SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER previous_conversion_id,
    ADD COLUMN target_schema_version SMALLINT UNSIGNED NOT NULL DEFAULT 3 AFTER source_schema_version,
    ADD COLUMN source_table VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'legacy' AFTER target_schema_version,
    ADD COLUMN target_table VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'v3' AFTER source_table,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT TRUE AFTER anti_join_result,
    ADD UNIQUE KEY uk_legacy_conversion_subject_attempt (subject_type, subject_id, converter_version, attempt),
    ADD KEY idx_legacy_conversion_previous (previous_conversion_id, id),
    ADD CONSTRAINT fk_legacy_conversion_previous FOREIGN KEY (previous_conversion_id) REFERENCES legacy_conversion_records (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_legacy_conversion_attempt CHECK (
        (attempt = 1 AND previous_conversion_id IS NULL)
        OR (attempt > 1 AND previous_conversion_id IS NOT NULL)
    );

ALTER TABLE incidents
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE agent_runs ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE evidence_items ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE change_requests ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE incident_signals
    ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;

ALTER TABLE incident_events
    ADD COLUMN source_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER event_type,
    ADD COLUMN target_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER source_status,
    ADD COLUMN reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_status,
    ADD COLUMN converter_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER reason_code,
    ADD COLUMN conversion_input_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER converter_version,
    ADD COLUMN conversion_output_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER conversion_input_hash,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER conversion_output_hash,
    ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy_context,
    ADD CONSTRAINT chk_incident_events_conversion_hashes CHECK (
        (conversion_input_hash IS NULL OR conversion_input_hash REGEXP '^[0-9a-f]{64}$')
        AND (conversion_output_hash IS NULL OR conversion_output_hash REGEXP '^[0-9a-f]{64}$')
    );

ALTER TABLE agent_steps ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no;
ALTER TABLE changes
    ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE remediation_plans
    ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER v3_status,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE remediation_approvals ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no;
ALTER TABLE change_candidates ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no;
ALTER TABLE change_candidate_assessments ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no;
ALTER TABLE verification_checks
    ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE verification_samples
    ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no,
    ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE verification_runs ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE async_tasks ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER migrated_legacy;
ALTER TABLE resolution_reports ADD COLUMN migrated_legacy_context BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no;
