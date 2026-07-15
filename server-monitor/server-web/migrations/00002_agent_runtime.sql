-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE agent_runs
    ADD COLUMN idempotency_key VARCHAR(128) NULL AFTER incident_id,
    ADD COLUMN attempt INT NOT NULL DEFAULT 1 AFTER idempotency_key,
    ADD COLUMN objective VARCHAR(2048) NOT NULL DEFAULT '' AFTER status,
    ADD COLUMN max_tool_calls INT NOT NULL DEFAULT 1 AFTER used_steps,
    ADD COLUMN used_tool_calls INT NOT NULL DEFAULT 0 AFTER max_tool_calls,
    ADD COLUMN max_model_calls INT NOT NULL DEFAULT 1 AFTER used_tool_calls,
    ADD COLUMN used_model_calls INT NOT NULL DEFAULT 0 AFTER max_model_calls,
    ADD COLUMN token_budget BIGINT NOT NULL DEFAULT 1 AFTER used_model_calls,
    ADD COLUMN max_evidence_items INT NOT NULL DEFAULT 1 AFTER output_tokens,
    ADD COLUMN used_evidence_items INT NOT NULL DEFAULT 0 AFTER max_evidence_items,
    ADD COLUMN max_runtime_ms BIGINT NOT NULL DEFAULT 120000 AFTER used_evidence_items,
	ADD COLUMN tool_timeout_ms BIGINT NOT NULL DEFAULT 15000 AFTER max_runtime_ms,
	ADD COLUMN max_evidence_bytes INT NOT NULL DEFAULT 16384 AFTER tool_timeout_ms,
	ADD COLUMN max_checkpoint_bytes INT NOT NULL DEFAULT 32768 AFTER max_evidence_bytes,
	ADD COLUMN max_step_retries INT NOT NULL DEFAULT 1 AFTER max_checkpoint_bytes,
    ADD COLUMN checkpoint_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER current_checkpoint,
    ADD COLUMN checkpoint_schema_version INT NOT NULL DEFAULT 1 AFTER checkpoint_version,
    ADD COLUMN checkpoint_hash CHAR(64) NOT NULL DEFAULT '' AFTER checkpoint_schema_version,
    ADD COLUMN lease_owner VARCHAR(128) NOT NULL DEFAULT '' AFTER checkpoint_hash,
    ADD COLUMN lease_expires_at DATETIME(6) NULL AFTER lease_owner,
    ADD COLUMN heartbeat_at DATETIME(6) NULL AFTER lease_expires_at,
    ADD COLUMN cancel_requested_at DATETIME(6) NULL AFTER heartbeat_at,
    ADD COLUMN failure_summary VARCHAR(2048) NOT NULL DEFAULT '' AFTER failure_code,
    ADD COLUMN deadline_at DATETIME(6) NULL AFTER completed_at,
    ADD COLUMN row_version BIGINT UNSIGNED NOT NULL DEFAULT 1 AFTER deadline_at,
    ADD COLUMN active_incident_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status IN ('PENDING', 'RUNNING') THEN incident_id ELSE NULL END
    ) STORED,
    ADD UNIQUE KEY uk_agent_runs_incident_idempotency (incident_id, idempotency_key),
    ADD UNIQUE KEY uk_agent_runs_one_active_incident (active_incident_id),
    ADD KEY idx_agent_runs_claim (status, lease_expires_at, created_at, id),
    ADD CONSTRAINT chk_agent_runs_runtime_budgets CHECK (
        max_tool_calls > 0 AND used_tool_calls >= 0 AND used_tool_calls <= max_tool_calls AND
        max_model_calls > 0 AND used_model_calls >= 0 AND used_model_calls <= max_model_calls AND
        token_budget > 0 AND input_tokens + output_tokens <= token_budget AND
        max_evidence_items > 0 AND used_evidence_items >= 0 AND used_evidence_items <= max_evidence_items AND
		max_runtime_ms > 0 AND tool_timeout_ms > 0 AND max_evidence_bytes > 0 AND
		max_checkpoint_bytes > 0 AND max_step_retries >= 0
    ),
    ADD CONSTRAINT chk_agent_runs_runtime_versions CHECK (
        attempt > 0 AND checkpoint_version >= 0 AND checkpoint_schema_version > 0 AND row_version > 0
    );

ALTER TABLE agent_steps
    ADD COLUMN arguments_hash CHAR(64) NOT NULL DEFAULT '' AFTER arguments_json,
    ADD COLUMN evidence_public_id CHAR(36) NOT NULL DEFAULT '' AFTER result_ref,
    ADD COLUMN retry_count INT NOT NULL DEFAULT 0 AFTER status,
    ADD COLUMN error_code VARCHAR(64) NOT NULL DEFAULT '' AFTER output_tokens,
    ADD COLUMN started_at DATETIME(6) NULL AFTER error_code,
    ADD COLUMN finished_at DATETIME(6) NULL AFTER started_at,
    ADD CONSTRAINT chk_agent_steps_retry CHECK (retry_count >= 0);

ALTER TABLE evidence_items
    ADD COLUMN tool_name VARCHAR(128) NOT NULL DEFAULT '' AFTER source,
    ADD COLUMN result_hash CHAR(64) NOT NULL DEFAULT '' AFTER facts_json,
    ADD COLUMN redaction_json JSON NULL AFTER raw_ref,
    ADD COLUMN valid BOOLEAN NOT NULL DEFAULT TRUE AFTER truncated,
    ADD COLUMN idempotency_key CHAR(64) NULL AFTER valid,
    ADD UNIQUE KEY uk_evidence_items_run_idempotency (agent_run_id, idempotency_key);

-- +goose Down
-- +goose NO TRANSACTION

ALTER TABLE evidence_items
    DROP INDEX uk_evidence_items_run_idempotency,
    DROP COLUMN idempotency_key,
    DROP COLUMN valid,
    DROP COLUMN redaction_json,
    DROP COLUMN result_hash,
    DROP COLUMN tool_name;

ALTER TABLE agent_steps
    DROP CHECK chk_agent_steps_retry,
    DROP COLUMN finished_at,
    DROP COLUMN started_at,
    DROP COLUMN error_code,
    DROP COLUMN retry_count,
    DROP COLUMN evidence_public_id,
    DROP COLUMN arguments_hash;

ALTER TABLE agent_runs
    DROP CHECK chk_agent_runs_runtime_versions,
    DROP CHECK chk_agent_runs_runtime_budgets,
    DROP INDEX idx_agent_runs_claim,
    DROP INDEX uk_agent_runs_one_active_incident,
    DROP INDEX uk_agent_runs_incident_idempotency,
    DROP COLUMN active_incident_id,
    DROP COLUMN row_version,
    DROP COLUMN deadline_at,
    DROP COLUMN failure_summary,
    DROP COLUMN cancel_requested_at,
    DROP COLUMN heartbeat_at,
    DROP COLUMN lease_expires_at,
    DROP COLUMN lease_owner,
    DROP COLUMN checkpoint_hash,
    DROP COLUMN checkpoint_schema_version,
    DROP COLUMN checkpoint_version,
    DROP COLUMN max_runtime_ms,
	DROP COLUMN max_step_retries,
	DROP COLUMN max_checkpoint_bytes,
	DROP COLUMN max_evidence_bytes,
	DROP COLUMN tool_timeout_ms,
    DROP COLUMN used_evidence_items,
    DROP COLUMN max_evidence_items,
    DROP COLUMN token_budget,
    DROP COLUMN used_model_calls,
    DROP COLUMN max_model_calls,
    DROP COLUMN used_tool_calls,
    DROP COLUMN max_tool_calls,
    DROP COLUMN objective,
    DROP COLUMN attempt,
    DROP COLUMN idempotency_key;
