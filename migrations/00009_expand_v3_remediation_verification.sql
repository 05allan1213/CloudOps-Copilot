-- +goose Up
-- +goose NO TRANSACTION

-- Phase 5/6 remains expand-compatible. Existing V2 rows and the partial V3
-- rows admitted by 00008 keep every new contract-version column NULL. A new
-- writer opts into the complete immutable contract by setting that version.

ALTER TABLE remediation_plans
    DROP CHECK chk_remediation_plans_operation,
    ADD COLUMN plan_content_schema_version SMALLINT UNSIGNED NULL AFTER canonical_plan_hash,
    ADD COLUMN incident_version BIGINT UNSIGNED NULL AFTER cycle_no,
    ADD COLUMN created_by_agent_run_id BIGINT UNSIGNED NULL AFTER incident_version,
    ADD COLUMN diagnosis_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER created_by_agent_run_id,
    ADD COLUMN target_base_branch VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_repository,
    ADD COLUMN last_known_good_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_base_revision,
    ADD COLUMN base_blob_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER last_known_good_sha,
    ADD COLUMN file_mode VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER base_blob_sha,
    ADD COLUMN target_resource_json JSON NULL AFTER target_path,
    ADD COLUMN target_field_ref VARCHAR(1024) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER target_resource_json,
    ADD COLUMN expected_post_image_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER expected_before_hash,
    ADD COLUMN expected_tree_hash VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER expected_post_image_hash,
    ADD COLUMN canonical_change_manifest_json JSON NULL AFTER proposed_patch_hash,
    ADD COLUMN bounded_diff MEDIUMTEXT NULL AFTER canonical_change_manifest_json,
    ADD COLUMN policy_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER policy_snapshot_hash,
    ADD COLUMN policy_snapshot_json JSON NULL AFTER policy_version,
    ADD COLUMN verification_plan_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER verification_plan_json,
    ADD COLUMN evidence_bindings_json JSON NULL AFTER evidence_references_json,
    ADD COLUMN evidence_set_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER evidence_bindings_json,
    ADD COLUMN expires_at DATETIME(6) NULL AFTER created_at,
    ADD UNIQUE KEY uk_remediation_plans_v3_owner (id, incident_id, cycle_no),
    ADD KEY idx_remediation_plans_v3_creator (created_by_agent_run_id, id),
    ADD CONSTRAINT fk_remediation_plans_v3_creator
        FOREIGN KEY (created_by_agent_run_id) REFERENCES agent_runs (id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_remediation_plans_operation CHECK (
        operation_type IN ('rollback_image', 'set_replicas', 'restore_required_env')
    ),
    ADD CONSTRAINT chk_remediation_plans_v3_complete CHECK (
        (
            plan_content_schema_version IS NULL
            AND incident_version IS NULL
            AND created_by_agent_run_id IS NULL
            AND diagnosis_hash IS NULL
            AND target_base_branch IS NULL
            AND last_known_good_sha IS NULL
            AND base_blob_sha IS NULL
            AND file_mode IS NULL
            AND target_resource_json IS NULL
            AND target_field_ref IS NULL
            AND expected_post_image_hash IS NULL
            AND expected_tree_hash IS NULL
            AND canonical_change_manifest_json IS NULL
            AND bounded_diff IS NULL
            AND policy_version IS NULL
            AND policy_snapshot_json IS NULL
            AND verification_plan_hash IS NULL
            AND evidence_bindings_json IS NULL
            AND evidence_set_hash IS NULL
            AND expires_at IS NULL
        )
        OR
        (
            domain_schema_version = 3
            AND plan_content_schema_version > 0
            AND incident_version > 0
            AND created_by_agent_run_id IS NOT NULL
            AND diagnosis_hash IS NOT NULL
            AND CHAR_LENGTH(diagnosis_hash) = 64
            AND operation_type = 'restore_required_env'
            AND target_repository IS NOT NULL
            AND CHAR_LENGTH(target_repository) > 0
            AND target_base_branch IS NOT NULL
            AND CHAR_LENGTH(target_base_branch) > 0
            AND target_base_revision IS NOT NULL
            AND CHAR_LENGTH(target_base_revision) BETWEEN 40 AND 64
            AND last_known_good_sha IS NOT NULL
            AND CHAR_LENGTH(last_known_good_sha) BETWEEN 40 AND 64
            AND base_blob_sha IS NOT NULL
            AND CHAR_LENGTH(base_blob_sha) BETWEEN 40 AND 64
            AND file_mode = '100644'
            AND target_path IS NOT NULL
            AND CHAR_LENGTH(target_path) > 0
            AND target_resource_json IS NOT NULL
            AND JSON_STORAGE_SIZE(target_resource_json) <= 4096
            AND target_field_ref IS NOT NULL
            AND CHAR_LENGTH(target_field_ref) > 0
            AND expected_before_hash IS NOT NULL
            AND CHAR_LENGTH(expected_before_hash) = 64
            AND expected_post_image_hash IS NOT NULL
            AND CHAR_LENGTH(expected_post_image_hash) = 64
            AND expected_tree_hash IS NOT NULL
            AND CHAR_LENGTH(expected_tree_hash) BETWEEN 40 AND 64
            AND canonical_change_manifest_json IS NOT NULL
            AND JSON_STORAGE_SIZE(canonical_change_manifest_json) <= 4096
            AND proposed_patch_hash IS NOT NULL
            AND CHAR_LENGTH(proposed_patch_hash) = 64
            AND bounded_diff IS NOT NULL
            AND OCTET_LENGTH(bounded_diff) BETWEEN 1 AND 65536
            AND policy_version IS NOT NULL
            AND CHAR_LENGTH(policy_version) > 0
            AND policy_snapshot_json IS NOT NULL
            AND JSON_STORAGE_SIZE(policy_snapshot_json) <= 16384
            AND policy_snapshot_hash IS NOT NULL
            AND CHAR_LENGTH(policy_snapshot_hash) = 64
            AND verification_plan_json IS NOT NULL
            AND JSON_STORAGE_SIZE(verification_plan_json) <= 16384
            AND verification_plan_hash IS NOT NULL
            AND CHAR_LENGTH(verification_plan_hash) = 64
            AND evidence_bindings_json IS NOT NULL
            AND JSON_STORAGE_SIZE(evidence_bindings_json) <= 16384
            AND evidence_set_hash IS NOT NULL
            AND CHAR_LENGTH(evidence_set_hash) = 64
            AND canonical_plan_hash IS NOT NULL
            AND CHAR_LENGTH(canonical_plan_hash) = 64
            AND hash_schema_version > 0
            AND expires_at IS NOT NULL
            AND expires_at > created_at
        )
    );

CREATE TABLE remediation_decisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    decision_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    plan_version INT UNSIGNED NOT NULL,
    decision VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_provider VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_login VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
    actor_role VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(1024) NOT NULL,
    request_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_authenticated_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    approved_hash_schema_version SMALLINT UNSIGNED NOT NULL,
    approved_plan_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_base_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_post_image_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_tree_hash VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_patch_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_policy_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_verification_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    approved_evidence_set_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_remediation_decisions_public_id (public_id),
    UNIQUE KEY uk_remediation_decisions_plan (plan_id),
    UNIQUE KEY uk_remediation_decisions_request (request_id),
    KEY idx_remediation_decisions_incident_cycle (incident_id, cycle_no, created_at, id),
    CONSTRAINT fk_remediation_decisions_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_remediation_decisions_plan_owner
        FOREIGN KEY (plan_id, incident_id, cycle_no)
        REFERENCES remediation_plans (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT chk_remediation_decisions_identity CHECK (
        domain_schema_version = 3
        AND decision_schema_version > 0
        AND cycle_no > 0
        AND plan_version > 0
        AND decision IN ('approved', 'rejected')
        AND actor_provider = 'github'
        AND CHAR_LENGTH(actor_login) > 0
        AND actor_role IN ('operator', 'admin')
        AND CHAR_LENGTH(request_id) > 0
        AND expires_at > request_authenticated_at
    ),
    CONSTRAINT chk_remediation_decisions_hashes CHECK (
        approved_hash_schema_version > 0
        AND CHAR_LENGTH(approved_plan_hash) = 64
        AND CHAR_LENGTH(approved_base_sha) BETWEEN 40 AND 64
        AND CHAR_LENGTH(approved_post_image_hash) = 64
        AND CHAR_LENGTH(approved_tree_hash) BETWEEN 40 AND 64
        AND CHAR_LENGTH(approved_patch_hash) = 64
        AND CHAR_LENGTH(approved_policy_hash) = 64
        AND CHAR_LENGTH(approved_verification_hash) = 64
        AND CHAR_LENGTH(approved_evidence_set_hash) = 64
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE change_requests
    ADD UNIQUE KEY uk_change_requests_v3_owner (id, incident_id, cycle_no);

CREATE TABLE change_request_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    event_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    change_request_id BIGINT UNSIGNED NOT NULL,
    sequence_no INT UNSIGNED NOT NULL,
    event_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_system VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    write_phase VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    external_write_started BOOLEAN NOT NULL DEFAULT FALSE,
    external_write_marker CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    payload_json JSON NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_change_request_events_public_id (public_id),
    UNIQUE KEY uk_change_request_events_sequence (change_request_id, sequence_no),
    KEY idx_change_request_events_incident_cycle (incident_id, cycle_no, occurred_at, id),
    CONSTRAINT fk_change_request_events_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_change_request_events_owner
        FOREIGN KEY (change_request_id, incident_id, cycle_no)
        REFERENCES change_requests (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT chk_change_request_events_identity CHECK (
        domain_schema_version = 3
        AND event_schema_version > 0
        AND cycle_no > 0
        AND sequence_no > 0
        AND CHAR_LENGTH(event_type) > 0
        AND source_system IN ('worker', 'github', 'argocd', 'kubernetes', 'system')
        AND (write_phase IS NULL OR write_phase IN ('ensure_branch', 'ensure_commit', 'ensure_draft_pr', 'observe'))
    ),
    CONSTRAINT chk_change_request_events_write_marker CHECK (
        (external_write_started = FALSE AND external_write_marker IS NULL)
        OR (external_write_started = TRUE AND external_write_marker IS NOT NULL
            AND CHAR_LENGTH(external_write_marker) = 64)
    ),
    CONSTRAINT chk_change_request_events_payload CHECK (
        JSON_STORAGE_SIZE(payload_json) <= 8192 AND CHAR_LENGTH(content_hash) = 64
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE verification_runs
    ADD COLUMN verification_contract_version SMALLINT UNSIGNED NULL AFTER verification_profile_hash,
    ADD COLUMN verification_profile_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER plan_json,
    ADD COLUMN common_stability_window_ms BIGINT UNSIGNED NULL AFTER verification_contract_version,
    ADD COLUMN common_success_since DATETIME(6) NULL AFTER common_stability_window_ms,
    ADD COLUMN common_window_completed_at DATETIME(6) NULL AFTER common_success_since,
    ADD UNIQUE KEY uk_verification_runs_v3_owner (id, incident_id, cycle_no),
    ADD CONSTRAINT chk_verification_runs_v3_complete CHECK (
        (
            verification_contract_version IS NULL
            AND verification_profile_id IS NULL
            AND common_stability_window_ms IS NULL
            AND common_success_since IS NULL
            AND common_window_completed_at IS NULL
        )
        OR
        (
            domain_schema_version = 3
            AND verification_contract_version > 0
            AND verification_profile_id IS NOT NULL
            AND verification_profile_id IN ('golden-required-env/v1', 'no-change/v1')
            AND verification_profile_version > 0
            AND verification_profile_hash IS NOT NULL
            AND CHAR_LENGTH(verification_profile_hash) = 64
            AND common_stability_window_ms = 60000
            AND target_revision IS NOT NULL
            AND CHAR_LENGTH(target_revision) BETWEEN 40 AND 64
            AND source_revision IS NOT NULL
            AND CHAR_LENGTH(source_revision) BETWEEN 40 AND 64
            AND image_digest IS NOT NULL
            AND CHAR_LENGTH(image_digest) = 71
            AND image_digest LIKE 'sha256:%'
            AND gitops_revision IS NOT NULL
            AND CHAR_LENGTH(gitops_revision) BETWEEN 40 AND 64
            AND plan_json IS NOT NULL
            AND JSON_STORAGE_SIZE(plan_json) <= 16384
            AND (
                (trigger_type = 'post_delivery'
                 AND verification_profile_id = 'golden-required-env/v1'
                 AND remediation_plan_id IS NOT NULL
                 AND change_request_id IS NOT NULL
                 AND trigger_signal_id IS NULL)
                OR
                (trigger_type = 'no_change_signal'
                 AND verification_profile_id = 'no-change/v1'
                 AND remediation_plan_id IS NULL
                 AND change_request_id IS NULL
                 AND trigger_signal_id IS NOT NULL)
            )
            AND (common_window_completed_at IS NULL OR common_success_since IS NOT NULL)
            AND (common_window_completed_at IS NULL OR common_window_completed_at >= common_success_since)
        )
    );

ALTER TABLE verification_checks
    DROP CHECK chk_verification_checks_type,
    ADD COLUMN check_spec_schema_version SMALLINT UNSIGNED NULL AFTER cycle_no,
    ADD COLUMN profile_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER check_type,
    ADD COLUMN template_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER profile_id,
    ADD COLUMN template_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER template_id,
    ADD COLUMN comparison VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER expected_json,
    ADD COLUMN threshold DOUBLE NULL AFTER comparison,
    ADD COLUMN source_identity VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER source_reference,
    ADD COLUMN initial_delay_ms BIGINT UNSIGNED NULL AFTER lookback_ms,
    ADD COLUMN min_samples INT UNSIGNED NULL AFTER poll_interval_ms,
    ADD COLUMN sample_unit VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER min_samples,
    ADD COLUMN failure_mode VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER sample_unit,
    ADD UNIQUE KEY uk_verification_checks_v3_owner (id, verification_run_id, incident_id, cycle_no),
    ADD CONSTRAINT chk_verification_checks_type CHECK (check_type IN (
        'argocd_revision','argocd_sync','argocd_health','deployment_rollout','workload_ready','alert_resolved',
        'metric_error_rate_below','metric_availability_above','metric_latency_p95_below',
        'log_error_absent','log_error_rate_below','trace_error_rate_below','trace_latency_p95_below',
        'argocd_exact_revision','argocd_sync_succeeded','deployment_observed_generation',
        'deployment_rollout_complete','incident_alerts_resolved','deployment_identity_unchanged',
        'log_required_env_error_absent'
    )),
    ADD CONSTRAINT chk_verification_checks_v3_complete CHECK (
        (
            check_spec_schema_version IS NULL
            AND profile_id IS NULL
            AND template_id IS NULL
            AND template_version IS NULL
            AND comparison IS NULL
            AND threshold IS NULL
            AND source_identity IS NULL
            AND initial_delay_ms IS NULL
            AND min_samples IS NULL
            AND sample_unit IS NULL
            AND failure_mode IS NULL
        )
        OR
        (
            domain_schema_version = 3
            AND check_spec_schema_version > 0
            AND profile_id IS NOT NULL
            AND profile_id IN ('golden-required-env/v1', 'no-change/v1')
            AND template_id IS NOT NULL
            AND CHAR_LENGTH(template_id) > 0
            AND template_version IS NOT NULL
            AND CHAR_LENGTH(template_version) > 0
            AND source_identity IS NOT NULL
            AND CHAR_LENGTH(source_identity) > 0
            AND initial_delay_ms IS NOT NULL
            AND initial_delay_ms >= 0
            AND min_samples IS NOT NULL
            AND min_samples > 0
            AND sample_unit IS NOT NULL
            AND CHAR_LENGTH(sample_unit) > 0
            AND failure_mode IS NOT NULL
            AND failure_mode IN ('resets', 'immediate')
            AND (
                (comparison IS NULL AND threshold IS NULL)
                OR (comparison IN ('lt', 'lte', 'gt', 'gte', 'absent') AND threshold IS NOT NULL)
            )
            AND (required_check = FALSE OR stability_window_ms = 60000)
            AND timeout_ms >= stability_window_ms
            AND poll_interval_ms > 0
        )
    );

CREATE TABLE verification_samples (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    sample_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    verification_run_id BIGINT UNSIGNED NOT NULL,
    verification_check_id BIGINT UNSIGNED NOT NULL,
    sample_sequence INT UNSIGNED NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    observed_json JSON NOT NULL,
    source_reference VARCHAR(1024) NOT NULL DEFAULT '',
    reason_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    window_start_at DATETIME(6) NULL,
    window_end_at DATETIME(6) NULL,
    sampled_at DATETIME(6) NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_verification_samples_public_id (public_id),
    UNIQUE KEY uk_verification_samples_check_sequence (verification_check_id, sample_sequence),
    KEY idx_verification_samples_run_sampled (verification_run_id, sampled_at, id),
    KEY idx_verification_samples_incident_cycle (incident_id, cycle_no, sampled_at, id),
    CONSTRAINT fk_verification_samples_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_verification_samples_run_owner
        FOREIGN KEY (verification_run_id, incident_id, cycle_no)
        REFERENCES verification_runs (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT fk_verification_samples_check_owner
        FOREIGN KEY (verification_check_id, verification_run_id, incident_id, cycle_no)
        REFERENCES verification_checks (id, verification_run_id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT chk_verification_samples_identity CHECK (
        domain_schema_version = 3
        AND sample_schema_version > 0
        AND cycle_no > 0
        AND sample_sequence > 0
        AND status IN ('passed', 'failed', 'pending', 'unavailable', 'invalid', 'timed_out')
        AND CHAR_LENGTH(content_hash) = 64
    ),
    CONSTRAINT chk_verification_samples_window CHECK (
        (window_start_at IS NULL AND window_end_at IS NULL)
        OR (window_start_at IS NOT NULL AND window_end_at IS NOT NULL
            AND window_end_at >= window_start_at AND sampled_at >= window_end_at)
    ),
    CONSTRAINT chk_verification_samples_payload CHECK (
        JSON_STORAGE_SIZE(observed_json) <= 16384
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE resolution_reports (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    report_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    verification_run_id BIGINT UNSIGNED NOT NULL,
    initial_signal_id BIGINT UNSIGNED NOT NULL,
    trigger_signal_id BIGINT UNSIGNED NULL,
    remediation_plan_id BIGINT UNSIGNED NULL,
    remediation_decision_id BIGINT UNSIGNED NULL,
    change_request_id BIGINT UNSIGNED NULL,
    trigger_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resolution_reason VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    service VARCHAR(255) NOT NULL,
    workload VARCHAR(255) NOT NULL,
    environment VARCHAR(255) NOT NULL,
    impact_summary VARCHAR(2048) NOT NULL,
    cycle_started_at DATETIME(6) NOT NULL,
    resolved_at DATETIME(6) NOT NULL,
    measured_duration_ms BIGINT UNSIGNED NOT NULL,
    bad_gitops_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    fix_gitops_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    source_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    image_digest VARCHAR(71) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    gitops_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    verification_profile_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    verification_profile_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    common_window_started_at DATETIME(6) NOT NULL,
    common_window_completed_at DATETIME(6) NOT NULL,
    trigger_signal_json JSON NOT NULL,
    diagnosis_json JSON NULL,
    evidence_json JSON NOT NULL,
    remediation_plan_json JSON NULL,
    remediation_decision_json JSON NULL,
    delivery_json JSON NULL,
    verification_json JSON NOT NULL,
    timeline_json JSON NOT NULL,
    agent_usage_json JSON NOT NULL,
    summary VARCHAR(2048) NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    generated_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_resolution_reports_public_id (public_id),
    UNIQUE KEY uk_resolution_reports_incident_cycle (incident_id, cycle_no),
    UNIQUE KEY uk_resolution_reports_verification_run (verification_run_id),
    KEY idx_resolution_reports_resolved (resolved_at, id),
    CONSTRAINT fk_resolution_reports_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_resolution_reports_verification_owner
        FOREIGN KEY (verification_run_id, incident_id, cycle_no)
        REFERENCES verification_runs (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT fk_resolution_reports_initial_signal
        FOREIGN KEY (initial_signal_id) REFERENCES incident_signals (id) ON DELETE RESTRICT,
    CONSTRAINT fk_resolution_reports_trigger_signal
        FOREIGN KEY (trigger_signal_id) REFERENCES incident_signals (id) ON DELETE RESTRICT,
    CONSTRAINT fk_resolution_reports_plan
        FOREIGN KEY (remediation_plan_id) REFERENCES remediation_plans (id) ON DELETE RESTRICT,
    CONSTRAINT fk_resolution_reports_decision
        FOREIGN KEY (remediation_decision_id) REFERENCES remediation_decisions (id) ON DELETE RESTRICT,
    CONSTRAINT fk_resolution_reports_change
        FOREIGN KEY (change_request_id) REFERENCES change_requests (id) ON DELETE RESTRICT,
    CONSTRAINT chk_resolution_reports_identity CHECK (
        domain_schema_version = 3
        AND report_schema_version > 0
        AND cycle_no > 0
        AND resolved_at >= cycle_started_at
        AND generated_at >= resolved_at
        AND common_window_completed_at >= common_window_started_at
        AND TIMESTAMPDIFF(MICROSECOND, common_window_started_at, common_window_completed_at) >= 60000000
        AND CHAR_LENGTH(source_revision) BETWEEN 40 AND 64
        AND CHAR_LENGTH(image_digest) = 71
        AND image_digest LIKE 'sha256:%'
        AND CHAR_LENGTH(gitops_revision) BETWEEN 40 AND 64
        AND verification_profile_id IN ('golden-required-env/v1', 'no-change/v1')
        AND CHAR_LENGTH(verification_profile_hash) = 64
        AND CHAR_LENGTH(content_hash) = 64
    ),
    CONSTRAINT chk_resolution_reports_path CHECK (
        (
            trigger_type = 'post_delivery'
            AND resolution_reason IN ('recovered_after_change', 'recovered_after_remediation')
            AND trigger_signal_id IS NULL
            AND remediation_plan_id IS NOT NULL
            AND remediation_decision_id IS NOT NULL
            AND change_request_id IS NOT NULL
            AND diagnosis_json IS NOT NULL
            AND remediation_plan_json IS NOT NULL
            AND remediation_decision_json IS NOT NULL
            AND delivery_json IS NOT NULL
            AND bad_gitops_revision IS NOT NULL
            AND CHAR_LENGTH(bad_gitops_revision) BETWEEN 40 AND 64
            AND fix_gitops_revision IS NOT NULL
            AND CHAR_LENGTH(fix_gitops_revision) BETWEEN 40 AND 64
            AND verification_profile_id = 'golden-required-env/v1'
        )
        OR
        (
            trigger_type = 'no_change_signal'
            AND resolution_reason IN ('recovered_before_diagnosis', 'recovered_without_change')
            AND trigger_signal_id IS NOT NULL
            AND remediation_plan_id IS NULL
            AND remediation_decision_id IS NULL
            AND change_request_id IS NULL
            AND remediation_plan_json IS NULL
            AND remediation_decision_json IS NULL
            AND delivery_json IS NULL
            AND bad_gitops_revision IS NULL
            AND fix_gitops_revision IS NULL
            AND verification_profile_id = 'no-change/v1'
        )
    ),
    CONSTRAINT chk_resolution_reports_payload CHECK (
        JSON_STORAGE_SIZE(trigger_signal_json) <= 8192
        AND (diagnosis_json IS NULL OR JSON_STORAGE_SIZE(diagnosis_json) <= 16384)
        AND JSON_STORAGE_SIZE(evidence_json) <= 32768
        AND (remediation_plan_json IS NULL OR JSON_STORAGE_SIZE(remediation_plan_json) <= 16384)
        AND (remediation_decision_json IS NULL OR JSON_STORAGE_SIZE(remediation_decision_json) <= 8192)
        AND (delivery_json IS NULL OR JSON_STORAGE_SIZE(delivery_json) <= 16384)
        AND JSON_STORAGE_SIZE(verification_json) <= 32768
        AND JSON_STORAGE_SIZE(timeline_json) <= 32768
        AND JSON_STORAGE_SIZE(agent_usage_json) <= 8192
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
