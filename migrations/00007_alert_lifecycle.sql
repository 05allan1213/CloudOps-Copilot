-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `configuration_revisions`
  DROP CHECK `chk_configuration_revisions_escalation`,
  ADD CONSTRAINT `chk_configuration_revisions_escalation`
    CHECK ((`automatic_escalation_enabled` in (0,1)));

CREATE TABLE IF NOT EXISTS `alert_ingress_locks` (
  `alert_key` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `touched_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`alert_key`),
  CONSTRAINT `chk_alert_ingress_locks_key` CHECK ((char_length(`alert_key`) = 64))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `alerts` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `source` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `alert_key` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `current_alert_instance_key` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `correlation_key` varchar(67) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `correlation_key_version` smallint unsigned NOT NULL,
  `fingerprint` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `severity` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `cluster` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `environment` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `namespace` varchar(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `service_name` varchar(255) NOT NULL,
  `target_kind` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `target_name` varchar(255) NOT NULL,
  `category` varchar(255) NOT NULL,
  `summary` varchar(2048) NOT NULL,
  `labels_json` json NOT NULL,
  `annotations_json` json NOT NULL,
  `first_seen_at` datetime(6) NOT NULL,
  `last_seen_at` datetime(6) NOT NULL,
  `starts_at` datetime(6) NOT NULL,
  `resolved_at` datetime(6) DEFAULT NULL,
  `recurrence_count` int unsigned NOT NULL DEFAULT '1',
  `signal_count` int unsigned NOT NULL DEFAULT '1',
  `row_version` bigint unsigned NOT NULL DEFAULT '1',
  `migrated_legacy` tinyint(1) NOT NULL DEFAULT '0',
  `migrated_legacy_context` tinyint(1) NOT NULL DEFAULT '0',
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alerts_public_id` (`public_id`),
  UNIQUE KEY `uk_alerts_source_fingerprint` (`source`,`fingerprint`),
  UNIQUE KEY `uk_alerts_key` (`alert_key`),
  KEY `idx_alerts_status_severity_updated` (`status`,`severity`,`updated_at`,`id`),
  KEY `idx_alerts_scope_updated` (`cluster`,`namespace`,`updated_at`,`id`),
  KEY `idx_alerts_correlation` (`correlation_key`,`updated_at`,`id`),
  CONSTRAINT `chk_alerts_status` CHECK ((`status` in (_ascii'firing',_ascii'resolved'))),
  CONSTRAINT `chk_alerts_severity` CHECK ((`severity` in (_ascii'unknown',_ascii'info',_ascii'warning',_ascii'critical'))),
  CONSTRAINT `chk_alerts_identity` CHECK (((char_length(`alert_key`) = 64) and (char_length(`current_alert_instance_key`) = 64) and (`correlation_key_version` > 0))),
  CONSTRAINT `chk_alerts_counts` CHECK (((`recurrence_count` > 0) and (`signal_count` > 0) and (`row_version` > 0))),
  CONSTRAINT `chk_alerts_resolution` CHECK ((((`status` = _ascii'resolved') and (`resolved_at` is not null)) or ((`status` = _ascii'firing') and (`resolved_at` is null))))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `alert_signal_links` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `alert_id` bigint unsigned NOT NULL,
  `signal_id` bigint unsigned NOT NULL,
  `provenance` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alert_signal_links_public_id` (`public_id`),
  UNIQUE KEY `uk_alert_signal_links_signal` (`signal_id`),
  KEY `idx_alert_signal_links_alert` (`alert_id`,`id`),
  CONSTRAINT `fk_alert_signal_links_alert` FOREIGN KEY (`alert_id`) REFERENCES `alerts` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_alert_signal_links_signal` FOREIGN KEY (`signal_id`) REFERENCES `incident_signals` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_alert_signal_links_provenance` CHECK ((`provenance` in (_ascii'signal_normalization',_ascii'legacy_automatic_ingress')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `alert_events` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `alert_id` bigint unsigned NOT NULL,
  `event_type` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `actor_type` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `actor_id` varchar(128) NOT NULL,
  `source_signal_id` bigint unsigned DEFAULT NULL,
  `idempotency_key` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `summary` varchar(2048) NOT NULL,
  `metadata_json` json NOT NULL,
  `occurred_at` datetime(6) NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alert_events_public_id` (`public_id`),
  UNIQUE KEY `uk_alert_events_idempotency` (`idempotency_key`),
  KEY `idx_alert_events_alert_occurred` (`alert_id`,`occurred_at`,`id`),
  CONSTRAINT `fk_alert_events_alert` FOREIGN KEY (`alert_id`) REFERENCES `alerts` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_alert_events_signal` FOREIGN KEY (`source_signal_id`) REFERENCES `incident_signals` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_alert_events_metadata` CHECK ((json_type(`metadata_json`) = _utf8mb4'OBJECT' and json_storage_size(`metadata_json`) <= 8192))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `alert_acknowledgements` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `alert_id` bigint unsigned NOT NULL,
  `recurrence_no` int unsigned NOT NULL,
  `alert_version` bigint unsigned NOT NULL,
  `actor_provider` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `actor_login` varchar(128) NOT NULL,
  `actor_role` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `reason` varchar(1024) NOT NULL,
  `idempotency_key` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alert_acknowledgements_public_id` (`public_id`),
  UNIQUE KEY `uk_alert_acknowledgements_recurrence` (`alert_id`,`recurrence_no`),
  UNIQUE KEY `uk_alert_acknowledgements_idempotency` (`idempotency_key`),
  CONSTRAINT `fk_alert_acknowledgements_alert` FOREIGN KEY (`alert_id`) REFERENCES `alerts` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_alert_acknowledgements_identity` CHECK (((`recurrence_no` > 0) and (`alert_version` > 0) and (char_length(trim(`reason`)) between 1 and 1024)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `escalation_policies` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `name` varchar(128) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `severities_json` json NOT NULL,
  `namespaces_json` json NOT NULL,
  `label_matchers_json` json NOT NULL,
  `minimum_firing_seconds` int unsigned NOT NULL DEFAULT '0',
  `minimum_recurrence_count` int unsigned NOT NULL DEFAULT '1',
  `create_incident` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_escalation_policies_public_id` (`public_id`),
  UNIQUE KEY `uk_escalation_policies_revision_name` (`configuration_revision_id`,`name`),
  KEY `idx_escalation_policies_revision_enabled` (`configuration_revision_id`,`enabled`,`id`),
  CONSTRAINT `fk_escalation_policies_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_escalation_policies_bounds` CHECK (((json_type(`severities_json`) = _utf8mb4'ARRAY') and (json_length(`severities_json`) between 1 and 4) and (json_storage_size(`severities_json`) <= 256) and (json_type(`namespaces_json`) = _utf8mb4'ARRAY') and (json_length(`namespaces_json`) between 0 and 100) and (json_storage_size(`namespaces_json`) <= 8192) and (json_type(`label_matchers_json`) = _utf8mb4'OBJECT') and (json_length(`label_matchers_json`) <= 8) and (json_storage_size(`label_matchers_json`) <= 4096) and (`minimum_firing_seconds` <= 604800) and (`minimum_recurrence_count` between 1 and 100) and (`create_incident` = 1)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `alert_silences` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `alert_id` bigint unsigned NOT NULL,
  `recurrence_no` int unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `provider_silence_id` varchar(128) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `matchers_json` json NOT NULL,
  `reason` varchar(1024) NOT NULL,
  `starts_at` datetime(6) NOT NULL,
  `ends_at` datetime(6) NOT NULL,
  `expired_at` datetime(6) DEFAULT NULL,
  `provider_error_code` varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `idempotency_key` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  `active_alert_key` bigint unsigned GENERATED ALWAYS AS ((case when (`status` in (_ascii'pending',_ascii'active')) then `alert_id` else NULL end)) STORED,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alert_silences_public_id` (`public_id`),
  UNIQUE KEY `uk_alert_silences_idempotency` (`idempotency_key`),
  UNIQUE KEY `uk_alert_silences_active_alert` (`active_alert_key`),
  KEY `idx_alert_silences_alert_created` (`alert_id`,`created_at`,`id`),
  KEY `idx_alert_silences_provider` (`provider_silence_id`),
  CONSTRAINT `fk_alert_silences_alert` FOREIGN KEY (`alert_id`) REFERENCES `alerts` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_alert_silences_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_alert_silences_status` CHECK ((`status` in (_ascii'pending',_ascii'active',_ascii'expired',_ascii'failed'))),
  CONSTRAINT `chk_alert_silences_bounds` CHECK (((`recurrence_no` > 0) and (`ends_at` > `starts_at`) and (timestampdiff(SECOND,`starts_at`,`ends_at`) between 300 and 86400) and (json_type(`matchers_json`) = _utf8mb4'ARRAY') and (json_length(`matchers_json`) between 1 and 8) and (json_storage_size(`matchers_json`) <= 4096)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `alert_incident_links` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `alert_id` bigint unsigned NOT NULL,
  `incident_id` bigint unsigned NOT NULL,
  `incident_cycle_no` bigint unsigned NOT NULL,
  `provenance` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `configuration_revision_id` bigint unsigned DEFAULT NULL,
  `escalation_policy_id` bigint unsigned DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alert_incident_links_public_id` (`public_id`),
  UNIQUE KEY `uk_alert_incident_links_relation` (`alert_id`,`incident_id`,`incident_cycle_no`),
  KEY `idx_alert_incident_links_incident` (`incident_id`,`incident_cycle_no`,`id`),
  KEY `idx_alert_incident_links_policy` (`escalation_policy_id`,`id`),
  CONSTRAINT `fk_alert_incident_links_alert` FOREIGN KEY (`alert_id`) REFERENCES `alerts` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_alert_incident_links_incident` FOREIGN KEY (`incident_id`) REFERENCES `incidents` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_alert_incident_links_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_alert_incident_links_policy` FOREIGN KEY (`escalation_policy_id`) REFERENCES `escalation_policies` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_alert_incident_links_provenance` CHECK ((`provenance` in (_ascii'owner_created',_ascii'owner_attached',_ascii'escalation_policy',_ascii'legacy_automatic_ingress'))),
  CONSTRAINT `chk_alert_incident_links_identity` CHECK (((`incident_cycle_no` > 0) and (((`provenance` = _ascii'escalation_policy') and (`configuration_revision_id` is not null) and (`escalation_policy_id` is not null)) or ((`provenance` <> _ascii'escalation_policy') and (`escalation_policy_id` is null)))))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO `alerts` (
  `public_id`, `source`, `alert_key`, `current_alert_instance_key`, `correlation_key`,
  `correlation_key_version`, `fingerprint`, `status`, `severity`, `cluster`, `environment`,
  `namespace`, `service_name`, `target_kind`, `target_name`, `category`, `summary`,
  `labels_json`, `annotations_json`, `first_seen_at`, `last_seen_at`, `starts_at`,
  `resolved_at`, `recurrence_count`, `signal_count`, `row_version`, `migrated_legacy`,
  `migrated_legacy_context`, `created_at`, `updated_at`
)
SELECT UUID(), source_signal.`source`, SHA2(CONCAT(
         UNHEX(LPAD(HEX(OCTET_LENGTH('alert')), 8, '0')), 'alert',
         UNHEX(LPAD(HEX(OCTET_LENGTH(source_signal.`source`)), 8, '0')), source_signal.`source`,
         UNHEX(LPAD(HEX(OCTET_LENGTH(source_signal.`fingerprint`)), 8, '0')), source_signal.`fingerprint`
       ), 256),
       source_signal.`alert_instance_key`, COALESCE(linked_incident.`correlation_key`, SHA2(CONCAT('legacy:', source_signal.`alert_instance_key`), 256)),
       2, source_signal.`fingerprint`, source_signal.`status`, source_signal.`severity`, source_signal.`cluster`, source_signal.`environment`,
       source_signal.`namespace`, source_signal.`service_name`, source_signal.`target_kind`, source_signal.`target_name`, source_signal.`category`,
       source_signal.`summary`, source_signal.`labels_json`, source_signal.`annotations_json`, source_signal.`occurred_at`, source_signal.`occurred_at`,
       source_signal.`starts_at`, source_signal.`ends_at`, 1, 1, 1, (source_signal.`incident_id` is not null),
       source_signal.`migrated_legacy_context`, source_signal.`created_at`, source_signal.`created_at`
FROM `incident_signals` AS source_signal
LEFT JOIN `incidents` AS linked_incident ON linked_incident.`id` = source_signal.`incident_id`
WHERE source_signal.`public_id` IS NOT NULL AND source_signal.`alert_instance_key` IS NOT NULL
ORDER BY source_signal.`id`;

UPDATE `alerts` AS alert
JOIN `incident_signals` AS latest
  ON latest.`id` = (
    SELECT MAX(candidate.`id`) FROM `incident_signals` AS candidate
    WHERE candidate.`source` = alert.`source` AND candidate.`fingerprint` = alert.`fingerprint`
  )
LEFT JOIN `incidents` AS incident ON incident.`id` = latest.`incident_id`
SET alert.`current_alert_instance_key` = latest.`alert_instance_key`,
    alert.`correlation_key` = COALESCE(incident.`correlation_key`, alert.`correlation_key`),
    alert.`status` = latest.`status`, alert.`severity` = latest.`severity`,
    alert.`cluster` = latest.`cluster`, alert.`environment` = latest.`environment`,
    alert.`namespace` = latest.`namespace`, alert.`service_name` = latest.`service_name`,
    alert.`target_kind` = latest.`target_kind`, alert.`target_name` = latest.`target_name`,
    alert.`category` = latest.`category`, alert.`summary` = latest.`summary`,
    alert.`labels_json` = latest.`labels_json`, alert.`annotations_json` = latest.`annotations_json`,
    alert.`first_seen_at` = (SELECT MIN(first_signal.`occurred_at`) FROM `incident_signals` AS first_signal WHERE first_signal.`source` = alert.`source` AND first_signal.`fingerprint` = alert.`fingerprint`),
    alert.`last_seen_at` = (SELECT MAX(last_signal.`occurred_at`) FROM `incident_signals` AS last_signal WHERE last_signal.`source` = alert.`source` AND last_signal.`fingerprint` = alert.`fingerprint`),
    alert.`starts_at` = latest.`starts_at`, alert.`resolved_at` = latest.`ends_at`,
    alert.`recurrence_count` = (SELECT COUNT(DISTINCT recurrence.`alert_instance_key`) FROM `incident_signals` AS recurrence WHERE recurrence.`source` = alert.`source` AND recurrence.`fingerprint` = alert.`fingerprint`),
    alert.`signal_count` = (SELECT COUNT(*) FROM `incident_signals` AS counted WHERE counted.`source` = alert.`source` AND counted.`fingerprint` = alert.`fingerprint`),
    alert.`migrated_legacy` = EXISTS(SELECT 1 FROM `incident_signals` AS legacy_signal WHERE legacy_signal.`source` = alert.`source` AND legacy_signal.`fingerprint` = alert.`fingerprint` AND legacy_signal.`incident_id` IS NOT NULL),
    alert.`migrated_legacy_context` = latest.`migrated_legacy_context`,
    alert.`updated_at` = latest.`created_at`;

INSERT IGNORE INTO `alert_signal_links` (`public_id`,`alert_id`,`signal_id`,`provenance`,`created_at`)
SELECT UUID(), alert.`id`, source_signal.`id`,
       IF(source_signal.`incident_id` IS NULL, 'signal_normalization', 'legacy_automatic_ingress'),
       source_signal.`created_at`
FROM `incident_signals` AS source_signal
JOIN `alerts` AS alert ON alert.`source` = source_signal.`source` AND alert.`fingerprint` = source_signal.`fingerprint`
WHERE source_signal.`public_id` IS NOT NULL AND source_signal.`alert_instance_key` IS NOT NULL;

INSERT IGNORE INTO `alert_incident_links` (
  `public_id`,`alert_id`,`incident_id`,`incident_cycle_no`,`provenance`,`created_at`
)
SELECT UUID(), alert.`id`, source_signal.`incident_id`, source_signal.`cycle_no`, 'legacy_automatic_ingress', MIN(source_signal.`created_at`)
FROM `incident_signals` AS source_signal
JOIN `alerts` AS alert ON alert.`source` = source_signal.`source` AND alert.`fingerprint` = source_signal.`fingerprint`
WHERE source_signal.`incident_id` IS NOT NULL AND source_signal.`cycle_no` IS NOT NULL
GROUP BY alert.`id`, source_signal.`incident_id`, source_signal.`cycle_no`;

INSERT IGNORE INTO `alert_events` (
  `public_id`,`alert_id`,`event_type`,`actor_type`,`actor_id`,`idempotency_key`,
  `summary`,`metadata_json`,`occurred_at`,`created_at`
)
SELECT UUID(), alert.`id`, 'alert_history_imported', 'migration', 'alert-lifecycle',
       SHA2(CONCAT('alert-history-imported:', alert.`public_id`), 256),
       'Existing automatic Signal-to-Incident history linked without rewriting source facts',
       JSON_OBJECT('provenance', 'legacy_automatic_ingress', 'signal_count', alert.`signal_count`),
       alert.`updated_at`, NOW(6)
FROM `alerts` AS alert
WHERE alert.`migrated_legacy` = 1;
