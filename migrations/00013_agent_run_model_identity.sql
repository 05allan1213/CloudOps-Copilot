-- +goose Up
-- +goose NO TRANSACTION

-- Existing rows remain readable with an all-NULL expanded identity. Every
-- AgentRun created by the V3 worker after this migration writes the complete
-- immutable provider/model/prompt/tool identity in one INSERT.
ALTER TABLE agent_runs
    ADD COLUMN model_provider VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER model,
    ADD COLUMN actual_model VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER model_provider,
    ADD COLUMN prompt_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER prompt_version,
    ADD COLUMN tool_schema_version VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER prompt_hash,
    ADD COLUMN tool_schema_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER tool_schema_version,
    ADD CONSTRAINT chk_agent_runs_model_identity CHECK (
        (model_provider IS NULL AND actual_model IS NULL AND prompt_hash IS NULL
         AND tool_schema_version IS NULL AND tool_schema_hash IS NULL)
        OR
        (CHAR_LENGTH(TRIM(model_provider)) BETWEEN 1 AND 64
         AND LOWER(TRIM(model_provider)) <> 'configured'
         AND CHAR_LENGTH(TRIM(actual_model)) BETWEEN 1 AND 128
         AND LOWER(TRIM(actual_model)) <> 'configured'
         AND CHAR_LENGTH(TRIM(prompt_version)) BETWEEN 1 AND 128
         AND LOWER(TRIM(prompt_version)) <> 'configured'
         AND prompt_hash REGEXP '^[0-9a-f]{64}$'
         AND CHAR_LENGTH(TRIM(tool_schema_version)) BETWEEN 1 AND 128
         AND LOWER(TRIM(tool_schema_version)) <> 'configured'
         AND tool_schema_hash REGEXP '^[0-9a-f]{64}$')
    );

ALTER TABLE agent_steps
    ADD COLUMN provider_request_id_hashes JSON NULL AFTER output_tokens,
    ADD CONSTRAINT chk_agent_steps_provider_request_ids CHECK (
        provider_request_id_hashes IS NULL
        OR (JSON_TYPE(provider_request_id_hashes) = 'ARRAY'
            AND JSON_LENGTH(provider_request_id_hashes) BETWEEN 1 AND 2
            AND JSON_STORAGE_SIZE(provider_request_id_hashes) <= 256)
    );

-- +goose Down
-- +goose NO TRANSACTION

ALTER TABLE agent_steps
    DROP CHECK chk_agent_steps_provider_request_ids,
    DROP COLUMN provider_request_id_hashes;

ALTER TABLE agent_runs
    DROP CHECK chk_agent_runs_model_identity,
    DROP COLUMN tool_schema_hash,
    DROP COLUMN tool_schema_version,
    DROP COLUMN prompt_hash,
    DROP COLUMN actual_model,
    DROP COLUMN model_provider;
