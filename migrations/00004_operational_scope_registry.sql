-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `operational_scopes`
  DROP INDEX `uk_operational_scopes_revision`,
  ADD COLUMN `is_default` tinyint(1) NOT NULL DEFAULT '0' AFTER `namespaces_json`,
  ADD UNIQUE KEY `uk_operational_scopes_revision_cluster` (`configuration_revision_id`,`cluster_id`),
  ADD KEY `idx_operational_scopes_revision_default` (`configuration_revision_id`,`is_default`,`id`);

UPDATE `operational_scopes` SET `is_default` = 1;

CREATE TABLE `active_operational_scope` (
  `singleton_id` tinyint unsigned NOT NULL,
  `operational_scope_id` bigint unsigned NOT NULL,
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`singleton_id`),
  UNIQUE KEY `uk_active_operational_scope` (`operational_scope_id`),
  CONSTRAINT `fk_active_operational_scope_scope` FOREIGN KEY (`operational_scope_id`) REFERENCES `operational_scopes` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_active_operational_scope_singleton` CHECK ((`singleton_id` = 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `active_operational_scope` (`singleton_id`, `operational_scope_id`)
SELECT 1, scope.`id`
FROM `active_configuration` AS active
JOIN `operational_scopes` AS scope
  ON scope.`configuration_revision_id` = active.`configuration_revision_id`
WHERE active.`singleton_id` = 1 AND scope.`is_default` = 1
ORDER BY scope.`id`
LIMIT 1;
