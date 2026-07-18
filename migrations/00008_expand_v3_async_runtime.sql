-- +goose Up
-- +goose NO TRANSACTION

-- Phase 2 is expand-only. Legacy status, lease, checkpoint and outbox fields
-- remain untouched. A NULL domain_schema_version identifies an unconverted
-- legacy row; only rows explicitly written with domain_schema_version = 3
-- participate in V3 generated-key constraints.

ALTER TABLE incidents
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER version,
    ADD COLUMN v3_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER domain_schema_version,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER v3_status,
    ADD COLUMN correlation_key_version SMALLINT UNSIGNED NULL AFTER correlation_key,
    ADD COLUMN needs_attention BOOLEAN NOT NULL DEFAULT FALSE AFTER cycle_no,
    ADD COLUMN blocking_reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER needs_attention,
    ADD COLUMN blocked_at DATETIME(6) NULL AFTER blocking_reason_code,
    ADD COLUMN terminal_at DATETIME(6) NULL AFTER resolved_at,
    ADD COLUMN active_correlation_key VARBINARY(67) GENERATED ALWAYS AS (
        CASE
            WHEN domain_schema_version = 3
             AND v3_status IN ('detected','investigating','awaiting_approval','delivering','verifying')
            THEN CONVERT(correlation_key USING binary)
            ELSE NULL
        END
    ) STORED,
    ADD UNIQUE KEY uk_incidents_v3_active_correlation (active_correlation_key),
    ADD KEY idx_incidents_v3_status_updated (domain_schema_version, v3_status, updated_at, id),
    ADD KEY idx_incidents_v3_terminal_correlation (correlation_key, terminal_at, id),
    ADD CONSTRAINT chk_incidents_v3_identity CHECK (
        (domain_schema_version IS NULL AND v3_status IS NULL AND cycle_no IS NULL AND correlation_key_version IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND v3_status IS NOT NULL AND cycle_no IS NOT NULL AND cycle_no > 0
         AND correlation_key_version IS NOT NULL AND correlation_key_version > 0)
    ),
    ADD CONSTRAINT chk_incidents_v3_status CHECK (
        v3_status IS NULL OR v3_status IN (
            'detected','investigating','awaiting_approval','delivering','verifying','resolved','closed'
        )
    ),
    ADD CONSTRAINT chk_incidents_v3_terminal CHECK (
        v3_status IS NULL
        OR (v3_status = 'resolved' AND resolved_at IS NOT NULL AND terminal_at IS NOT NULL)
        OR (v3_status = 'closed' AND terminal_at IS NOT NULL)
        OR (v3_status NOT IN ('resolved','closed') AND resolved_at IS NULL AND terminal_at IS NULL)
    );

ALTER TABLE incident_signals
    ADD COLUMN public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER id,
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN canonical_schema_version SMALLINT UNSIGNED NULL AFTER source_event_id,
    ADD COLUMN correlation_key_version SMALLINT UNSIGNED NULL AFTER canonical_schema_version,
    ADD COLUMN alert_instance_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER fingerprint,
    ADD COLUMN starts_at DATETIME(6) NULL AFTER occurred_at,
    ADD COLUMN ends_at DATETIME(6) NULL AFTER starts_at,
    ADD UNIQUE KEY uk_incident_signals_v3_public_id (public_id),
    ADD KEY idx_incident_signals_v3_cycle_instance (incident_id, cycle_no, alert_instance_key, status, id),
    ADD KEY idx_incident_signals_v3_source_instance (source, alert_instance_key, starts_at, id),
    ADD CONSTRAINT chk_incident_signals_v3_identity CHECK (
        (domain_schema_version IS NULL AND cycle_no IS NULL AND canonical_schema_version IS NULL AND correlation_key_version IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND cycle_no IS NOT NULL AND cycle_no > 0
         AND canonical_schema_version IS NOT NULL AND canonical_schema_version > 0
         AND correlation_key_version IS NOT NULL AND correlation_key_version > 0
         AND public_id IS NOT NULL AND alert_instance_key IS NOT NULL
         AND CHAR_LENGTH(alert_instance_key) = 64)
    ),
    ADD CONSTRAINT chk_incident_signals_v3_times CHECK (
        starts_at IS NULL OR ends_at IS NULL OR ends_at >= starts_at
    );

ALTER TABLE incident_events
    ADD COLUMN public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER id,
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN event_schema_version SMALLINT UNSIGNED NULL AFTER cycle_no,
    ADD UNIQUE KEY uk_incident_events_v3_public_id (public_id),
    ADD KEY idx_incident_events_v3_cycle (incident_id, cycle_no, occurred_at, id),
    ADD CONSTRAINT chk_incident_events_v3_identity CHECK (
        (domain_schema_version IS NULL AND cycle_no IS NULL AND event_schema_version IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND cycle_no IS NOT NULL AND cycle_no > 0
         AND event_schema_version IS NOT NULL AND event_schema_version > 0 AND public_id IS NOT NULL)
    ),
    ADD CONSTRAINT chk_incident_events_v3_metadata_size CHECK (
        JSON_STORAGE_SIZE(metadata_json) <= 8192
    );

ALTER TABLE agent_runs
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN v3_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER status,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN expected_incident_version BIGINT UNSIGNED NULL AFTER cycle_no,
    ADD COLUMN active_incident_cycle_key VARBINARY(17) GENERATED ALWAYS AS (
        CASE
            WHEN domain_schema_version = 3 AND v3_status IN ('pending','running') AND cycle_no IS NOT NULL
            THEN UNHEX(CONCAT('01', LPAD(HEX(incident_id), 16, '0'), LPAD(HEX(cycle_no), 16, '0')))
            ELSE NULL
        END
    ) STORED,
    ADD UNIQUE KEY uk_agent_runs_v3_active_cycle (active_incident_cycle_key),
    ADD KEY idx_agent_runs_v3_incident_cycle (incident_id, cycle_no, v3_status, id),
    ADD CONSTRAINT chk_agent_runs_v3_identity CHECK (
        (domain_schema_version IS NULL AND v3_status IS NULL AND cycle_no IS NULL AND expected_incident_version IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND v3_status IS NOT NULL AND cycle_no IS NOT NULL AND cycle_no > 0
         AND expected_incident_version IS NOT NULL AND expected_incident_version > 0)
    ),
    ADD CONSTRAINT chk_agent_runs_v3_status CHECK (
        v3_status IS NULL OR v3_status IN ('pending','running','completed','failed','cancelled')
    );

ALTER TABLE agent_steps
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER agent_run_id,
    ADD COLUMN incident_id BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER incident_id,
    ADD KEY idx_agent_steps_v3_incident_cycle (incident_id, cycle_no, sequence, id),
    ADD CONSTRAINT fk_agent_steps_v3_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_agent_steps_v3_identity CHECK (
        (domain_schema_version IS NULL AND incident_id IS NULL AND cycle_no IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND incident_id IS NOT NULL AND cycle_no IS NOT NULL AND cycle_no > 0)
    );

ALTER TABLE evidence_items
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN producer_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER source,
    ADD COLUMN producer_dedupe_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER producer_type,
    ADD COLUMN content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER result_hash,
    ADD UNIQUE KEY uk_evidence_items_v3_producer (
        incident_id, cycle_no, producer_type, producer_dedupe_key, content_hash
    ),
    ADD KEY idx_evidence_items_v3_cycle_collected (incident_id, cycle_no, collected_at, id),
    ADD CONSTRAINT chk_evidence_items_v3_identity CHECK (
        (domain_schema_version IS NULL AND cycle_no IS NULL AND producer_type IS NULL
         AND producer_dedupe_key IS NULL AND content_hash IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND cycle_no IS NOT NULL AND cycle_no > 0 AND producer_type IS NOT NULL
         AND producer_dedupe_key IS NOT NULL AND content_hash IS NOT NULL
         AND CHAR_LENGTH(content_hash) = 64)
    );

ALTER TABLE changes
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD KEY idx_changes_v3_incident_cycle (incident_id, cycle_no, created_at, id),
    ADD CONSTRAINT chk_changes_v3_identity CHECK (
        (domain_schema_version IS NULL AND cycle_no IS NULL)
        OR (domain_schema_version IS NOT NULL AND domain_schema_version = 3
            AND cycle_no IS NOT NULL AND cycle_no > 0)
    );

ALTER TABLE remediation_plans
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN v3_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER status,
    ADD COLUMN hash_schema_version SMALLINT UNSIGNED NULL AFTER plan_hash,
    ADD COLUMN canonical_plan_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER hash_schema_version,
    ADD COLUMN verification_plan_json JSON NULL AFTER validation_plan,
    ADD COLUMN active_incident_cycle_key VARBINARY(17) GENERATED ALWAYS AS (
        CASE
            WHEN domain_schema_version = 3 AND v3_status IN ('awaiting_approval','approved') AND cycle_no IS NOT NULL
            THEN UNHEX(CONCAT('01', LPAD(HEX(incident_id), 16, '0'), LPAD(HEX(cycle_no), 16, '0')))
            ELSE NULL
        END
    ) STORED,
    ADD UNIQUE KEY uk_remediation_plans_v3_active_cycle (active_incident_cycle_key),
    ADD KEY idx_remediation_plans_v3_incident_cycle (incident_id, cycle_no, v3_status, id),
    ADD CONSTRAINT chk_remediation_plans_v3_identity CHECK (
        (domain_schema_version IS NULL AND cycle_no IS NULL AND v3_status IS NULL
         AND hash_schema_version IS NULL AND canonical_plan_hash IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND cycle_no IS NOT NULL AND cycle_no > 0 AND v3_status IS NOT NULL
         AND hash_schema_version IS NOT NULL AND hash_schema_version > 0
         AND canonical_plan_hash IS NOT NULL AND CHAR_LENGTH(canonical_plan_hash) = 64)
    ),
    ADD CONSTRAINT chk_remediation_plans_v3_status CHECK (
        v3_status IS NULL OR v3_status IN (
            'awaiting_approval','approved','rejected','superseded','cancelled','consumed','invalidated','policy_rejected'
        )
    ),
    ADD CONSTRAINT chk_remediation_plans_v3_payload_size CHECK (
        verification_plan_json IS NULL OR JSON_STORAGE_SIZE(verification_plan_json) <= 16384
    );

ALTER TABLE remediation_approvals
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER plan_id,
    ADD COLUMN incident_id BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER incident_id,
    ADD KEY idx_remediation_approvals_v3_incident_cycle (incident_id, cycle_no, id),
    ADD CONSTRAINT fk_remediation_approvals_v3_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_remediation_approvals_v3_identity CHECK (
        (domain_schema_version IS NULL AND incident_id IS NULL AND cycle_no IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND incident_id IS NOT NULL AND cycle_no IS NOT NULL AND cycle_no > 0)
    );

ALTER TABLE change_requests
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER plan_id,
    ADD COLUMN incident_id BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN v3_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER status,
    ADD COLUMN write_phase VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER v3_status,
    ADD COLUMN expected_subject_version BIGINT UNSIGNED NULL AFTER row_version,
    ADD COLUMN logical_operation_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER idempotency_key,
    ADD COLUMN external_write_started_at DATETIME(6) NULL AFTER logical_operation_key,
    ADD COLUMN external_write_marker CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER external_write_started_at,
    ADD COLUMN active_incident_cycle_key VARBINARY(17) GENERATED ALWAYS AS (
        CASE
            WHEN domain_schema_version = 3
             AND v3_status IN ('pending','pr_open','merged','syncing','rolling_out')
             AND cycle_no IS NOT NULL
            THEN UNHEX(CONCAT('01', LPAD(HEX(incident_id), 16, '0'), LPAD(HEX(cycle_no), 16, '0')))
            ELSE NULL
        END
    ) STORED,
    ADD UNIQUE KEY uk_change_requests_v3_active_cycle (active_incident_cycle_key),
    ADD UNIQUE KEY uk_change_requests_v3_logical_operation (logical_operation_key),
    ADD KEY idx_change_requests_v3_incident_cycle (incident_id, cycle_no, v3_status, id),
    ADD CONSTRAINT fk_change_requests_v3_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_change_requests_v3_identity CHECK (
        (domain_schema_version IS NULL AND incident_id IS NULL AND cycle_no IS NULL AND v3_status IS NULL
         AND expected_subject_version IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND incident_id IS NOT NULL AND cycle_no IS NOT NULL AND cycle_no > 0
         AND v3_status IS NOT NULL AND expected_subject_version IS NOT NULL
         AND expected_subject_version > 0)
    ),
    ADD CONSTRAINT chk_change_requests_v3_status CHECK (
        v3_status IS NULL OR v3_status IN (
            'pending','pr_open','merged','syncing','rolling_out','delivered','failed','cancelled','superseded'
        )
    ),
    ADD CONSTRAINT chk_change_requests_v3_write_marker CHECK (
        external_write_started_at IS NULL OR external_write_marker IS NOT NULL
    );

ALTER TABLE verification_runs
    MODIFY COLUMN remediation_plan_id BIGINT UNSIGNED NULL,
    MODIFY COLUMN change_request_id BIGINT UNSIGNED NULL,
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER incident_id,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN v3_status VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER status,
    ADD COLUMN trigger_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER v3_status,
    ADD COLUMN trigger_signal_id BIGINT UNSIGNED NULL AFTER change_request_id,
    ADD COLUMN source_revision VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_revision,
    ADD COLUMN image_digest VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER source_revision,
    ADD COLUMN gitops_revision VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER image_digest,
    ADD COLUMN verification_profile_version SMALLINT UNSIGNED NULL AFTER plan_json,
    ADD COLUMN verification_profile_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER verification_profile_version,
    ADD COLUMN expected_subject_version BIGINT UNSIGNED NULL AFTER row_version,
    ADD COLUMN trigger_identity VARBINARY(10) GENERATED ALWAYS AS (
        CASE
            WHEN trigger_type = 'post_delivery' AND change_request_id IS NOT NULL
            THEN UNHEX(CONCAT('01', '01', LPAD(HEX(change_request_id), 16, '0')))
            WHEN trigger_type = 'no_change_signal' AND trigger_signal_id IS NOT NULL
            THEN UNHEX(CONCAT('01', '02', LPAD(HEX(trigger_signal_id), 16, '0')))
            ELSE NULL
        END
    ) STORED,
    ADD COLUMN active_incident_cycle_key VARBINARY(17) GENERATED ALWAYS AS (
        CASE
            WHEN domain_schema_version = 3 AND v3_status IN ('pending','running') AND cycle_no IS NOT NULL
            THEN UNHEX(CONCAT('01', LPAD(HEX(incident_id), 16, '0'), LPAD(HEX(cycle_no), 16, '0')))
            ELSE NULL
        END
    ) STORED,
    ADD UNIQUE KEY uk_verification_runs_v3_active_cycle (active_incident_cycle_key),
    ADD UNIQUE KEY uk_verification_runs_v3_trigger_attempt (
        incident_id, cycle_no, trigger_identity, attempt
    ),
    ADD KEY idx_verification_runs_v3_incident_cycle (incident_id, cycle_no, v3_status, id),
    ADD CONSTRAINT fk_verification_runs_v3_trigger_signal FOREIGN KEY (trigger_signal_id) REFERENCES incident_signals (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_verification_runs_v3_identity CHECK (
        (domain_schema_version IS NULL AND cycle_no IS NULL AND v3_status IS NULL
         AND trigger_type IS NULL AND trigger_signal_id IS NULL AND expected_subject_version IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND cycle_no IS NOT NULL AND cycle_no > 0 AND v3_status IS NOT NULL
         AND expected_subject_version IS NOT NULL AND expected_subject_version > 0
         AND verification_profile_version IS NOT NULL AND verification_profile_version > 0
         AND verification_profile_hash IS NOT NULL
         AND CHAR_LENGTH(verification_profile_hash) = 64)
    ),
    ADD CONSTRAINT chk_verification_runs_v3_status CHECK (
        v3_status IS NULL OR v3_status IN ('pending','running','passed','failed','inconclusive','timed_out','cancelled')
    ),
    ADD CONSTRAINT chk_verification_runs_v3_trigger CHECK (
        domain_schema_version IS NULL
        OR
        (domain_schema_version = 3 AND trigger_type IS NOT NULL AND (
            (trigger_type = 'post_delivery' AND change_request_id IS NOT NULL AND trigger_signal_id IS NULL)
            OR
            (trigger_type = 'no_change_signal' AND change_request_id IS NULL AND trigger_signal_id IS NOT NULL)
        ))
    );

ALTER TABLE verification_checks
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER verification_run_id,
    ADD COLUMN incident_id BIGINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN cycle_no BIGINT UNSIGNED NULL AFTER incident_id,
    ADD KEY idx_verification_checks_v3_incident_cycle (incident_id, cycle_no, status, id),
    ADD CONSTRAINT fk_verification_checks_v3_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_verification_checks_v3_identity CHECK (
        (domain_schema_version IS NULL AND incident_id IS NULL AND cycle_no IS NULL)
        OR
        (domain_schema_version IS NOT NULL AND domain_schema_version = 3
         AND incident_id IS NOT NULL AND cycle_no IS NOT NULL AND cycle_no > 0)
    );

ALTER TABLE incident_correlation_locks
    ADD COLUMN domain_schema_version SMALLINT UNSIGNED NULL AFTER correlation_key,
    ADD COLUMN correlation_key_version SMALLINT UNSIGNED NULL AFTER domain_schema_version,
    ADD KEY idx_incident_correlation_locks_v3 (domain_schema_version, correlation_key_version, correlation_key),
    ADD CONSTRAINT chk_incident_correlation_locks_v3_identity CHECK (
        (domain_schema_version IS NULL AND correlation_key_version IS NULL)
        OR (domain_schema_version IS NOT NULL AND domain_schema_version = 3
            AND correlation_key_version IS NOT NULL AND correlation_key_version > 0)
    );

CREATE TABLE signal_rejections (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_event_id VARCHAR(67) CHARACTER SET ascii COLLATE ascii_bin NULL,
    fingerprint VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    alert_instance_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    correlation_key VARCHAR(67) CHARACTER SET ascii COLLATE ascii_bin NULL,
    reason_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    dedupe_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    payload_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    details_json JSON NOT NULL,
    received_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_signal_rejections_public_id (public_id),
    UNIQUE KEY uk_signal_rejections_dedupe_key (dedupe_key),
    KEY idx_signal_rejections_reason_received (reason_code, received_at, id),
    KEY idx_signal_rejections_source_event (source, source_event_id, id),
    CONSTRAINT chk_signal_rejections_hashes CHECK (
        CHAR_LENGTH(dedupe_key) = 64 AND CHAR_LENGTH(payload_hash) = 64
        AND (alert_instance_key IS NULL OR CHAR_LENGTH(alert_instance_key) = 64)
    ),
    CONSTRAINT chk_signal_rejections_details_size CHECK (
        JSON_STORAGE_SIZE(details_json) <= 8192
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE command_idempotency_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_identity_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    command_scope VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    idempotency_key VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    http_status SMALLINT UNSIGNED NULL,
    response_json JSON NULL,
    resource_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    resource_public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    completed_at DATETIME(6) NULL,
    expires_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_command_idempotency_public_id (public_id),
    UNIQUE KEY uk_command_idempotency_actor_scope_key (actor_identity_hash, command_scope, idempotency_key),
    KEY idx_command_idempotency_cleanup (status, expires_at, id),
    CONSTRAINT chk_command_idempotency_hashes CHECK (
        CHAR_LENGTH(actor_identity_hash) = 64 AND CHAR_LENGTH(request_hash) = 64
    ),
    CONSTRAINT chk_command_idempotency_status CHECK (status IN ('processing','completed')),
    CONSTRAINT chk_command_idempotency_completion CHECK (
        (status = 'processing' AND completed_at IS NULL AND http_status IS NULL AND response_json IS NULL)
        OR
        (status = 'completed' AND completed_at IS NOT NULL AND http_status BETWEEN 100 AND 599
         AND response_json IS NOT NULL)
    ),
    CONSTRAINT chk_command_idempotency_response_size CHECK (
        response_json IS NULL OR JSON_STORAGE_SIZE(response_json) <= 32768
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE async_tasks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    queue VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    subject_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    subject_id BIGINT UNSIGNED NOT NULL,
    transition VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    expected_subject_version BIGINT UNSIGNED NOT NULL,
    payload_schema_version SMALLINT UNSIGNED NOT NULL,
    payload_json JSON NOT NULL,
    checkpoint_schema_version SMALLINT UNSIGNED NULL,
    checkpoint_version BIGINT UNSIGNED NULL DEFAULT 0,
    checkpoint_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    checkpoint_json JSON NULL,
    dedupe_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    replay_generation INT UNSIGNED NOT NULL DEFAULT 0,
    logical_operation_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'ready',
    priority INT NOT NULL DEFAULT 0,
    available_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    attempt INT UNSIGNED NOT NULL DEFAULT 0,
    max_attempts INT UNSIGNED NOT NULL,
    lease_owner VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    lease_generation BIGINT UNSIGNED NOT NULL DEFAULT 0,
    lease_expires_at DATETIME(6) NULL,
    heartbeat_at DATETIME(6) NULL,
    last_error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    last_error_summary VARCHAR(2048) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    started_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    dead_at DATETIME(6) NULL,
    cancelled_at DATETIME(6) NULL,
    replayed_from_task_id BIGINT UNSIGNED NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_async_tasks_public_id (public_id),
    UNIQUE KEY uk_async_tasks_dedupe_generation (dedupe_key, replay_generation),
    KEY idx_async_tasks_ready_claim (queue, status, available_at, priority DESC, id),
    KEY idx_async_tasks_expired_takeover (queue, status, lease_expires_at, id),
    KEY idx_async_tasks_incident_cycle (incident_id, cycle_no, created_at, id),
    KEY idx_async_tasks_logical_operation (logical_operation_key, replay_generation, id),
    KEY idx_async_tasks_replayed_from (replayed_from_task_id, id),
    CONSTRAINT fk_async_tasks_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_async_tasks_replayed_from FOREIGN KEY (replayed_from_task_id) REFERENCES async_tasks (id) ON DELETE RESTRICT,
    CONSTRAINT chk_async_tasks_queue CHECK (queue IN ('investigate','deliver','observe','verify')),
    CONSTRAINT chk_async_tasks_type CHECK (task_type IN (
        'investigation.advance','remediation.prepare','change.ensure_pr','delivery.observe','verification.advance'
    )),
    CONSTRAINT chk_async_tasks_queue_type CHECK (
        (queue = 'investigate' AND task_type IN ('investigation.advance','remediation.prepare'))
        OR (queue = 'deliver' AND task_type = 'change.ensure_pr')
        OR (queue = 'observe' AND task_type = 'delivery.observe')
        OR (queue = 'verify' AND task_type = 'verification.advance')
    ),
    CONSTRAINT chk_async_tasks_subject_type CHECK (subject_type IN (
        'incident','agent_run','remediation_plan','change_request','verification_run'
    )),
    CONSTRAINT chk_async_tasks_subject_transition CHECK (
        (task_type = 'investigation.advance' AND (
            (subject_type = 'incident' AND transition = 'investigation.start')
            OR (subject_type = 'agent_run' AND transition = 'investigation.step')
        ))
        OR (task_type = 'remediation.prepare'
            AND subject_type = 'agent_run' AND transition = 'remediation.prepare')
        OR (task_type = 'change.ensure_pr'
            AND subject_type IN ('remediation_plan','change_request') AND transition = 'change.ensure_pr')
        OR (task_type = 'delivery.observe'
            AND subject_type = 'change_request' AND transition = 'delivery.observe')
        OR (task_type = 'verification.advance'
            AND subject_type = 'verification_run' AND transition = 'verification.advance')
    ),
    CONSTRAINT chk_async_tasks_status CHECK (status IN ('ready','running','succeeded','dead','cancelled')),
    CONSTRAINT chk_async_tasks_versions CHECK (
        cycle_no > 0 AND expected_subject_version > 0 AND payload_schema_version > 0
        AND max_attempts > 0 AND attempt <= max_attempts
    ),
    CONSTRAINT chk_async_tasks_checkpoint CHECK (
        (checkpoint_json IS NULL AND checkpoint_schema_version IS NULL AND checkpoint_hash IS NULL
         AND (checkpoint_version IS NULL OR checkpoint_version = 0))
        OR
        (checkpoint_json IS NOT NULL AND checkpoint_schema_version IS NOT NULL
         AND checkpoint_schema_version > 0 AND checkpoint_version IS NOT NULL
         AND checkpoint_version > 0 AND checkpoint_hash IS NOT NULL
         AND CHAR_LENGTH(checkpoint_hash) = 64)
    ),
    CONSTRAINT chk_async_tasks_payload_size CHECK (JSON_STORAGE_SIZE(payload_json) <= 8192),
    CONSTRAINT chk_async_tasks_checkpoint_size CHECK (
        checkpoint_json IS NULL OR JSON_STORAGE_SIZE(checkpoint_json) <= 131072
    ),
    CONSTRAINT chk_async_tasks_lease_shape CHECK (
        (status = 'running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (status <> 'running' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE async_task_attempts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id BIGINT UNSIGNED NOT NULL,
    attempt INT UNSIGNED NOT NULL,
    expected_subject_version BIGINT UNSIGNED NOT NULL,
    lease_owner VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    lease_generation BIGINT UNSIGNED NOT NULL,
    claim_kind VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at DATETIME(6) NOT NULL,
    last_heartbeat_at DATETIME(6) NULL,
    finished_at DATETIME(6) NULL,
    error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    error_summary VARCHAR(2048) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_async_task_attempts_public_id (public_id),
    UNIQUE KEY uk_async_task_attempts_task_attempt (task_id, attempt),
    KEY idx_async_task_attempts_task_created (task_id, created_at, id),
    CONSTRAINT fk_async_task_attempts_task FOREIGN KEY (task_id) REFERENCES async_tasks (id) ON DELETE RESTRICT,
    CONSTRAINT chk_async_task_attempts_attempt CHECK (
        attempt > 0 AND expected_subject_version > 0 AND lease_generation > 0
    ),
    CONSTRAINT chk_async_task_attempts_claim_kind CHECK (claim_kind IN ('ready','takeover')),
    CONSTRAINT chk_async_task_attempts_status CHECK (
        status IN ('running','succeeded','retry','dead','lease_expired','cancelled','lease_lost')
    ),
    CONSTRAINT chk_async_task_attempts_terminal CHECK (
        (status = 'running' AND finished_at IS NULL)
        OR (status <> 'running' AND finished_at IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE migration_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    plan_version INT UNSIGNED NOT NULL,
    stage VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    operation VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    attempt INT UNSIGNED NOT NULL DEFAULT 1,
    previous_ledger_id BIGINT UNSIGNED NULL,
    source_schema_version BIGINT UNSIGNED NOT NULL,
    target_schema_version BIGINT UNSIGNED NOT NULL,
    source_table VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_table VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    batch_no BIGINT UNSIGNED NOT NULL,
    id_min BIGINT UNSIGNED NULL,
    id_max BIGINT UNSIGNED NULL,
    source_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    target_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    skipped_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    rejected_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    converter_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    bounded_summary VARCHAR(2048) NULL,
    source_exact_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    binary_image_digest VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_migration_ledger_public_id (public_id),
    UNIQUE KEY uk_migration_ledger_unit_attempt (plan_version, operation, batch_no, attempt),
    KEY idx_migration_ledger_stage_status (stage, status, started_at, id),
    KEY idx_migration_ledger_previous (previous_ledger_id, id),
    CONSTRAINT fk_migration_ledger_previous FOREIGN KEY (previous_ledger_id) REFERENCES migration_ledger (id) ON DELETE RESTRICT,
    CONSTRAINT chk_migration_ledger_versions CHECK (
        plan_version > 0 AND attempt > 0 AND target_schema_version >= source_schema_version
    ),
    CONSTRAINT chk_migration_ledger_range CHECK (
        id_min IS NULL OR id_max IS NULL OR id_max >= id_min
    ),
    CONSTRAINT chk_migration_ledger_hashes CHECK (
        (source_hash IS NULL OR CHAR_LENGTH(source_hash) = 64)
        AND (target_hash IS NULL OR CHAR_LENGTH(target_hash) = 64)
        AND CHAR_LENGTH(source_exact_sha) BETWEEN 40 AND 64
    ),
    CONSTRAINT chk_migration_ledger_status CHECK (status IN ('running','passed','failed')),
    CONSTRAINT chk_migration_ledger_completion CHECK (
        (status = 'running' AND completed_at IS NULL)
        OR (status IN ('passed','failed') AND completed_at IS NOT NULL)
    ),
    CONSTRAINT chk_migration_ledger_retry_link CHECK (
        (attempt = 1 AND previous_ledger_id IS NULL)
        OR (attempt > 1 AND previous_ledger_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
