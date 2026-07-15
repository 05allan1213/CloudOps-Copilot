-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE incident_events
    ADD COLUMN idempotency_key CHAR(64) NULL AFTER event_type,
    ADD UNIQUE KEY uk_incident_events_idempotency (idempotency_key);

ALTER TABLE change_requests
    DROP CHECK chk_change_requests_status,
    ADD COLUMN pr_state VARCHAR(16) NOT NULL DEFAULT '' AFTER pr_url,
    ADD COLUMN merged_commit_sha CHAR(64) NOT NULL DEFAULT '' AFTER pr_state,
    ADD COLUMN target_revision CHAR(64) NOT NULL DEFAULT '' AFTER merged_commit_sha,
    ADD COLUMN argocd_application VARCHAR(255) NOT NULL DEFAULT '' AFTER target_revision,
    ADD COLUMN argocd_project VARCHAR(255) NOT NULL DEFAULT '' AFTER argocd_application,
    ADD COLUMN detected_revision VARCHAR(255) NOT NULL DEFAULT '' AFTER argocd_project,
    ADD COLUMN argocd_sync_status VARCHAR(32) NOT NULL DEFAULT '' AFTER detected_revision,
    ADD COLUMN argocd_operation_phase VARCHAR(32) NOT NULL DEFAULT '' AFTER argocd_sync_status,
    ADD COLUMN argocd_health_status VARCHAR(32) NOT NULL DEFAULT '' AFTER argocd_operation_phase,
    ADD COLUMN resource_health_json JSON NULL AFTER argocd_health_status,
    ADD COLUMN sync_started_at DATETIME(6) NULL AFTER resource_health_json,
    ADD COLUMN sync_completed_at DATETIME(6) NULL AFTER sync_started_at,
    ADD COLUMN cluster VARCHAR(255) NOT NULL DEFAULT '' AFTER sync_completed_at,
    ADD COLUMN environment VARCHAR(255) NOT NULL DEFAULT '' AFTER cluster,
    ADD COLUMN namespace VARCHAR(255) NOT NULL DEFAULT '' AFTER environment,
    ADD COLUMN workload_kind VARCHAR(64) NOT NULL DEFAULT '' AFTER namespace,
    ADD COLUMN workload_name VARCHAR(255) NOT NULL DEFAULT '' AFTER workload_kind,
    ADD COLUMN deployment_generation BIGINT NOT NULL DEFAULT 0 AFTER workload_name,
    ADD COLUMN observed_generation BIGINT NOT NULL DEFAULT 0 AFTER deployment_generation,
    ADD COLUMN rollout_revision VARCHAR(64) NOT NULL DEFAULT '' AFTER observed_generation,
    ADD COLUMN desired_replicas INT NOT NULL DEFAULT 0 AFTER rollout_revision,
    ADD COLUMN updated_replicas INT NOT NULL DEFAULT 0 AFTER desired_replicas,
    ADD COLUMN available_replicas INT NOT NULL DEFAULT 0 AFTER updated_replicas,
    ADD COLUMN unavailable_replicas INT NOT NULL DEFAULT 0 AFTER available_replicas,
    ADD COLUMN delivery_started_at DATETIME(6) NULL AFTER unavailable_replicas,
    ADD COLUMN delivery_deadline_at DATETIME(6) NULL AFTER delivery_started_at,
    ADD COLUMN delivery_completed_at DATETIME(6) NULL AFTER delivery_deadline_at,
    ADD COLUMN next_poll_at DATETIME(6) NULL AFTER delivery_completed_at,
    ADD COLUMN last_observed_at DATETIME(6) NULL AFTER next_poll_at,
    ADD COLUMN failure_reason VARCHAR(128) NOT NULL DEFAULT '' AFTER failure_code,
    ADD KEY idx_change_requests_delivery_claim (status, next_poll_at, lease_expires_at, id),
    ADD KEY idx_change_requests_exact_revision (repository, merged_commit_sha, target_revision),
    ADD CONSTRAINT chk_change_requests_delivery_counts CHECK (
        deployment_generation >= 0 AND observed_generation >= 0 AND
        desired_replicas >= 0 AND updated_replicas >= 0 AND available_replicas >= 0 AND unavailable_replicas >= 0
    ),
    ADD CONSTRAINT chk_change_requests_status CHECK (status IN (
        'pending','delivering','pr_created','ci_pending','ci_passed','merge_pending','merged',
        'argocd_pending','syncing','synced','rollout_pending','delivered','ci_failed','pr_closed',
        'merge_timeout','revision_mismatch','argocd_failed','argocd_timeout','rollout_failed',
        'delivery_cancelled','failed'
    ));

CREATE TABLE verification_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    remediation_plan_id BIGINT UNSIGNED NOT NULL,
    change_request_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) NOT NULL,
    target_revision CHAR(64) NOT NULL,
    plan_json JSON NOT NULL,
    started_at DATETIME(6) NULL,
    deadline_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    attempt INT NOT NULL,
    lease_owner VARCHAR(128) NOT NULL DEFAULT '',
    lease_expires_at DATETIME(6) NULL,
    heartbeat_at DATETIME(6) NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    result_summary VARCHAR(2048) NOT NULL DEFAULT '',
    failure_reason VARCHAR(128) NOT NULL DEFAULT '',
    active_identity VARCHAR(192) GENERATED ALWAYS AS (
        CASE WHEN status IN ('pending','running') THEN CONCAT(change_request_id, ':', target_revision) ELSE NULL END
    ) STORED,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_verification_runs_public_id (public_id),
    UNIQUE KEY uk_verification_runs_attempt (change_request_id, target_revision, attempt),
    UNIQUE KEY uk_verification_runs_active_identity (active_identity),
    KEY idx_verification_runs_incident_created (incident_id, created_at, id),
    KEY idx_verification_runs_claim (status, lease_expires_at, created_at, id),
    CONSTRAINT fk_verification_runs_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_verification_runs_plan FOREIGN KEY (remediation_plan_id) REFERENCES remediation_plans (id) ON DELETE RESTRICT,
    CONSTRAINT fk_verification_runs_change_request FOREIGN KEY (change_request_id) REFERENCES change_requests (id) ON DELETE RESTRICT,
    CONSTRAINT chk_verification_runs_status CHECK (status IN ('pending','running','passed','failed','timed_out','cancelled')),
    CONSTRAINT chk_verification_runs_versions CHECK (attempt > 0 AND row_version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE verification_checks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    verification_run_id BIGINT UNSIGNED NOT NULL,
    check_type VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    required_check BOOLEAN NOT NULL DEFAULT TRUE,
    subject_json JSON NOT NULL,
    expected_json JSON NOT NULL,
    observed_json JSON NULL,
    source_reference VARCHAR(1024) NOT NULL DEFAULT '',
    lookback_ms BIGINT NOT NULL DEFAULT 0,
    stability_window_ms BIGINT NOT NULL,
    timeout_ms BIGINT NOT NULL,
    poll_interval_ms BIGINT NOT NULL,
    first_checked_at DATETIME(6) NULL,
    last_checked_at DATETIME(6) NULL,
    passed_at DATETIME(6) NULL,
    consecutive_success_since DATETIME(6) NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    failure_reason VARCHAR(128) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_verification_checks_public_id (public_id),
    UNIQUE KEY uk_verification_checks_run_type (verification_run_id, check_type),
    KEY idx_verification_checks_run_status (verification_run_id, status, id),
    CONSTRAINT fk_verification_checks_run FOREIGN KEY (verification_run_id) REFERENCES verification_runs (id) ON DELETE RESTRICT,
    CONSTRAINT chk_verification_checks_type CHECK (check_type IN (
        'argocd_revision','argocd_sync','argocd_health','deployment_rollout','workload_ready',
        'alert_resolved','metric_threshold','log_error_rate','trace_error_rate'
    )),
    CONSTRAINT chk_verification_checks_status CHECK (status IN ('pending','running','passed','failed','unavailable','invalid','cancelled')),
    CONSTRAINT chk_verification_checks_bounds CHECK (
        lookback_ms >= 0 AND stability_window_ms > 0 AND timeout_ms >= stability_window_ms AND
        poll_interval_ms > 0 AND attempt_count >= 0
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
-- +goose NO TRANSACTION

DROP TABLE IF EXISTS verification_checks;
DROP TABLE IF EXISTS verification_runs;

UPDATE change_requests
SET status = CASE WHEN status = 'delivered' THEN 'pr_created' ELSE 'failed' END
WHERE status NOT IN ('pending','delivering','pr_created','failed');

ALTER TABLE change_requests
    DROP CHECK chk_change_requests_status,
    DROP CHECK chk_change_requests_delivery_counts,
    DROP INDEX idx_change_requests_exact_revision,
    DROP INDEX idx_change_requests_delivery_claim,
    DROP COLUMN failure_reason,
    DROP COLUMN last_observed_at,
    DROP COLUMN next_poll_at,
    DROP COLUMN delivery_completed_at,
    DROP COLUMN delivery_deadline_at,
    DROP COLUMN delivery_started_at,
    DROP COLUMN unavailable_replicas,
    DROP COLUMN available_replicas,
    DROP COLUMN updated_replicas,
    DROP COLUMN desired_replicas,
    DROP COLUMN rollout_revision,
    DROP COLUMN observed_generation,
    DROP COLUMN deployment_generation,
    DROP COLUMN workload_name,
    DROP COLUMN workload_kind,
    DROP COLUMN namespace,
    DROP COLUMN environment,
    DROP COLUMN cluster,
    DROP COLUMN sync_completed_at,
    DROP COLUMN sync_started_at,
    DROP COLUMN resource_health_json,
    DROP COLUMN argocd_health_status,
    DROP COLUMN argocd_operation_phase,
    DROP COLUMN argocd_sync_status,
    DROP COLUMN detected_revision,
    DROP COLUMN argocd_project,
    DROP COLUMN argocd_application,
    DROP COLUMN target_revision,
    DROP COLUMN merged_commit_sha,
    DROP COLUMN pr_state,
    ADD CONSTRAINT chk_change_requests_status CHECK (status IN ('pending','delivering','pr_created','failed'));

ALTER TABLE incident_events
    DROP INDEX uk_incident_events_idempotency,
    DROP COLUMN idempotency_key;
