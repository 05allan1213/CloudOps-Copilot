-- +goose Up
-- +goose NO TRANSACTION

-- Phase 7A Release A is archive/conversion only.  These tables deliberately
-- retain legacy source identifiers and canonical hashes; no legacy table or
-- lease column is removed by this migration.

CREATE TABLE cutover_controls (
    control_name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    plan_version INT UNSIGNED NOT NULL,
    source_exact_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    binary_image_digest VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ingress_quiesced BOOLEAN NOT NULL,
    mutations_quiesced BOOLEAN NOT NULL,
    legacy_workers_quiesced BOOLEAN NOT NULL,
    observed_ingress_writers BIGINT UNSIGNED NOT NULL,
    observed_mutation_writers BIGINT UNSIGNED NOT NULL,
    observed_legacy_workers BIGINT UNSIGNED NOT NULL,
    observed_unknown_external_writes BIGINT UNSIGNED NOT NULL,
    prepared_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (control_name),
    CONSTRAINT chk_cutover_controls_identity CHECK (
        plan_version > 0 AND CHAR_LENGTH(source_exact_sha) BETWEEN 40 AND 64
        AND binary_image_digest REGEXP '^sha256:[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_cutover_controls_quiesce CHECK (
        completed_at IS NULL OR (
            ingress_quiesced AND mutations_quiesced AND legacy_workers_quiesced
            AND observed_ingress_writers = 0 AND observed_mutation_writers = 0
            AND observed_legacy_workers = 0 AND observed_unknown_external_writes = 0
        )
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_outbox_archive (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_outbox_id BIGINT UNSIGNED NOT NULL,
    event_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id VARCHAR(128) NOT NULL,
    event_type VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    schema_version INT UNSIGNED NOT NULL,
    publication_state VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    payload_json JSON NOT NULL,
    payload_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    published_at DATETIME(6) NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_legacy_outbox_archive_source (source_outbox_id),
    UNIQUE KEY uk_legacy_outbox_archive_event (event_id),
    KEY idx_legacy_outbox_archive_type (event_type, schema_version, publication_state, id),
    CONSTRAINT chk_legacy_outbox_archive_state CHECK (publication_state IN ('published','unpublished')),
    CONSTRAINT chk_legacy_outbox_archive_hash CHECK (CHAR_LENGTH(payload_hash) = 64)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_incident_state_archive (
    source_incident_id BIGINT UNSIGNED NOT NULL,
    incident_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_version BIGINT UNSIGNED NOT NULL,
    snapshot_json JSON NOT NULL,
    snapshot_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_incident_id),
    UNIQUE KEY uk_legacy_incident_archive_public (incident_public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_agent_checkpoint_archive (
    source_agent_run_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    source_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_schema_version INT UNSIGNED NOT NULL,
    checkpoint_json JSON NULL,
    checkpoint_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_agent_run_id),
    KEY idx_legacy_agent_archive_incident (incident_id, source_agent_run_id),
    CONSTRAINT chk_legacy_agent_archive_status CHECK (conversion_status IN ('passed','failed','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_change_request_archive (
    source_change_request_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    source_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    snapshot_json JSON NOT NULL,
    snapshot_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    external_state VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_change_request_id),
    KEY idx_legacy_change_archive_incident (incident_id, source_change_request_id),
    CONSTRAINT chk_legacy_change_archive_status CHECK (conversion_status IN ('passed','failed','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_verification_archive (
    source_verification_run_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    source_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    profile_json JSON NOT NULL,
    profile_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    conversion_status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_verification_run_id),
    KEY idx_legacy_verification_archive_incident (incident_id, source_verification_run_id),
    CONSTRAINT chk_legacy_verification_archive_status CHECK (conversion_status IN ('passed','failed','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_postmortem_archive (
    source_postmortem_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    source_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content_json JSON NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    generated_at DATETIME(6) NOT NULL,
    archived_at DATETIME(6) NOT NULL,
    PRIMARY KEY (source_postmortem_id),
    UNIQUE KEY uk_legacy_postmortem_archive_public (source_public_id),
    KEY idx_legacy_postmortem_archive_incident (incident_id, source_postmortem_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE legacy_conversion_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    subject_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    subject_id BIGINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    converter_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    input_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    output_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_task_id BIGINT UNSIGNED NULL,
    anti_join_result VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_legacy_conversion_subject (subject_type, subject_id, converter_version),
    UNIQUE KEY uk_legacy_conversion_public (public_id),
    KEY idx_legacy_conversion_incident (incident_id, cycle_no, id),
    CONSTRAINT fk_legacy_conversion_task FOREIGN KEY (target_task_id) REFERENCES async_tasks (id) ON DELETE RESTRICT,
    CONSTRAINT chk_legacy_conversion_status CHECK (status IN ('passed','failed','skipped')),
    CONSTRAINT chk_legacy_conversion_antijoin CHECK (anti_join_result IN ('created','existing-target-task','anti-join-skipped','not-applicable'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE incidents ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER blocking_reason_code;
ALTER TABLE agent_runs ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER expected_incident_version;
ALTER TABLE evidence_items ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER valid;
ALTER TABLE change_requests ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER expected_subject_version;
ALTER TABLE verification_runs ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER expected_subject_version;
ALTER TABLE async_tasks ADD COLUMN migrated_legacy BOOLEAN NOT NULL DEFAULT FALSE AFTER logical_operation_key;
