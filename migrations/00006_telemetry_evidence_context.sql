-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `query_definitions`
  DROP CHECK `chk_query_definitions_provider`,
  ADD CONSTRAINT `chk_query_definitions_provider`
    CHECK ((`provider` in (_ascii'prometheus',_ascii'elasticsearch',_ascii'tempo')));

ALTER TABLE `query_authorizations`
  DROP CHECK `chk_query_authorizations_provider`,
  ADD CONSTRAINT `chk_query_authorizations_provider`
    CHECK ((`provider` in (_ascii'prometheus',_ascii'elasticsearch',_ascii'tempo')));

ALTER TABLE `query_executions`
  DROP CHECK `chk_query_executions_provider`,
  ADD CONSTRAINT `chk_query_executions_provider`
    CHECK ((`provider` in (_ascii'prometheus',_ascii'elasticsearch',_ascii'tempo')));

ALTER TABLE `evidence_items`
  DROP CHECK `chk_evidence_items_durable_contract`,
  DROP CHECK `chk_evidence_items_identity`,
  MODIFY COLUMN `incident_id` bigint unsigned DEFAULT NULL,
  ADD COLUMN `query_execution_id` bigint unsigned DEFAULT NULL AFTER `cycle_no`,
  ADD UNIQUE KEY `uk_evidence_items_query_content` (`query_execution_id`,`content_hash`),
  ADD KEY `idx_evidence_items_query_collected` (`query_execution_id`,`collected_at`,`id`),
  ADD CONSTRAINT `fk_evidence_items_query_execution`
    FOREIGN KEY (`query_execution_id`) REFERENCES `query_executions` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `chk_evidence_items_identity` CHECK ((
    (
      `incident_id` is not null AND `cycle_no` is not null AND `cycle_no` > 0 AND
      `query_execution_id` is null AND `producer_type` is not null AND
      `producer_type` <> _ascii'query_execution'
    ) OR (
      `incident_id` is null AND `cycle_no` is null AND `query_execution_id` is not null AND
      `agent_step_id` is null AND `verification_run_id` is null AND `verification_check_id` is null AND
      `change_request_id` is null AND `change_id` is null AND
      `producer_type` = _ascii'query_execution'
    )
  ) AND `producer_dedupe_key` is not null AND `content_hash` is not null AND char_length(`content_hash`) = 64),
  ADD CONSTRAINT `chk_evidence_items_durable_contract` CHECK ((
    `evidence_contract_version` is null OR (
      `evidence_contract_version` = 1 AND
      `producer_type` in (_ascii'agent_step',_ascii'verification_check',_ascii'delivery_observation',_ascii'system_enrichment',_ascii'query_execution') AND
      char_length(trim(`producer_id`)) between 1 and 128 AND
      char_length(trim(`producer_version`)) between 1 and 128 AND
      char_length(trim(`producer_dedupe_key`)) between 1 and 128 AND
      `fact_schema_version` > 0 AND regexp_like(`fact_schema_hash`,_ascii'^[0-9a-f]{64}$') AND
      regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND `result_hash` = `content_hash` AND
      char_length(trim(`adapter_version`)) between 1 and 128 AND
      char_length(trim(`query_template_id`)) between 1 and 128 AND
      char_length(trim(`query_template_version`)) between 1 and 64 AND
      regexp_like(`scope_snapshot_hash`,_ascii'^[0-9a-f]{64}$') AND
      regexp_like(`arguments_hash`,_ascii'^[0-9a-f]{64}$') AND
      `observed_at` is not null AND `collected_at` >= `observed_at` AND
      json_type(`facts_json`) = _utf8mb4'OBJECT' AND
      json_type(json_extract(`facts_json`,_utf8mb4'$.facts')) = _utf8mb4'ARRAY' AND
      json_length(json_extract(`facts_json`,_utf8mb4'$.facts')) between 1 and 64 AND
      json_storage_size(`facts_json`) <= 16384 AND
      `provenance_json` is not null AND json_type(`provenance_json`) = _utf8mb4'OBJECT' AND
      json_storage_size(`provenance_json`) <= 8192 AND regexp_like(`provenance_hash`,_ascii'^[0-9a-f]{64}$') AND
      `trust_axes_json` is not null AND json_type(`trust_axes_json`) = _utf8mb4'OBJECT' AND
      json_storage_size(`trust_axes_json`) <= 4096 AND
      `claim_use` in (_ascii'support',_ascii'blocking',_ascii'context',_ascii'forbidden',_ascii'mixed') AND
      `corroboration_groups_json` is not null AND json_type(`corroboration_groups_json`) = _utf8mb4'ARRAY' AND
      json_length(`corroboration_groups_json`) between 1 and 64 AND
      `input_evidence_ids_json` is not null AND json_type(`input_evidence_ids_json`) = _utf8mb4'ARRAY' AND
      `input_sample_ids_json` is not null AND json_type(`input_sample_ids_json`) = _utf8mb4'ARRAY' AND
      `input_hashes_json` is not null AND json_type(`input_hashes_json`) = _utf8mb4'ARRAY' AND
      json_length(`input_hashes_json`) = (json_length(`input_evidence_ids_json`) + json_length(`input_sample_ids_json`)) AND
      `redaction_policy_version` is not null AND char_length(trim(`redaction_policy_version`)) between 1 and 128 AND
      `redaction_counts_json` is not null AND json_type(`redaction_counts_json`) = _utf8mb4'OBJECT' AND
      `prompt_safety_flags_json` is not null AND json_type(`prompt_safety_flags_json`) = _utf8mb4'OBJECT' AND
      `safe_raw_reference` is not null AND (
        (`producer_type` = _ascii'agent_step' AND `agent_step_id` is not null AND `verification_run_id` is null AND `verification_check_id` is null AND `change_request_id` is null AND `query_execution_id` is null) OR
        (`producer_type` = _ascii'verification_check' AND `agent_step_id` is null AND `verification_run_id` is not null AND `verification_check_id` is not null AND `change_request_id` is null AND `query_execution_id` is null) OR
        (`producer_type` = _ascii'delivery_observation' AND `agent_step_id` is null AND `verification_run_id` is null AND `verification_check_id` is null AND `change_request_id` is not null AND `query_execution_id` is null) OR
        (`producer_type` = _ascii'system_enrichment' AND `agent_step_id` is null AND `verification_run_id` is null AND `verification_check_id` is null AND `change_request_id` is null AND `query_execution_id` is null AND (json_length(`input_evidence_ids_json`) + json_length(`input_sample_ids_json`)) > 0) OR
        (`producer_type` = _ascii'query_execution' AND `query_execution_id` is not null AND `incident_id` is null AND `cycle_no` is null AND `agent_step_id` is null AND `verification_run_id` is null AND `verification_check_id` is null AND `change_request_id` is null)
      )
    )
  ));

CREATE TABLE `agent_consultations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `title` varchar(128) NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_consultations_public_id` (`public_id`),
  KEY `idx_agent_consultations_updated` (`updated_at`,`id`),
  CONSTRAINT `chk_agent_consultations_title` CHECK ((char_length(trim(`title`)) between 2 and 128)),
  CONSTRAINT `chk_agent_consultations_status` CHECK ((`status` in (_ascii'open',_ascii'archived'))),
  CONSTRAINT `chk_agent_consultations_actor` CHECK ((`created_by` = _ascii'local-owner'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `context_snapshots` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `consultation_id` bigint unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `cluster_id` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `environment` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `namespaces_json` json NOT NULL,
  `resource_refs_json` json NOT NULL,
  `range_start` datetime(6) NOT NULL,
  `range_end` datetime(6) NOT NULL,
  `query_execution_refs_json` json NOT NULL,
  `evidence_refs_json` json NOT NULL,
  `content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_context_snapshots_public_id` (`public_id`),
  UNIQUE KEY `uk_context_snapshots_consultation_content` (`consultation_id`,`content_hash`),
  KEY `idx_context_snapshots_created` (`created_at`,`id`),
  CONSTRAINT `fk_context_snapshots_consultation` FOREIGN KEY (`consultation_id`) REFERENCES `agent_consultations` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_context_snapshots_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_context_snapshots_scope` CHECK ((
    char_length(trim(`cluster_id`)) between 1 and 128 AND
    char_length(trim(`environment`)) between 1 and 64 AND
    json_type(`namespaces_json`) = _utf8mb4'ARRAY' AND json_length(`namespaces_json`) between 1 and 100 AND
    json_type(`resource_refs_json`) = _utf8mb4'ARRAY' AND json_length(`resource_refs_json`) between 1 and 32 AND
    `range_end` > `range_start`
  )),
  CONSTRAINT `chk_context_snapshots_references` CHECK ((
    json_type(`query_execution_refs_json`) = _utf8mb4'ARRAY' AND json_length(`query_execution_refs_json`) between 0 and 32 AND
    json_type(`evidence_refs_json`) = _utf8mb4'ARRAY' AND json_length(`evidence_refs_json`) between 0 and 32 AND
    (json_length(`query_execution_refs_json`) + json_length(`evidence_refs_json`)) between 1 and 64 AND
    json_storage_size(`resource_refs_json`) <= 16384 AND
    json_storage_size(`query_execution_refs_json`) <= 4096 AND
    json_storage_size(`evidence_refs_json`) <= 4096 AND
    regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND `created_by` = _ascii'local-owner'
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
