-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE verification_checks
    DROP CHECK chk_verification_checks_type,
    ADD CONSTRAINT chk_verification_checks_type CHECK (check_type IN (
        'argocd_revision','argocd_sync','argocd_health','deployment_rollout','workload_ready','alert_resolved',
        'metric_error_rate_below','metric_availability_above','metric_latency_p95_below',
        'log_error_absent','log_error_rate_below','trace_error_rate_below','trace_latency_p95_below'
    ));

ALTER TABLE verification_checks
    DROP CHECK chk_verification_checks_status,
    ADD CONSTRAINT chk_verification_checks_status CHECK (status IN ('pending','running','passed','failed','timed_out','unavailable','invalid','cancelled'));

CREATE TABLE postmortems (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    verification_run_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(512) NOT NULL,
    impact_summary VARCHAR(2048) NOT NULL,
    detected_at DATETIME(6) NOT NULL,
    mitigated_at DATETIME(6) NULL,
    resolved_at DATETIME(6) NOT NULL,
    duration_seconds BIGINT NOT NULL,
    service VARCHAR(255) NOT NULL,
    workload VARCHAR(255) NOT NULL,
    environment VARCHAR(255) NOT NULL,
    triggering_signal_json JSON NOT NULL,
    change_correlation_json JSON NOT NULL,
    root_cause_json JSON NOT NULL,
    remediation_summary_json JSON NOT NULL,
    approval_summary_json JSON NOT NULL,
    delivery_revision CHAR(64) NOT NULL,
    verification_summary VARCHAR(2048) NOT NULL,
    checks_json JSON NOT NULL,
    timeline_json JSON NOT NULL,
    follow_up_actions_json JSON NOT NULL,
    generated_at DATETIME(6) NOT NULL,
    generation_version INT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_postmortems_public_id (public_id),
    UNIQUE KEY uk_postmortems_incident (incident_id),
    UNIQUE KEY uk_postmortems_final_attempt (incident_id, verification_run_id),
    KEY idx_postmortems_resolved (resolved_at, id),
    CONSTRAINT fk_postmortems_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_postmortems_verification FOREIGN KEY (verification_run_id) REFERENCES verification_runs (id) ON DELETE RESTRICT,
    CONSTRAINT chk_postmortems_bounds CHECK (
        duration_seconds >= 0 AND generation_version > 0 AND
        JSON_STORAGE_SIZE(triggering_signal_json) <= 8192 AND JSON_STORAGE_SIZE(change_correlation_json) <= 8192 AND
        JSON_STORAGE_SIZE(root_cause_json) <= 16384 AND JSON_STORAGE_SIZE(remediation_summary_json) <= 8192 AND
        JSON_STORAGE_SIZE(approval_summary_json) <= 8192 AND JSON_STORAGE_SIZE(checks_json) <= 32768 AND
        JSON_STORAGE_SIZE(timeline_json) <= 32768 AND JSON_STORAGE_SIZE(follow_up_actions_json) <= 8192
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
-- +goose NO TRANSACTION

-- DATA LOSS: export postmortems before production rollback. This drops only
-- Phase 6 structured postmortem data and preserves all Phase 1-5 objects.
DROP TABLE IF EXISTS postmortems;

-- The expanded enum constraint is retained so Phase 1-5 verification audit
-- rows are never deleted or rewritten during rollback. A Phase 5 binary does
-- not claim or execute the added terminal/check rows.
