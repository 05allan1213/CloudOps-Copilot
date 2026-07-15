-- +goose Up
-- +goose NO TRANSACTION

CREATE TABLE changes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    repository VARCHAR(512) NOT NULL DEFAULT '',
    repository_owner VARCHAR(255) NOT NULL DEFAULT '',
    commit_sha VARCHAR(64) NOT NULL DEFAULT '',
    base_commit_sha VARCHAR(64) NOT NULL DEFAULT '',
    pull_request_number BIGINT NOT NULL DEFAULT 0,
    workflow_run_id BIGINT NOT NULL DEFAULT 0,
    workflow_name VARCHAR(255) NOT NULL DEFAULT '',
    workflow_conclusion VARCHAR(32) NOT NULL DEFAULT '',
    image_repository VARCHAR(512) NOT NULL DEFAULT '',
    image_tag VARCHAR(255) NOT NULL DEFAULT '',
    image_digest VARCHAR(255) NOT NULL DEFAULT '',
    image_revision VARCHAR(64) NOT NULL DEFAULT '',
    argocd_application VARCHAR(255) NOT NULL DEFAULT '',
    argocd_project VARCHAR(255) NOT NULL DEFAULT '',
    argocd_target_revision VARCHAR(255) NOT NULL DEFAULT '',
    argocd_deployed_revision VARCHAR(255) NOT NULL DEFAULT '',
    environment VARCHAR(255) NOT NULL DEFAULT '',
    cluster VARCHAR(255) NOT NULL DEFAULT '',
    namespace VARCHAR(255) NOT NULL DEFAULT '',
    service_name VARCHAR(255) NOT NULL DEFAULT '',
    workload_kind VARCHAR(64) NOT NULL DEFAULT '',
    workload_name VARCHAR(255) NOT NULL DEFAULT '',
    gitops_path VARCHAR(512) NOT NULL DEFAULT '',
    started_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    deployed_at DATETIME(6) NULL,
    status VARCHAR(32) NOT NULL,
    category VARCHAR(32) NOT NULL,
    change_summary VARCHAR(4096) NOT NULL DEFAULT '',
    risk_summary VARCHAR(2048) NOT NULL DEFAULT '',
    correlation_score INT NOT NULL DEFAULT 0,
    correlation_reasons_json JSON NOT NULL,
    metadata_json JSON NOT NULL,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    degraded BOOLEAN NOT NULL DEFAULT FALSE,
    idempotency_key CHAR(64) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_changes_public_id (public_id),
    UNIQUE KEY uk_changes_incident_idempotency (incident_id, idempotency_key),
    KEY idx_changes_incident_deployed (incident_id, deployed_at, id),
    KEY idx_changes_repository_commit (repository, commit_sha),
    KEY idx_changes_image_digest (image_digest),
    KEY idx_changes_argocd_application (argocd_application, deployed_at),
    KEY idx_changes_service_namespace (service_name, namespace, deployed_at),
    KEY idx_changes_source_status (source_type, status, category),
    CONSTRAINT fk_changes_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT chk_changes_score CHECK (correlation_score >= 0 AND correlation_score <= 100),
    CONSTRAINT chk_changes_external_numbers CHECK (pull_request_number >= 0 AND workflow_run_id >= 0),
    CONSTRAINT chk_changes_source CHECK (source_type IN ('github_commit', 'github_pull_request', 'ci', 'image', 'argocd')),
    CONSTRAINT chk_changes_status CHECK (status IN ('candidate', 'matched', 'excluded', 'unknown')),
    CONSTRAINT chk_changes_category CHECK (category IN ('confirmed_match', 'high_confidence', 'low_confidence', 'excluded', 'no_data'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE evidence_items
    ADD COLUMN change_id BIGINT UNSIGNED NULL AFTER agent_run_id,
    ADD UNIQUE KEY uk_evidence_items_change (change_id),
    ADD CONSTRAINT fk_evidence_items_change FOREIGN KEY (change_id) REFERENCES changes (id) ON DELETE RESTRICT;

-- +goose Down
-- +goose NO TRANSACTION

ALTER TABLE evidence_items
    DROP FOREIGN KEY fk_evidence_items_change,
    DROP INDEX uk_evidence_items_change,
    DROP COLUMN change_id;

DROP TABLE IF EXISTS changes;
