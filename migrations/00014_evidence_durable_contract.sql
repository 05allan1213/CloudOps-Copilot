-- +goose Up
-- +goose NO TRANSACTION

-- This is an expand contract. Existing Evidence remains identifiable by a
-- NULL evidence_contract_version; every active V3 writer after this migration
-- writes the complete version-1 durable contract. Producer ownership uses
-- real composite foreign keys. Supersession remains normalized in
-- evidence_supersessions, whose owner FKs and increasing IDs already enforce
-- same-cycle, no-self-reference, and acyclic append-only corrections.

ALTER TABLE agent_steps
    ADD UNIQUE KEY uk_agent_steps_v3_owner (id, agent_run_id, incident_id, cycle_no);

ALTER TABLE evidence_items
    ADD COLUMN evidence_contract_version SMALLINT UNSIGNED NULL AFTER domain_schema_version,
    ADD COLUMN producer_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER producer_type,
    ADD COLUMN producer_version VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER producer_id,
    ADD COLUMN agent_step_id BIGINT UNSIGNED NULL AFTER agent_run_id,
    ADD COLUMN verification_run_id BIGINT UNSIGNED NULL AFTER agent_step_id,
    ADD COLUMN verification_check_id BIGINT UNSIGNED NULL AFTER verification_run_id,
    ADD COLUMN change_request_id BIGINT UNSIGNED NULL AFTER verification_check_id,
    ADD COLUMN fact_schema_version SMALLINT UNSIGNED NULL AFTER facts_json,
    ADD COLUMN fact_schema_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER fact_schema_version,
    ADD COLUMN adapter_version VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER producer_dedupe_key,
    ADD COLUMN query_template_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER adapter_version,
    ADD COLUMN query_template_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER query_template_id,
    ADD COLUMN scope_snapshot_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER query_template_version,
    ADD COLUMN arguments_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER scope_snapshot_hash,
    ADD COLUMN observed_at DATETIME(6) NULL AFTER collected_at,
    ADD COLUMN provenance_json JSON NULL AFTER fact_schema_hash,
    ADD COLUMN provenance_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER provenance_json,
    ADD COLUMN trust_axes_json JSON NULL AFTER provenance_hash,
    ADD COLUMN claim_use VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER trust_axes_json,
    ADD COLUMN corroboration_groups_json JSON NULL AFTER claim_use,
    ADD COLUMN input_evidence_ids_json JSON NULL AFTER corroboration_groups_json,
    ADD COLUMN input_sample_ids_json JSON NULL AFTER input_evidence_ids_json,
    ADD COLUMN input_hashes_json JSON NULL AFTER input_sample_ids_json,
    ADD COLUMN source_revision VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER input_hashes_json,
    ADD COLUMN resource_version VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER source_revision,
    ADD COLUMN redaction_policy_version VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER redaction_json,
    ADD COLUMN redaction_counts_json JSON NULL AFTER redaction_policy_version,
    ADD COLUMN prompt_safety_flags_json JSON NULL AFTER redaction_counts_json,
    ADD COLUMN safe_raw_reference VARCHAR(1024) NULL AFTER raw_ref,
    ADD KEY idx_evidence_items_v3_agent_step_owner (agent_step_id, agent_run_id, incident_id, cycle_no),
    ADD KEY idx_evidence_items_v3_verification_owner (verification_check_id, verification_run_id, incident_id, cycle_no),
    ADD KEY idx_evidence_items_v3_change_owner (change_request_id, incident_id, cycle_no),
    ADD CONSTRAINT fk_evidence_items_v3_agent_step_owner
        FOREIGN KEY (agent_step_id, agent_run_id, incident_id, cycle_no)
        REFERENCES agent_steps (id, agent_run_id, incident_id, cycle_no) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_evidence_items_v3_verification_run_owner
        FOREIGN KEY (verification_run_id, incident_id, cycle_no)
        REFERENCES verification_runs (id, incident_id, cycle_no) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_evidence_items_v3_verification_check_owner
        FOREIGN KEY (verification_check_id, verification_run_id, incident_id, cycle_no)
        REFERENCES verification_checks (id, verification_run_id, incident_id, cycle_no) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_evidence_items_v3_change_owner
        FOREIGN KEY (change_request_id, incident_id, cycle_no)
        REFERENCES change_requests (id, incident_id, cycle_no) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_evidence_items_v3_durable_contract CHECK (
        evidence_contract_version IS NULL
        OR (
            evidence_contract_version = 1
            AND producer_type IN ('agent_step','verification_check','delivery_observation','system_enrichment')
            AND CHAR_LENGTH(TRIM(producer_id)) BETWEEN 1 AND 128
            AND CHAR_LENGTH(TRIM(producer_version)) BETWEEN 1 AND 128
            AND CHAR_LENGTH(TRIM(producer_dedupe_key)) BETWEEN 1 AND 128
            AND fact_schema_version > 0
            AND fact_schema_hash REGEXP '^[0-9a-f]{64}$'
            AND content_hash REGEXP '^[0-9a-f]{64}$'
            AND result_hash = content_hash
            AND CHAR_LENGTH(TRIM(adapter_version)) BETWEEN 1 AND 128
            AND CHAR_LENGTH(TRIM(query_template_id)) BETWEEN 1 AND 128
            AND CHAR_LENGTH(TRIM(query_template_version)) BETWEEN 1 AND 64
            AND scope_snapshot_hash REGEXP '^[0-9a-f]{64}$'
            AND arguments_hash REGEXP '^[0-9a-f]{64}$'
            AND observed_at IS NOT NULL
            AND collected_at >= observed_at
            AND JSON_TYPE(facts_json) = 'OBJECT'
            AND COALESCE(JSON_TYPE(JSON_EXTRACT(facts_json, '$.facts')) = 'ARRAY', FALSE)
            AND JSON_LENGTH(JSON_EXTRACT(facts_json, '$.facts')) BETWEEN 1 AND 64
            AND JSON_STORAGE_SIZE(facts_json) <= 16384
            AND provenance_json IS NOT NULL AND JSON_TYPE(provenance_json) = 'OBJECT'
            AND JSON_STORAGE_SIZE(provenance_json) <= 8192
            AND provenance_hash REGEXP '^[0-9a-f]{64}$'
            AND trust_axes_json IS NOT NULL AND JSON_TYPE(trust_axes_json) = 'OBJECT'
            AND JSON_STORAGE_SIZE(trust_axes_json) <= 4096
            AND claim_use IN ('support','blocking','context','forbidden','mixed')
            AND corroboration_groups_json IS NOT NULL AND JSON_TYPE(corroboration_groups_json) = 'ARRAY'
            AND JSON_LENGTH(corroboration_groups_json) BETWEEN 1 AND 64
            AND input_evidence_ids_json IS NOT NULL AND JSON_TYPE(input_evidence_ids_json) = 'ARRAY'
            AND input_sample_ids_json IS NOT NULL AND JSON_TYPE(input_sample_ids_json) = 'ARRAY'
            AND input_hashes_json IS NOT NULL AND JSON_TYPE(input_hashes_json) = 'ARRAY'
            AND JSON_LENGTH(input_hashes_json) = JSON_LENGTH(input_evidence_ids_json) + JSON_LENGTH(input_sample_ids_json)
            AND redaction_policy_version IS NOT NULL
            AND CHAR_LENGTH(TRIM(redaction_policy_version)) BETWEEN 1 AND 128
            AND redaction_counts_json IS NOT NULL AND JSON_TYPE(redaction_counts_json) = 'OBJECT'
            AND prompt_safety_flags_json IS NOT NULL AND JSON_TYPE(prompt_safety_flags_json) = 'OBJECT'
            AND safe_raw_reference IS NOT NULL
            AND (
                (producer_type = 'agent_step'
                 AND agent_step_id IS NOT NULL
                 AND verification_run_id IS NULL AND verification_check_id IS NULL AND change_request_id IS NULL)
                OR
                (producer_type = 'verification_check'
                 AND agent_step_id IS NULL
                 AND verification_run_id IS NOT NULL AND verification_check_id IS NOT NULL AND change_request_id IS NULL)
                OR
                (producer_type = 'delivery_observation'
                 AND agent_step_id IS NULL
                 AND verification_run_id IS NULL AND verification_check_id IS NULL AND change_request_id IS NOT NULL)
                OR
                (producer_type = 'system_enrichment'
                 AND agent_step_id IS NULL AND verification_run_id IS NULL
                 AND verification_check_id IS NULL AND change_request_id IS NULL
                 AND JSON_LENGTH(input_evidence_ids_json) + JSON_LENGTH(input_sample_ids_json) > 0)
            )
        )
    );
