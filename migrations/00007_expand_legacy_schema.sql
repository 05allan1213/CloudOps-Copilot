-- +goose Up
-- +goose NO TRANSACTION

-- Phase 1 transfers compatibility schema ownership from runtime GORM
-- AutoMigrate to explicit forward-only Goose history. These tables and
-- constraints intentionally match the legacy model-generated MySQL schema.
CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL,
    password VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'viewer',
    token_version BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_users_username (username)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS host_groups (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_host_groups_name (name)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS host_group_members (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    group_id BIGINT UNSIGNED NOT NULL,
    instance VARCHAR(256) NOT NULL,
    created_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_group_instance (group_id, instance),
    KEY idx_host_group_members_instance (instance),
    CONSTRAINT fk_host_groups_members FOREIGN KEY (group_id) REFERENCES host_groups (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS alert_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    expr TEXT NOT NULL,
    duration VARCHAR(32) NOT NULL DEFAULT '2m',
    severity VARCHAR(32) NOT NULL DEFAULT 'warning',
    summary VARCHAR(512) NOT NULL DEFAULT '',
    description TEXT NOT NULL,
    enabled TINYINT(1) NOT NULL,
    created_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_alert_rules_name (name),
    KEY idx_alert_rules_enabled (enabled)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS notification_channels (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    type VARCHAR(32) NOT NULL DEFAULT 'webhook',
    url VARCHAR(512) NOT NULL DEFAULT '',
    enabled TINYINT(1) NOT NULL,
    created_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_notification_channels_name (name),
    KEY idx_notification_channels_type (type)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS alert_histories (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    fingerprint VARCHAR(64) NOT NULL DEFAULT '',
    alert_name VARCHAR(128) NOT NULL DEFAULT '',
    instance VARCHAR(256) NOT NULL DEFAULT '',
    severity VARCHAR(32) NOT NULL DEFAULT 'warning',
    status VARCHAR(32) NOT NULL DEFAULT 'firing',
    summary VARCHAR(512) NOT NULL DEFAULT '',
    labels_json TEXT NOT NULL,
    annotations_json TEXT NULL,
    fired_at DATETIME(3) NOT NULL,
    resolved_at DATETIME(3) DEFAULT NULL,
    created_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    KEY idx_alert_history_fingerprint_fired_at (fingerprint, fired_at),
    KEY idx_alert_histories_alert_name (alert_name),
    KEY idx_alert_histories_instance (instance),
    KEY idx_alert_histories_severity (severity),
    KEY idx_alert_histories_status (status),
    KEY idx_alert_histories_fired_at (fired_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS diagnosis_reports (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    alert_history_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    alert_name VARCHAR(128) NOT NULL DEFAULT '',
    target_kind VARCHAR(64) NOT NULL DEFAULT 'host',
    target_name VARCHAR(256) NOT NULL DEFAULT '',
    namespace VARCHAR(128) NOT NULL DEFAULT '',
    severity VARCHAR(32) NOT NULL DEFAULT 'warning',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    summary TEXT NOT NULL,
    root_cause TEXT NOT NULL,
    evidence_json LONGTEXT NOT NULL,
    runbooks_json LONGTEXT NOT NULL,
    recommended_actions_json LONGTEXT NOT NULL,
    rule_analysis_json LONGTEXT NOT NULL,
    confidence DOUBLE NOT NULL DEFAULT 0,
    llm_prompt_hash VARCHAR(64) NOT NULL DEFAULT '',
    llm_model VARCHAR(128) NOT NULL DEFAULT '',
    trigger_type VARCHAR(32) NOT NULL DEFAULT 'manual',
    created_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    KEY idx_diagnosis_reports_alert_history_id (alert_history_id),
    KEY idx_diagnosis_reports_fingerprint (fingerprint),
    KEY idx_diagnosis_reports_alert_name (alert_name),
    KEY idx_diagnosis_reports_severity (severity),
    KEY idx_diagnosis_reports_status (status),
    KEY idx_diagnosis_reports_created_by (created_by),
    KEY idx_diagnosis_reports_created_at (created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS diagnosis_feedback (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    diagnosis_id BIGINT UNSIGNED NOT NULL,
    rating VARCHAR(16) NOT NULL,
    comment VARCHAR(512) NOT NULL DEFAULT '',
    created_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_diagnosis_user (diagnosis_id, created_by),
    KEY idx_diagnosis_feedback_diagnosis_id (diagnosis_id),
    KEY idx_diagnosis_feedback_created_by (created_by)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS pending_actions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    diagnosis_report_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    action_type VARCHAR(64) NOT NULL,
    target_kind VARCHAR(32) NOT NULL,
    target_name VARCHAR(256) NOT NULL,
    namespace VARCHAR(128) NOT NULL,
    params_json LONGTEXT NOT NULL,
    dedupe_key VARCHAR(128) NOT NULL,
    risk_level VARCHAR(16) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    requested_by VARCHAR(32) NOT NULL DEFAULT 'ai-copilot',
    approved_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
    executed_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
    result_json LONGTEXT NOT NULL,
    error_message TEXT NOT NULL,
    created_at DATETIME(3) DEFAULT NULL,
    approved_at DATETIME(3) DEFAULT NULL,
    executed_at DATETIME(3) DEFAULT NULL,
    updated_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_pending_actions_dedupe_key (dedupe_key),
    KEY idx_pending_actions_diagnosis_report_id (diagnosis_report_id),
    KEY idx_pending_actions_action_type (action_type),
    KEY idx_pending_actions_target_name (target_name),
    KEY idx_pending_actions_namespace (namespace),
    KEY idx_pending_actions_risk_level (risk_level),
    KEY idx_pending_actions_status (status),
    KEY idx_pending_actions_created_at (created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor VARCHAR(128) NOT NULL,
    actor_role VARCHAR(32) NOT NULL,
    action VARCHAR(64) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(128) NOT NULL,
    request_json LONGTEXT NOT NULL,
    result VARCHAR(32) NOT NULL,
    error_message TEXT NOT NULL,
    trace_id VARCHAR(64) NOT NULL,
    created_at DATETIME(3) DEFAULT NULL,
    PRIMARY KEY (id),
    KEY idx_audit_logs_actor (actor),
    KEY idx_audit_logs_actor_role (actor_role),
    KEY idx_audit_logs_action (action),
    KEY idx_audit_logs_resource_type (resource_type),
    KEY idx_audit_logs_resource_id (resource_id),
    KEY idx_audit_logs_result (result),
    KEY idx_audit_logs_trace_id (trace_id),
    KEY idx_audit_logs_created_at (created_at)
) ENGINE=InnoDB;
