-- +goose Up
-- +goose NO TRANSACTION

CREATE TABLE remediation_plans (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    plan_version INT NOT NULL,
    plan_hash CHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    operation_type VARCHAR(32) NOT NULL,
    target_repository VARCHAR(255) NOT NULL,
    target_base_revision CHAR(64) NOT NULL,
    target_path VARCHAR(1024) NOT NULL,
    parameters_json JSON NOT NULL,
    evidence_references_json JSON NOT NULL,
    risk_level VARCHAR(16) NOT NULL,
    policy_snapshot_hash CHAR(64) NOT NULL,
    expected_before_hash CHAR(64) NOT NULL,
    proposed_patch_hash CHAR(64) NOT NULL,
    patch_summary VARCHAR(2048) NOT NULL,
    rollback_plan VARCHAR(4096) NOT NULL,
    validation_plan VARCHAR(4096) NOT NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_remediation_plans_public_id (public_id),
    UNIQUE KEY uk_remediation_plans_incident_version (incident_id, plan_version),
    KEY idx_remediation_plans_status_updated (status, updated_at, id),
    KEY idx_remediation_plans_incident_created (incident_id, created_at, id),
    CONSTRAINT fk_remediation_plans_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT chk_remediation_plans_version CHECK (plan_version > 0 AND row_version > 0),
    CONSTRAINT chk_remediation_plans_hashes CHECK (
        CHAR_LENGTH(plan_hash) = 64 AND CHAR_LENGTH(policy_snapshot_hash) = 64 AND
        CHAR_LENGTH(expected_before_hash) = 64 AND CHAR_LENGTH(proposed_patch_hash) = 64
    ),
    CONSTRAINT chk_remediation_plans_operation CHECK (operation_type IN ('rollback_image', 'set_replicas')),
    CONSTRAINT chk_remediation_plans_risk CHECK (risk_level IN ('low', 'medium', 'high')),
    CONSTRAINT chk_remediation_plans_status CHECK (status IN ('draft', 'awaiting_approval', 'approved', 'delivery_pending', 'delivering', 'pr_created', 'ci_pending', 'ci_passed', 'ci_failed', 'policy_rejected', 'rejected', 'cancelled', 'superseded'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE remediation_approvals (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    decision VARCHAR(16) NOT NULL,
    actor VARCHAR(128) NOT NULL,
    approved_plan_hash CHAR(64) NOT NULL,
    approved_patch_hash CHAR(64) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_remediation_approvals_public_id (public_id),
    UNIQUE KEY uk_remediation_approvals_plan (plan_id),
    CONSTRAINT fk_remediation_approvals_plan FOREIGN KEY (plan_id) REFERENCES remediation_plans (id) ON DELETE RESTRICT,
    CONSTRAINT chk_remediation_approvals_decision CHECK (decision IN ('approved', 'rejected')),
    CONSTRAINT chk_remediation_approvals_hashes CHECK (CHAR_LENGTH(approved_plan_hash) = 64 AND CHAR_LENGTH(approved_patch_hash) = 64)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE change_requests (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    repository VARCHAR(255) NOT NULL,
    base_revision CHAR(64) NOT NULL,
    head_branch VARCHAR(255) NOT NULL,
    commit_sha CHAR(64) NOT NULL DEFAULT '',
    pr_number BIGINT NOT NULL DEFAULT 0,
    pr_url VARCHAR(1024) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    ci_status VARCHAR(16) NOT NULL,
    idempotency_key CHAR(64) NOT NULL,
    lease_owner VARCHAR(128) NOT NULL DEFAULT '',
    lease_expires_at DATETIME(6) NULL,
    heartbeat_at DATETIME(6) NULL,
    attempts INT NOT NULL DEFAULT 0,
    failure_code VARCHAR(64) NOT NULL DEFAULT '',
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_change_requests_public_id (public_id),
    UNIQUE KEY uk_change_requests_plan (plan_id),
    UNIQUE KEY uk_change_requests_idempotency (idempotency_key),
    UNIQUE KEY uk_change_requests_repository_branch (repository, head_branch),
    KEY idx_change_requests_claim (status, lease_expires_at, created_at, id),
    CONSTRAINT fk_change_requests_plan FOREIGN KEY (plan_id) REFERENCES remediation_plans (id) ON DELETE RESTRICT,
    CONSTRAINT chk_change_requests_versions CHECK (attempts >= 0 AND row_version > 0 AND pr_number >= 0),
    CONSTRAINT chk_change_requests_ci CHECK (ci_status IN ('pending', 'passing', 'failing', 'cancelled')),
    CONSTRAINT chk_change_requests_status CHECK (status IN ('pending', 'delivering', 'pr_created', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
-- +goose NO TRANSACTION

DROP TABLE IF EXISTS change_requests;
DROP TABLE IF EXISTS remediation_approvals;
DROP TABLE IF EXISTS remediation_plans;
