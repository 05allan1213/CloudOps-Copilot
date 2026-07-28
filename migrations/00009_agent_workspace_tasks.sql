-- +goose Up
-- +goose NO TRANSACTION

CREATE TABLE `agent_workspace_tasks` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `agent_run_id` bigint unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `task_type` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'ready',
  `priority` int NOT NULL DEFAULT '0',
  `available_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `attempt` int unsigned NOT NULL DEFAULT '0',
  `max_attempts` int unsigned NOT NULL DEFAULT '2',
  `lease_owner` varchar(128) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `lease_generation` bigint unsigned NOT NULL DEFAULT '0',
  `lease_expires_at` datetime(6) DEFAULT NULL,
  `heartbeat_at` datetime(6) DEFAULT NULL,
  `last_error_code` varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `last_error_summary` varchar(2048) DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  `started_at` datetime(6) DEFAULT NULL,
  `completed_at` datetime(6) DEFAULT NULL,
  `dead_at` datetime(6) DEFAULT NULL,
  `cancelled_at` datetime(6) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_workspace_tasks_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_workspace_tasks_run` (`agent_run_id`),
  KEY `idx_agent_workspace_tasks_ready_claim` (`status`,`available_at`,`priority` DESC,`id`),
  KEY `idx_agent_workspace_tasks_takeover` (`status`,`lease_expires_at`,`id`),
  KEY `idx_agent_workspace_tasks_revision` (`configuration_revision_id`,`created_at`,`id`),
  CONSTRAINT `fk_agent_workspace_tasks_run` FOREIGN KEY (`agent_run_id`) REFERENCES `agent_runs` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_workspace_tasks_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_workspace_tasks_type` CHECK ((`task_type` = _ascii'workspace.run')),
  CONSTRAINT `chk_agent_workspace_tasks_status` CHECK ((`status` in (_ascii'ready',_ascii'running',_ascii'succeeded',_ascii'dead',_ascii'cancelled'))),
  CONSTRAINT `chk_agent_workspace_tasks_attempts` CHECK ((`max_attempts` between 1 and 4 AND `attempt` <= `max_attempts`)),
  CONSTRAINT `chk_agent_workspace_tasks_lease` CHECK ((
    (`status` = _ascii'running' AND `lease_owner` is not null AND char_length(trim(`lease_owner`)) between 1 and 128 AND `lease_expires_at` is not null) OR
    (`status` <> _ascii'running' AND `lease_owner` is null AND `lease_expires_at` is null)
  )),
  CONSTRAINT `chk_agent_workspace_tasks_terminal` CHECK ((
    (`status` in (_ascii'ready',_ascii'running') AND `completed_at` is null AND `dead_at` is null AND `cancelled_at` is null) OR
    (`status` = _ascii'succeeded' AND `completed_at` is not null AND `dead_at` is null AND `cancelled_at` is null) OR
    (`status` = _ascii'dead' AND `completed_at` is null AND `dead_at` is not null AND `cancelled_at` is null) OR
    (`status` = _ascii'cancelled' AND `completed_at` is null AND `dead_at` is null AND `cancelled_at` is not null)
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `agent_workspace_task_attempts` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `public_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `task_id` bigint unsigned NOT NULL,
  `configuration_revision_id` bigint unsigned NOT NULL,
  `attempt` int unsigned NOT NULL,
  `lease_owner` varchar(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `lease_generation` bigint unsigned NOT NULL,
  `claim_kind` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` varchar(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `started_at` datetime(6) NOT NULL,
  `last_heartbeat_at` datetime(6) DEFAULT NULL,
  `finished_at` datetime(6) DEFAULT NULL,
  `error_code` varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `error_summary` varchar(2048) DEFAULT NULL,
  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_agent_workspace_task_attempts_public_id` (`public_id`),
  UNIQUE KEY `uk_agent_workspace_task_attempts_task_attempt` (`task_id`,`attempt`),
  KEY `idx_agent_workspace_task_attempts_task_created` (`task_id`,`created_at`,`id`),
  KEY `idx_agent_workspace_task_attempts_revision` (`configuration_revision_id`,`created_at`,`id`),
  CONSTRAINT `fk_agent_workspace_task_attempts_task` FOREIGN KEY (`task_id`) REFERENCES `agent_workspace_tasks` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `fk_agent_workspace_task_attempts_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  CONSTRAINT `chk_agent_workspace_task_attempts_identity` CHECK ((
    `attempt` > 0 AND `lease_generation` > 0 AND char_length(trim(`lease_owner`)) between 1 and 128
  )),
  CONSTRAINT `chk_agent_workspace_task_attempts_claim` CHECK ((`claim_kind` in (_ascii'ready',_ascii'takeover'))),
  CONSTRAINT `chk_agent_workspace_task_attempts_status` CHECK ((`status` in (
    _ascii'running',_ascii'succeeded',_ascii'retry',_ascii'dead',_ascii'lease_expired',_ascii'cancelled',_ascii'lease_lost'
  ))),
  CONSTRAINT `chk_agent_workspace_task_attempts_terminal` CHECK ((
    (`status` = _ascii'running' AND `finished_at` is null) OR
    (`status` <> _ascii'running' AND `finished_at` is not null)
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
