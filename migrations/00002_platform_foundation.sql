-- +goose Up
-- +goose NO TRANSACTION

CREATE TABLE `configuration_revisions` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `revision_number` bigint unsigned NOT NULL,
  `configuration_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `summary` varchar(255) NOT NULL,
  `query_max_lookback_seconds` int unsigned NOT NULL,
  `query_max_results` int unsigned NOT NULL,
  `telemetry_retention_days` int unsigned NOT NULL,
  `browser_notifications_enabled` tinyint(1) NOT NULL DEFAULT '0',
  `automatic_escalation_enabled` tinyint(1) NOT NULL DEFAULT '0',
  `created_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_configuration_revisions_public_id` (`public_id`),
  UNIQUE KEY `uk_configuration_revisions_number` (`revision_number`),
  KEY `idx_configuration_revisions_hash` (`configuration_hash`,`revision_number`),
  CONSTRAINT `chk_configuration_revisions_hash` CHECK ((char_length(`configuration_hash`) = 64)),
  CONSTRAINT `chk_configuration_revisions_limits` CHECK (((`revision_number` > 0) and (`query_max_lookback_seconds` between 60 and 2592000) and (`query_max_results` between 1 and 10000) and (`telemetry_retention_days` between 1 and 365))),
  CONSTRAINT `chk_configuration_revisions_escalation` CHECK ((`automatic_escalation_enabled` = 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `secret_versions` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `provider` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `purpose` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `fingerprint` char(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `relative_path` varchar(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `state` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_secret_versions_public_id` (`public_id`),
  UNIQUE KEY `uk_secret_versions_path` (`relative_path`),
  KEY `idx_secret_versions_provider_purpose` (`provider`,`purpose`,`created_at`,`id`),
  CONSTRAINT `chk_secret_versions_provider` CHECK ((`provider` in (_ascii'llm',_ascii'kubernetes',_ascii'prometheus',_ascii'alertmanager',_ascii'elasticsearch',_ascii'tempo',_ascii'github',_ascii'argocd'))),
  CONSTRAINT `chk_secret_versions_state` CHECK ((`state` in (_ascii'configured',_ascii'invalid')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `provider_configurations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `provider` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT '0',
  `endpoint` varchar(2048) NOT NULL DEFAULT '',
  `model` varchar(255) NOT NULL DEFAULT '',
  `timeout_ms` int unsigned NOT NULL,
  `max_results` int unsigned NOT NULL,
  `context_link_base` varchar(2048) NOT NULL DEFAULT '',
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_provider_configurations_revision_provider` (`configuration_revision_id`,`provider`),
  CONSTRAINT `fk_provider_configurations_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_provider_configurations_provider` CHECK ((`provider` in (_ascii'llm',_ascii'kubernetes',_ascii'prometheus',_ascii'alertmanager',_ascii'elasticsearch',_ascii'tempo',_ascii'github',_ascii'argocd'))),
  CONSTRAINT `chk_provider_configurations_limits` CHECK (((`timeout_ms` between 1000 and 60000) and (`max_results` between 1 and 10000)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `configuration_secret_references` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `provider` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `purpose` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `secret_version_id` bigint unsigned NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_configuration_secret_references_identity` (`configuration_revision_id`,`provider`,`purpose`),
  KEY `idx_configuration_secret_references_secret` (`secret_version_id`,`configuration_revision_id`),
  CONSTRAINT `fk_configuration_secret_references_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_configuration_secret_references_secret` FOREIGN KEY (`secret_version_id`) REFERENCES `secret_versions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_configuration_secret_references_provider` CHECK ((`provider` in (_ascii'llm',_ascii'kubernetes',_ascii'prometheus',_ascii'alertmanager',_ascii'elasticsearch',_ascii'tempo',_ascii'github',_ascii'argocd')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `operational_scopes` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `name` varchar(128) NOT NULL,
  `cluster_id` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `environment` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `namespaces_json` json NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_operational_scopes_public_id` (`public_id`),
  UNIQUE KEY `uk_operational_scopes_revision` (`configuration_revision_id`),
  CONSTRAINT `fk_operational_scopes_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_operational_scopes_namespaces` CHECK (((json_type(`namespaces_json`) = _utf8mb4'ARRAY') and (json_length(`namespaces_json`) between 1 and 100) and (json_storage_size(`namespaces_json`) <= 8192)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `active_configuration` (
  `singleton_id` tinyint unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`singleton_id`),
  UNIQUE KEY `uk_active_configuration_revision` (`configuration_revision_id`),
  CONSTRAINT `fk_active_configuration_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_active_configuration_singleton` CHECK ((`singleton_id` = 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `configuration_validations` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `draft_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `draft_json` json NOT NULL,
  `valid` tinyint(1) NOT NULL,
  `errors_json` json NOT NULL,
  `provider_results_json` json NOT NULL,
	`applied_revision_id` bigint unsigned DEFAULT NULL,
  `created_by` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `expires_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_configuration_validations_public_id` (`public_id`),
	UNIQUE KEY `uk_configuration_validations_applied_revision` (`applied_revision_id`),
  KEY `idx_configuration_validations_hash_expiry` (`draft_hash`,`expires_at`,`id`),
	CONSTRAINT `fk_configuration_validations_applied_revision` FOREIGN KEY (`applied_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_configuration_validations_hash` CHECK ((char_length(`draft_hash`) = 64)),
  CONSTRAINT `chk_configuration_validations_size` CHECK (((json_storage_size(`draft_json`) <= 65536) and (json_storage_size(`errors_json`) <= 16384) and (json_storage_size(`provider_results_json`) <= 16384)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `provider_health` (
  `provider` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `configuration_revision_id` bigint unsigned DEFAULT NULL,
  `state` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `detail` varchar(1024) NOT NULL,
  `checked_at` datetime(6) DEFAULT NULL,
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`provider`),
  KEY `idx_provider_health_revision` (`configuration_revision_id`,`provider`),
  CONSTRAINT `fk_provider_health_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_provider_health_provider` CHECK ((`provider` in (_ascii'mysql',_ascii'llm',_ascii'kubernetes',_ascii'prometheus',_ascii'alertmanager',_ascii'elasticsearch',_ascii'tempo',_ascii'github',_ascii'argocd'))),
  CONSTRAINT `chk_provider_health_state` CHECK ((`state` in (_ascii'available',_ascii'partial',_ascii'unavailable',_ascii'disabled',_ascii'not_configured')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `configuration_activation_tasks` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'ready',
  `worker_id` varchar(128) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `attempt` int unsigned NOT NULL DEFAULT '0',
  `available_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `observed_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `observed_at` datetime(6) DEFAULT NULL,
  `last_error` varchar(1024) DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_configuration_activation_tasks_public_id` (`public_id`),
  UNIQUE KEY `uk_configuration_activation_tasks_revision` (`configuration_revision_id`),
  KEY `idx_configuration_activation_tasks_claim` (`status`,`available_at`,`id`),
  CONSTRAINT `fk_configuration_activation_tasks_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_configuration_activation_tasks_status` CHECK ((`status` in (_ascii'ready',_ascii'running',_ascii'succeeded',_ascii'failed'))),
  CONSTRAINT `chk_configuration_activation_tasks_attempt` CHECK ((`attempt` <= 10))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `owner_notifications` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source_type` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source_public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source_state` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `severity` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `reason` varchar(2048) NOT NULL,
  `context_workspace` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `context_path` varchar(512) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `context_query_json` json NOT NULL,
  `operational_scope_public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `dedupe_identity` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `read_at` datetime(6) DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_owner_notifications_public_id` (`public_id`),
  UNIQUE KEY `uk_owner_notifications_dedupe` (`dedupe_identity`),
  KEY `idx_owner_notifications_unread` (`read_at`,`created_at`,`id`),
  CONSTRAINT `chk_owner_notifications_severity` CHECK ((`severity` in (_ascii'P1',_ascii'P2',_ascii'P3',_ascii'info'))),
  CONSTRAINT `chk_owner_notifications_context` CHECK (((left(`context_path`,1) = _utf8mb4'/') and (json_type(`context_query_json`) = _utf8mb4'OBJECT') and (json_storage_size(`context_query_json`) <= 4096)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `backup_records` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `backup_name` varchar(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `schema_version` bigint unsigned NOT NULL,
  `schema_identity` char(71) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source_commit` char(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_backup_records_public_id` (`public_id`),
  UNIQUE KEY `uk_backup_records_name` (`backup_name`),
  KEY `idx_backup_records_created` (`created_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `configuration_revisions` (
  `public_id`, `revision_number`, `configuration_hash`, `summary`,
  `query_max_lookback_seconds`, `query_max_results`, `telemetry_retention_days`,
  `browser_notifications_enabled`, `automatic_escalation_enabled`, `created_by`
) VALUES (
  UUID(), 1, SHA2('cloudops-default-operational-configuration', 256), 'Initial local operational configuration',
  86400, 1000, 7, 0, 0, 'migration'
);

SET @cloudops_initial_configuration_revision_id = LAST_INSERT_ID();

INSERT INTO `provider_configurations` (
  `configuration_revision_id`, `provider`, `enabled`, `endpoint`, `model`, `timeout_ms`, `max_results`, `context_link_base`
) VALUES
  (@cloudops_initial_configuration_revision_id, 'llm', 0, 'https://api.deepseek.com/v1', 'deepseek-chat', 60000, 800, ''),
  (@cloudops_initial_configuration_revision_id, 'kubernetes', 0, '', '', 10000, 200, ''),
  (@cloudops_initial_configuration_revision_id, 'prometheus', 0, '', '', 5000, 1000, ''),
  (@cloudops_initial_configuration_revision_id, 'alertmanager', 0, '', '', 5000, 200, ''),
  (@cloudops_initial_configuration_revision_id, 'elasticsearch', 0, '', '', 10000, 1000, ''),
  (@cloudops_initial_configuration_revision_id, 'tempo', 0, '', '', 10000, 200, ''),
  (@cloudops_initial_configuration_revision_id, 'github', 0, 'https://api.github.com', '', 10000, 200, 'https://github.com'),
  (@cloudops_initial_configuration_revision_id, 'argocd', 0, '', '', 10000, 200, '');

INSERT INTO `operational_scopes` (
  `public_id`, `configuration_revision_id`, `name`, `cluster_id`, `environment`, `namespaces_json`
) VALUES (UUID(), @cloudops_initial_configuration_revision_id, '本地 CloudOps', 'cloudops-local', 'local', JSON_ARRAY('demo'));

INSERT INTO `active_configuration` (`singleton_id`, `configuration_revision_id`)
VALUES (1, @cloudops_initial_configuration_revision_id);

INSERT INTO `provider_health` (`provider`, `configuration_revision_id`, `state`, `detail`, `checked_at`) VALUES
  ('mysql', @cloudops_initial_configuration_revision_id, 'available', 'MySQL schema and durable configuration are available', NOW(6)),
  ('llm', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('kubernetes', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('prometheus', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('alertmanager', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('elasticsearch', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('tempo', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('github', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL),
  ('argocd', @cloudops_initial_configuration_revision_id, 'disabled', 'Provider is disabled in the active revision', NULL);

INSERT INTO `configuration_activation_tasks` (`public_id`, `configuration_revision_id`, `status`)
VALUES (UUID(), @cloudops_initial_configuration_revision_id, 'ready');

ALTER TABLE `async_tasks`
  ADD COLUMN `configuration_revision_id` bigint unsigned DEFAULT NULL AFTER `payload_json`,
  ADD COLUMN `configuration_observed_at` datetime(6) DEFAULT NULL AFTER `configuration_revision_id`;

UPDATE `async_tasks`
SET `configuration_revision_id` = @cloudops_initial_configuration_revision_id
WHERE `configuration_revision_id` IS NULL;

ALTER TABLE `async_tasks`
  MODIFY COLUMN `configuration_revision_id` bigint unsigned NOT NULL,
  ADD KEY `idx_async_tasks_configuration_revision` (`configuration_revision_id`,`created_at`,`id`),
  ADD CONSTRAINT `fk_async_tasks_configuration_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT;

ALTER TABLE `async_task_attempts`
  ADD COLUMN `configuration_revision_id` bigint unsigned DEFAULT NULL AFTER `task_id`;

UPDATE `async_task_attempts` AS attempt
JOIN `async_tasks` AS task ON task.`id` = attempt.`task_id`
SET attempt.`configuration_revision_id` = task.`configuration_revision_id`
WHERE attempt.`configuration_revision_id` IS NULL;

ALTER TABLE `async_task_attempts`
  MODIFY COLUMN `configuration_revision_id` bigint unsigned NOT NULL,
  ADD KEY `idx_async_task_attempts_configuration_revision` (`configuration_revision_id`,`created_at`,`id`),
  ADD CONSTRAINT `fk_async_task_attempts_configuration_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT;
