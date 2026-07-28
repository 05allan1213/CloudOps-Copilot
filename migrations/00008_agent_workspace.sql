-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `agent_runs`
  DROP CHECK `chk_agent_runs_identity`,
  MODIFY COLUMN `incident_id` bigint unsigned DEFAULT NULL,
  MODIFY COLUMN `expected_incident_version` bigint unsigned NOT NULL DEFAULT '0',
  ADD COLUMN `subject_type` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'incident' AFTER `public_id`,
  ADD COLUMN `alert_id` bigint unsigned DEFAULT NULL AFTER `incident_id`,
  ADD COLUMN `consultation_id` bigint unsigned DEFAULT NULL AFTER `alert_id`,
  ADD COLUMN `configuration_revision_id` bigint unsigned DEFAULT NULL AFTER `consultation_id`,
  ADD COLUMN `context_snapshot_id` bigint unsigned DEFAULT NULL AFTER `configuration_revision_id`,
  ADD COLUMN `run_kind` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'incident' AFTER `idempotency_key`,
  ADD COLUMN `outcome` varchar(16) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL AFTER `final_diagnosis`,
  ADD COLUMN `uncertainty` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'unknown' AFTER `outcome`,
  ADD COLUMN `active_workspace_subject_key` varchar(48) CHARACTER SET ascii COLLATE ascii_bin
    GENERATED ALWAYS AS ((case
      when (`run_kind` = _ascii'workspace' and `status` in (_ascii'pending',_ascii'running') and `subject_type` = _ascii'alert') then concat(_ascii'alert:',`alert_id`)
      when (`run_kind` = _ascii'workspace' and `status` in (_ascii'pending',_ascii'running') and `subject_type` = _ascii'consultation') then concat(_ascii'consultation:',`consultation_id`)
      else NULL end)) STORED,
  ADD UNIQUE KEY `uk_agent_runs_workspace_idempotency` (`subject_type`,`alert_id`,`consultation_id`,`idempotency_key`),
  ADD UNIQUE KEY `uk_agent_runs_active_workspace_subject` (`active_workspace_subject_key`),
  ADD KEY `idx_agent_runs_subject_created` (`subject_type`,`alert_id`,`consultation_id`,`created_at`,`id`),
  ADD KEY `idx_agent_runs_workspace_claim` (`run_kind`,`status`,`lease_expires_at`,`created_at`,`id`),
  ADD KEY `idx_agent_runs_configuration_revision` (`configuration_revision_id`,`created_at`,`id`),
  ADD CONSTRAINT `fk_agent_runs_alert` FOREIGN KEY (`alert_id`) REFERENCES `alerts` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_agent_runs_consultation` FOREIGN KEY (`consultation_id`) REFERENCES `agent_consultations` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_agent_runs_configuration_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `chk_agent_runs_identity` CHECK ((
    (`subject_type` = _ascii'incident' AND `run_kind` = _ascii'incident' AND `incident_id` is not null AND
      `alert_id` is null AND `consultation_id` is null AND `cycle_no` > 0 AND `expected_incident_version` > 0) OR
    (`subject_type` = _ascii'alert' AND `run_kind` = _ascii'workspace' AND `incident_id` is null AND
      `alert_id` is not null AND `consultation_id` is null AND `cycle_no` is null AND `expected_incident_version` = 0 AND
      `configuration_revision_id` is not null) OR
    (`subject_type` = _ascii'consultation' AND `run_kind` = _ascii'workspace' AND `incident_id` is null AND
      `alert_id` is null AND `consultation_id` is not null AND `cycle_no` is null AND `expected_incident_version` = 0 AND
      `configuration_revision_id` is not null)
  )),
  ADD CONSTRAINT `chk_agent_runs_outcome` CHECK ((
    (`outcome` is null AND `status` in (_ascii'pending',_ascii'running')) OR
    (`outcome` in (_ascii'diagnosed',_ascii'insufficient',_ascii'cancelled',_ascii'failed') AND
      `status` in (_ascii'completed',_ascii'failed',_ascii'cancelled')) OR
    (`run_kind` = _ascii'incident' AND `outcome` is null AND `status` in (_ascii'completed',_ascii'failed',_ascii'cancelled'))
  )),
  ADD CONSTRAINT `chk_agent_runs_uncertainty` CHECK ((`uncertainty` in (_ascii'unknown',_ascii'low',_ascii'medium',_ascii'high')));

ALTER TABLE `agent_steps`
  DROP CHECK `chk_agent_steps_identity`,
  ADD CONSTRAINT `chk_agent_steps_identity` CHECK ((
    (`incident_id` is not null AND `cycle_no` is not null AND `cycle_no` > 0) OR
    (`incident_id` is null AND `cycle_no` is null)
  ));

ALTER TABLE `context_snapshots`
  DROP CHECK `chk_context_snapshots_references`,
  MODIFY COLUMN `consultation_id` bigint unsigned DEFAULT NULL,
  ADD COLUMN `agent_run_id` bigint unsigned DEFAULT NULL AFTER `consultation_id`,
  ADD COLUMN `subject_type` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'consultation' AFTER `agent_run_id`,
  ADD COLUMN `filters_json` json DEFAULT NULL AFTER `resource_refs_json`,
  ADD COLUMN `query_definition_refs_json` json DEFAULT NULL AFTER `query_execution_refs_json`,
  ADD UNIQUE KEY `uk_context_snapshots_run` (`agent_run_id`),
  ADD KEY `idx_context_snapshots_consultation_created` (`consultation_id`,`created_at`,`id`),
  ADD CONSTRAINT `fk_context_snapshots_agent_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `chk_context_snapshots_references` CHECK ((
    ((`subject_type` = _ascii'consultation' AND `consultation_id` is not null AND `agent_run_id` is null AND
       (json_length(`query_execution_refs_json`) + json_length(`evidence_refs_json`)) between 1 and 64) OR
     (`subject_type` in (_ascii'alert',_ascii'incident') AND `consultation_id` is null AND `agent_run_id` is not null AND
       (json_length(`query_execution_refs_json`) + json_length(`evidence_refs_json`)) between 0 and 64)) AND
    json_type(`filters_json`) = _utf8mb4'OBJECT' AND json_storage_size(`filters_json`) <= 8192 AND
    json_type(`query_definition_refs_json`) = _utf8mb4'ARRAY' AND json_length(`query_definition_refs_json`) between 0 and 32 AND
    json_type(`query_execution_refs_json`) = _utf8mb4'ARRAY' AND json_length(`query_execution_refs_json`) between 0 and 32 AND
    json_type(`evidence_refs_json`) = _utf8mb4'ARRAY' AND json_length(`evidence_refs_json`) between 0 and 32 AND
    json_storage_size(`resource_refs_json`) <= 16384 AND
    json_storage_size(`query_definition_refs_json`) <= 4096 AND
    json_storage_size(`query_execution_refs_json`) <= 4096 AND
    json_storage_size(`evidence_refs_json`) <= 4096 AND
    regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND `created_by` = _ascii'local-owner'
  ));

UPDATE `context_snapshots`
SET `filters_json` = JSON_OBJECT(), `query_definition_refs_json` = JSON_ARRAY()
WHERE `filters_json` is null OR `query_definition_refs_json` is null;

ALTER TABLE `context_snapshots`
  MODIFY COLUMN `filters_json` json NOT NULL,
  MODIFY COLUMN `query_definition_refs_json` json NOT NULL;

ALTER TABLE `agent_runs`
  ADD CONSTRAINT `fk_agent_runs_context_snapshot` FOREIGN KEY (`context_snapshot_id`) REFERENCES `context_snapshots` (`id`) ON DELETE RESTRICT;

ALTER TABLE `evidence_items`
  DROP CHECK `chk_evidence_items_identity`,
  ADD KEY `idx_evidence_items_agent_step` (`agent_step_id`),
  ADD CONSTRAINT `fk_evidence_items_agent_step` FOREIGN KEY (`agent_step_id`) REFERENCES `agent_steps` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `chk_evidence_items_identity` CHECK ((
    ((`incident_id` is not null AND `cycle_no` is not null AND `cycle_no` > 0 AND
       `query_execution_id` is null AND `producer_type` is not null AND `producer_type` <> _ascii'query_execution') OR
     (`incident_id` is null AND `cycle_no` is null AND `query_execution_id` is not null AND
       `agent_step_id` is null AND `verification_run_id` is null AND `verification_check_id` is null AND
       `change_request_id` is null AND `change_id` is null AND `producer_type` = _ascii'query_execution') OR
     (`incident_id` is null AND `cycle_no` is null AND `query_execution_id` is null AND
       `agent_step_id` is not null AND `producer_type` = _ascii'agent_step' AND
       `verification_run_id` is null AND `verification_check_id` is null AND
       `change_request_id` is null AND `change_id` is null)) AND
    `producer_dedupe_key` is not null AND `content_hash` is not null AND char_length(`content_hash`) = 64
  ));

CREATE TABLE `agent_consultation_messages` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `consultation_id` bigint unsigned NOT NULL,
  `agent_run_id` bigint unsigned DEFAULT NULL,
  `context_snapshot_id` bigint unsigned NOT NULL,
  `sequence` int unsigned NOT NULL,
  `role` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `content` text NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `completed_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_consultation_messages_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_consultation_messages_sequence` (`consultation_id`,`sequence`),
  KEY `idx_agent_consultation_messages_run` (`agent_run_id`,`id`),
  CONSTRAINT `fk_agent_consultation_messages_consultation` FOREIGN KEY (`consultation_id`) REFERENCES `agent_consultations` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_consultation_messages_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_consultation_messages_snapshot` FOREIGN KEY (`context_snapshot_id`) REFERENCES `context_snapshots` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_consultation_messages_role` CHECK ((`role` in (_ascii'owner',_ascii'assistant'))),
  CONSTRAINT `chk_agent_consultation_messages_status` CHECK ((`status` in (_ascii'accepted',_ascii'completed',_ascii'failed',_ascii'cancelled'))),
  CONSTRAINT `chk_agent_consultation_messages_content` CHECK ((char_length(trim(`content`)) between 1 and 16000))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_stream_events` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `agent_run_id` bigint unsigned NOT NULL,
  `consultation_id` bigint unsigned DEFAULT NULL,
  `sequence` int unsigned NOT NULL,
  `event_type` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `payload_json` json NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_stream_events_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_stream_events_sequence` (`agent_run_id`,`sequence`),
  KEY `idx_agent_stream_events_consultation` (`consultation_id`,`id`),
  CONSTRAINT `fk_agent_stream_events_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_stream_events_consultation` FOREIGN KEY (`consultation_id`) REFERENCES `agent_consultations` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_stream_events_type` CHECK ((`event_type` in (
    _ascii'run.created',_ascii'run.started',_ascii'tool.started',_ascii'tool.completed',_ascii'tool.failed',
    _ascii'answer.delta',_ascii'answer.completed',_ascii'run.completed',_ascii'run.failed',_ascii'run.cancelled'))),
  CONSTRAINT `chk_agent_stream_events_payload` CHECK ((json_type(`payload_json`) = _utf8mb4'OBJECT' AND json_storage_size(`payload_json`) <= 16384))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_evidence_citations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `agent_run_id` bigint unsigned NOT NULL,
  `message_id` bigint unsigned DEFAULT NULL,
  `evidence_item_id` bigint unsigned NOT NULL,
  `citation_use` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_evidence_citations_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_evidence_citations_identity` (`agent_run_id`,`message_id`,`evidence_item_id`,`citation_use`),
  CONSTRAINT `fk_agent_evidence_citations_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_evidence_citations_message` FOREIGN KEY (`message_id`) REFERENCES `agent_consultation_messages` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_evidence_citations_evidence` FOREIGN KEY (`evidence_item_id`) REFERENCES `evidence_items` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_evidence_citations_use` CHECK ((`citation_use` in (_ascii'support',_ascii'context',_ascii'blocking')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `knowledge_items` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `title` varchar(128) NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `current_revision_no` int unsigned NOT NULL,
  `created_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_knowledge_items_public_id` (`public_id`),
  KEY `idx_knowledge_items_status_updated` (`status`,`updated_at`,`id`),
  CONSTRAINT `chk_knowledge_items_status` CHECK ((`status` in (_ascii'active',_ascii'disabled',_ascii'deleted'))),
  CONSTRAINT `chk_knowledge_items_identity` CHECK ((char_length(trim(`title`)) between 2 and 128 AND `current_revision_no` > 0 AND `created_by` = _ascii'local-owner'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `knowledge_item_revisions` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `knowledge_item_id` bigint unsigned NOT NULL,
  `revision_no` int unsigned NOT NULL,
  `content` text NOT NULL,
  `content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source_type` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source_consultation_id` bigint unsigned DEFAULT NULL,
  `source_message_id` bigint unsigned DEFAULT NULL,
  `cluster_id` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `environment` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `namespaces_json` json NOT NULL,
  `resource_refs_json` json NOT NULL,
  `review_at` datetime(6) DEFAULT NULL,
  `expires_at` datetime(6) DEFAULT NULL,
  `confirmed_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_knowledge_item_revisions_public_id` (`public_id`),
  UNIQUE KEY `uk_knowledge_item_revisions_number` (`knowledge_item_id`,`revision_no`),
  UNIQUE KEY `uk_knowledge_item_revisions_content` (`knowledge_item_id`,`content_hash`),
  KEY `idx_knowledge_item_revisions_scope` (`cluster_id`,`environment`,`created_at`,`id`),
  CONSTRAINT `fk_knowledge_item_revisions_item` FOREIGN KEY (`knowledge_item_id`) REFERENCES `knowledge_items` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_knowledge_item_revisions_consultation` FOREIGN KEY (`source_consultation_id`) REFERENCES `agent_consultations` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_knowledge_item_revisions_message` FOREIGN KEY (`source_message_id`) REFERENCES `agent_consultation_messages` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_knowledge_item_revisions_source` CHECK ((
    (`source_type` = _ascii'manual' AND `source_consultation_id` is null AND `source_message_id` is null) OR
    (`source_type` = _ascii'consultation' AND `source_consultation_id` is not null AND `source_message_id` is not null)
  )),
  CONSTRAINT `chk_knowledge_item_revisions_scope` CHECK ((
    `revision_no` > 0 AND char_length(trim(`content`)) between 1 and 16000 AND
    regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND
    char_length(trim(`cluster_id`)) between 1 and 128 AND char_length(trim(`environment`)) between 1 and 64 AND
    json_type(`namespaces_json`) = _utf8mb4'ARRAY' AND json_length(`namespaces_json`) between 1 and 100 AND
    json_type(`resource_refs_json`) = _utf8mb4'ARRAY' AND json_length(`resource_refs_json`) between 0 and 32 AND
    (`expires_at` is null OR `expires_at` > `created_at`) AND `confirmed_by` = _ascii'local-owner'
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_guidance_citations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `agent_run_id` bigint unsigned NOT NULL,
  `message_id` bigint unsigned DEFAULT NULL,
  `guidance_type` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `knowledge_revision_id` bigint unsigned DEFAULT NULL,
  `runbook_path` varchar(512) DEFAULT NULL,
  `runbook_revision` char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_guidance_citations_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_guidance_citations_knowledge` (`agent_run_id`,`knowledge_revision_id`),
  UNIQUE KEY `uk_agent_guidance_citations_runbook` (`agent_run_id`,`runbook_path`,`runbook_revision`),
  CONSTRAINT `fk_agent_guidance_citations_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_guidance_citations_message` FOREIGN KEY (`message_id`) REFERENCES `agent_consultation_messages` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_guidance_citations_knowledge` FOREIGN KEY (`knowledge_revision_id`) REFERENCES `knowledge_item_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_guidance_citations_identity` CHECK ((
    (`guidance_type` = _ascii'knowledge' AND `knowledge_revision_id` is not null AND `runbook_path` is null AND `runbook_revision` is null) OR
    (`guidance_type` = _ascii'runbook' AND `knowledge_revision_id` is null AND char_length(trim(`runbook_path`)) between 1 and 512 AND regexp_like(`runbook_revision`,_ascii'^[0-9a-f]{64}$'))
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_action_cards` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `agent_run_id` bigint unsigned NOT NULL,
  `action_type` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `target_json` json NOT NULL,
  `parameters_json` json NOT NULL,
  `preconditions_json` json NOT NULL,
  `risk` varchar(1024) NOT NULL,
  `content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `expires_at` datetime(6) NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_action_cards_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_action_cards_content` (`agent_run_id`,`content_hash`),
  CONSTRAINT `fk_agent_action_cards_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_action_cards_status` CHECK ((`status` in (_ascii'proposed',_ascii'authorized',_ascii'expired',_ascii'cancelled'))),
  CONSTRAINT `chk_agent_action_cards_content` CHECK ((
    char_length(trim(`action_type`)) between 1 and 64 AND json_type(`target_json`) = _utf8mb4'OBJECT' AND
    json_type(`parameters_json`) = _utf8mb4'OBJECT' AND json_type(`preconditions_json`) = _utf8mb4'ARRAY' AND
    char_length(trim(`risk`)) between 1 and 1024 AND regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND `expires_at` > `created_at`
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_operation_plans` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `agent_run_id` bigint unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `operation_type` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `target_json` json NOT NULL,
  `parameters_json` json NOT NULL,
  `intended_state_json` json NOT NULL,
  `preconditions_json` json NOT NULL,
  `risk` varchar(2048) NOT NULL,
  `verification_intent_json` json NOT NULL,
  `content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `expires_at` datetime(6) NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_operation_plans_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_operation_plans_content` (`agent_run_id`,`content_hash`),
  CONSTRAINT `fk_agent_operation_plans_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_operation_plans_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_operation_plans_status` CHECK ((`status` in (_ascii'proposed',_ascii'authorized',_ascii'expired',_ascii'cancelled'))),
  CONSTRAINT `chk_agent_operation_plans_content` CHECK ((
    char_length(trim(`operation_type`)) between 1 and 64 AND json_type(`target_json`) = _utf8mb4'OBJECT' AND
    json_type(`parameters_json`) = _utf8mb4'OBJECT' AND json_type(`intended_state_json`) = _utf8mb4'OBJECT' AND
    json_type(`preconditions_json`) = _utf8mb4'ARRAY' AND char_length(trim(`risk`)) between 1 and 2048 AND
    json_type(`verification_intent_json`) = _utf8mb4'OBJECT' AND regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND
    `expires_at` > `created_at`
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_action_authorizations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `subject_type` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `action_card_id` bigint unsigned DEFAULT NULL,
  `operation_plan_id` bigint unsigned DEFAULT NULL,
  `authorized_content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `authorized_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `reason` varchar(1024) NOT NULL,
  `expires_at` datetime(6) NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_action_authorizations_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_action_authorizations_card` (`action_card_id`),
  UNIQUE KEY `uk_agent_action_authorizations_plan` (`operation_plan_id`),
  CONSTRAINT `fk_agent_action_authorizations_card` FOREIGN KEY (`action_card_id`) REFERENCES `agent_action_cards` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_action_authorizations_plan` FOREIGN KEY (`operation_plan_id`) REFERENCES `agent_operation_plans` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_action_authorizations_identity` CHECK ((
    (`subject_type` = _ascii'action_card' AND `action_card_id` is not null AND `operation_plan_id` is null) OR
    (`subject_type` = _ascii'operation_plan' AND `action_card_id` is null AND `operation_plan_id` is not null)
  )),
  CONSTRAINT `chk_agent_action_authorizations_content` CHECK ((
    regexp_like(`authorized_content_hash`,_ascii'^[0-9a-f]{64}$') AND `authorized_by` = _ascii'local-owner' AND
    char_length(trim(`reason`)) between 1 and 1024 AND `expires_at` > `created_at`
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
