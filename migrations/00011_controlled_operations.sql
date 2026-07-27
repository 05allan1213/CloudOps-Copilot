-- +goose Up
-- +goose NO TRANSACTION

-- Operation Plans and Action Authorizations remain owned by the Agent
-- Workspace tables. These rows add durable execution, audit, and current
-- verification facts without creating a second plan or approval source.
CREATE TABLE `operation_executions` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `subject_type` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `action_card_id` bigint unsigned DEFAULT NULL,
  `operation_plan_id` bigint unsigned DEFAULT NULL,
  `authorization_id` bigint unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `expected_content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'ready',
  `attempt` int unsigned NOT NULL DEFAULT '0',
  `max_attempts` int unsigned NOT NULL DEFAULT '2',
  `lease_owner` varchar(128) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `lease_generation` bigint unsigned NOT NULL DEFAULT '0',
  `lease_expires_at` datetime(6) DEFAULT NULL,
  `external_effect_started_at` datetime(6) DEFAULT NULL,
  `external_effect_marker` char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `result_json` json DEFAULT NULL,
  `failure_code` varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `failure_summary` varchar(2048) DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `started_at` datetime(6) DEFAULT NULL,
  `completed_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_operation_executions_public_id` (`public_id`),
  UNIQUE KEY `uk_operation_executions_card` (`action_card_id`),
  UNIQUE KEY `uk_operation_executions_plan` (`operation_plan_id`),
  KEY `idx_operation_executions_claim` (`status`,`lease_expires_at`,`created_at`,`id`),
  KEY `idx_operation_executions_revision` (`configuration_revision_id`,`created_at`,`id`),
  CONSTRAINT `fk_operation_executions_card` FOREIGN KEY (`action_card_id`) REFERENCES `agent_action_cards` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_operation_executions_plan` FOREIGN KEY (`operation_plan_id`) REFERENCES `agent_operation_plans` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_operation_executions_authorization` FOREIGN KEY (`authorization_id`) REFERENCES `agent_action_authorizations` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_operation_executions_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_operation_executions_subject` CHECK ((
    (`subject_type` = _ascii'action_card' AND `action_card_id` is not null AND `operation_plan_id` is null) OR
    (`subject_type` = _ascii'operation_plan' AND `action_card_id` is null AND `operation_plan_id` is not null)
  )),
  CONSTRAINT `chk_operation_executions_status` CHECK ((`status` in (
    _ascii'ready',_ascii'running',_ascii'succeeded',_ascii'failed',
    _ascii'precondition_failed',_ascii'verification_failed',_ascii'cancelled'
  ))),
  CONSTRAINT `chk_operation_executions_hash` CHECK ((regexp_like(`expected_content_hash`,_ascii'^[0-9a-f]{64}$'))),
  CONSTRAINT `chk_operation_executions_attempt` CHECK ((`max_attempts` = 2 AND `attempt` <= `max_attempts`)),
  CONSTRAINT `chk_operation_executions_external_effect` CHECK ((
    (`external_effect_started_at` is null AND `external_effect_marker` is null) OR
    (`external_effect_started_at` is not null AND regexp_like(`external_effect_marker`,_ascii'^[0-9a-f]{64}$'))
  )),
  CONSTRAINT `chk_operation_executions_terminal` CHECK ((
    (`status` in (_ascii'ready',_ascii'running') AND `completed_at` is null) OR
    (`status` not in (_ascii'ready',_ascii'running') AND `completed_at` is not null)
  )),
  CONSTRAINT `chk_operation_executions_result_size` CHECK ((
    (`result_json` is null OR json_storage_size(`result_json`) <= 32768) AND
    (`failure_summary` is null OR char_length(`failure_summary`) <= 2048)
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `operation_events` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `execution_id` bigint unsigned NOT NULL,
  `sequence_no` int unsigned NOT NULL,
  `event_type` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `payload_json` json NOT NULL,
  `content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `occurred_at` datetime(6) NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_operation_events_public_id` (`public_id`),
  UNIQUE KEY `uk_operation_events_sequence` (`execution_id`,`sequence_no`),
  KEY `idx_operation_events_occurred` (`occurred_at`,`id`),
  CONSTRAINT `fk_operation_events_execution` FOREIGN KEY (`execution_id`) REFERENCES `operation_executions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_operation_events_content` CHECK ((
    char_length(trim(`event_type`)) between 1 and 64 AND json_type(`payload_json`) = _utf8mb4'OBJECT' AND
    json_storage_size(`payload_json`) <= 16384 AND regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$')
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `operation_verification_observations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `execution_id` bigint unsigned NOT NULL,
  `source` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `provider_identity_json` json NOT NULL,
  `evidence_json` json NOT NULL,
  `content_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `summary` varchar(1024) NOT NULL,
  `observed_at` datetime(6) NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_operation_verification_public_id` (`public_id`),
  UNIQUE KEY `uk_operation_verification_execution` (`execution_id`),
  KEY `idx_operation_verification_observed` (`observed_at`,`id`),
  CONSTRAINT `fk_operation_verification_execution` FOREIGN KEY (`execution_id`) REFERENCES `operation_executions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_operation_verification_source` CHECK ((`source` in (_ascii'local',_ascii'kubernetes'))),
  CONSTRAINT `chk_operation_verification_status` CHECK ((`status` in (_ascii'passed',_ascii'failed'))),
  CONSTRAINT `chk_operation_verification_content` CHECK ((
    json_type(`provider_identity_json`) = _utf8mb4'OBJECT' AND json_storage_size(`provider_identity_json`) <= 8192 AND
    json_type(`evidence_json`) = _utf8mb4'OBJECT' AND json_storage_size(`evidence_json`) <= 16384 AND
    regexp_like(`content_hash`,_ascii'^[0-9a-f]{64}$') AND char_length(trim(`summary`)) between 1 and 1024
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- A local change freeze is a bounded, reversible product action. It is also a
-- fail-closed precondition for Kubernetes effects on the same exact target.
CREATE TABLE `operation_change_freezes` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `target_identity_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `cluster_id` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `environment` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `namespace` varchar(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `workload_kind` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `workload_name` varchar(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `enabled` tinyint(1) NOT NULL,
  `reason` varchar(1024) NOT NULL,
  `row_version` bigint unsigned NOT NULL DEFAULT '1',
  `updated_by_execution_id` bigint unsigned NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_operation_change_freezes_public_id` (`public_id`),
  UNIQUE KEY `uk_operation_change_freezes_target` (`target_identity_hash`),
  CONSTRAINT `fk_operation_change_freezes_execution` FOREIGN KEY (`updated_by_execution_id`) REFERENCES `operation_executions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_operation_change_freezes_identity` CHECK ((
    regexp_like(`target_identity_hash`,_ascii'^[0-9a-f]{64}$') AND
    char_length(trim(`cluster_id`)) between 1 and 128 AND char_length(trim(`environment`)) between 1 and 128 AND
    char_length(trim(`namespace`)) between 1 and 253 AND `workload_kind` = _ascii'Deployment' AND
    char_length(trim(`workload_name`)) between 1 and 253 AND char_length(trim(`reason`)) between 1 and 1024 AND
    `row_version` > 0
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
