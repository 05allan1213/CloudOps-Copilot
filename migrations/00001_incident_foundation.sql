-- +goose Up
-- +goose NO TRANSACTION

CREATE TABLE incidents (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    fingerprint VARCHAR(128) NOT NULL,
    correlation_key VARCHAR(67) NOT NULL,
    cluster VARCHAR(255) NOT NULL,
    namespace VARCHAR(255) NOT NULL,
    service_name VARCHAR(255) NOT NULL,
    environment VARCHAR(255) NOT NULL,
    target_kind VARCHAR(64) NOT NULL,
    target_name VARCHAR(255) NOT NULL,
    severity VARCHAR(16) NOT NULL,
    status VARCHAR(32) NOT NULL,
    summary VARCHAR(2048) NOT NULL,
    first_seen_at DATETIME(6) NOT NULL,
    last_seen_at DATETIME(6) NOT NULL,
    resolved_at DATETIME(6) NULL,
    current_agent_run_id BIGINT UNSIGNED NULL,
    version BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_incidents_public_id (public_id),
    KEY idx_incidents_status_updated (status, updated_at, id),
    KEY idx_incidents_severity_updated (severity, updated_at, id),
    KEY idx_incidents_cluster_namespace (cluster, namespace, updated_at),
    KEY idx_incidents_service (service_name, updated_at),
    KEY idx_incidents_correlation_open (correlation_key, status, last_seen_at),
    KEY idx_incidents_fingerprint_open (fingerprint, status, last_seen_at),
    CONSTRAINT chk_incidents_version CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE agent_runs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL,
    model VARCHAR(128) NOT NULL,
    prompt_version VARCHAR(128) NOT NULL,
    max_steps INT NOT NULL,
    used_steps INT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    current_checkpoint JSON NULL,
    final_diagnosis JSON NULL,
    failure_code VARCHAR(128) NOT NULL,
    started_at DATETIME(6) NULL,
    completed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_runs_public_id (public_id),
    KEY idx_agent_runs_incident_created (incident_id, created_at, id),
    KEY idx_agent_runs_status_updated (status, updated_at),
    CONSTRAINT fk_agent_runs_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT chk_agent_runs_steps CHECK (max_steps > 0 AND used_steps >= 0 AND used_steps <= max_steps),
    CONSTRAINT chk_agent_runs_tokens CHECK (input_tokens >= 0 AND output_tokens >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE incidents
    ADD CONSTRAINT fk_incidents_current_agent_run
    FOREIGN KEY (current_agent_run_id) REFERENCES agent_runs (id) ON DELETE SET NULL;

CREATE TABLE agent_steps (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    agent_run_id BIGINT UNSIGNED NOT NULL,
    sequence INT NOT NULL,
    step_type VARCHAR(64) NOT NULL,
    short_reason VARCHAR(1024) NOT NULL,
    selected_tool VARCHAR(128) NOT NULL,
    arguments_json JSON NULL,
    result_summary VARCHAR(4096) NOT NULL,
    result_ref VARCHAR(1024) NOT NULL,
    status VARCHAR(32) NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_steps_public_id (public_id),
    UNIQUE KEY uk_agent_steps_run_sequence (agent_run_id, sequence),
    KEY idx_agent_steps_run_created (agent_run_id, created_at),
    CONSTRAINT fk_agent_steps_run FOREIGN KEY (agent_run_id) REFERENCES agent_runs (id) ON DELETE RESTRICT,
    CONSTRAINT chk_agent_steps_sequence CHECK (sequence > 0),
    CONSTRAINT chk_agent_steps_usage CHECK (duration_ms >= 0 AND input_tokens >= 0 AND output_tokens >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE incident_signals (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    incident_id BIGINT UNSIGNED NULL,
    source VARCHAR(64) NOT NULL,
    source_event_id VARCHAR(67) NOT NULL,
    fingerprint VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL,
    severity VARCHAR(16) NOT NULL,
    cluster VARCHAR(255) NOT NULL,
    namespace VARCHAR(255) NOT NULL,
    service_name VARCHAR(255) NOT NULL,
    environment VARCHAR(255) NOT NULL,
    target_kind VARCHAR(64) NOT NULL,
    target_name VARCHAR(255) NOT NULL,
    category VARCHAR(255) NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    received_at DATETIME(6) NOT NULL,
    summary VARCHAR(2048) NOT NULL,
    labels_json JSON NOT NULL,
    annotations_json JSON NOT NULL,
    raw_payload JSON NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_incident_signals_source_event (source, source_event_id),
    KEY idx_incident_signals_incident_occurred (incident_id, occurred_at, id),
    KEY idx_incident_signals_fingerprint (fingerprint, occurred_at),
    CONSTRAINT fk_incident_signals_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE incident_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    incident_id BIGINT UNSIGNED NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    actor_type VARCHAR(32) NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    summary VARCHAR(2048) NOT NULL,
    metadata_json JSON NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_incident_events_incident_occurred (incident_id, occurred_at, id),
    KEY idx_incident_events_type_occurred (event_type, occurred_at),
    CONSTRAINT fk_incident_events_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE evidence_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    agent_run_id BIGINT UNSIGNED NULL,
    type VARCHAR(64) NOT NULL,
    source VARCHAR(128) NOT NULL,
    resource_ref VARCHAR(1024) NOT NULL,
    time_range_json JSON NULL,
    query_text VARCHAR(4096) NOT NULL,
    summary VARCHAR(4096) NOT NULL,
    facts_json JSON NOT NULL,
    raw_ref VARCHAR(1024) NOT NULL,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    collected_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_evidence_items_public_id (public_id),
    KEY idx_evidence_items_incident_collected (incident_id, collected_at, id),
    KEY idx_evidence_items_run_collected (agent_run_id, collected_at),
    CONSTRAINT fk_evidence_items_incident FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_evidence_items_agent_run FOREIGN KEY (agent_run_id) REFERENCES agent_runs (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE outbox_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id VARCHAR(128) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    schema_version INT NOT NULL,
    payload_json JSON NOT NULL,
    occurred_at DATETIME(6) NOT NULL,
    published_at DATETIME(6) NULL,
    attempts INT NOT NULL DEFAULT 0,
    last_error VARCHAR(2048) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_outbox_events_event_id (event_id),
    KEY idx_outbox_events_pending (published_at, created_at, id),
    KEY idx_outbox_events_aggregate (aggregate_type, aggregate_id, created_at),
    CONSTRAINT chk_outbox_schema_version CHECK (schema_version > 0),
    CONSTRAINT chk_outbox_attempts CHECK (attempts >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE incident_correlation_locks (
    correlation_key VARCHAR(67) NOT NULL,
    touched_at DATETIME(6) NOT NULL,
    PRIMARY KEY (correlation_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
-- +goose NO TRANSACTION

DROP TABLE IF EXISTS incident_correlation_locks;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS evidence_items;
DROP TABLE IF EXISTS incident_events;
DROP TABLE IF EXISTS incident_signals;
DROP TABLE IF EXISTS agent_steps;
ALTER TABLE incidents DROP FOREIGN KEY fk_incidents_current_agent_run;
DROP TABLE IF EXISTS agent_runs;
DROP TABLE IF EXISTS incidents;

