-- +goose Up
-- +goose NO TRANSACTION

-- Incident Investigations now use the same durable Workspace runtime as Alert
-- Investigations and Consultations. Historical incident AgentRuns retain their
-- original run_kind and provenance.
ALTER TABLE `agent_runs`
  DROP INDEX `uk_agent_runs_active_workspace_subject`,
  DROP CHECK `chk_agent_runs_identity`,
  DROP COLUMN `active_workspace_subject_key`,
  ADD COLUMN `active_workspace_subject_key` varchar(64) CHARACTER SET ascii COLLATE ascii_bin
    GENERATED ALWAYS AS ((case
      when (`run_kind` = _ascii'workspace' and `status` in (_ascii'pending',_ascii'running') and `subject_type` = _ascii'alert') then concat(_ascii'alert:',`alert_id`)
      when (`run_kind` = _ascii'workspace' and `status` in (_ascii'pending',_ascii'running') and `subject_type` = _ascii'incident') then concat(_ascii'incident:',`incident_id`,_ascii':',`cycle_no`)
      when (`run_kind` = _ascii'workspace' and `status` in (_ascii'pending',_ascii'running') and `subject_type` = _ascii'consultation') then concat(_ascii'consultation:',`consultation_id`)
      else NULL end)) STORED,
  ADD UNIQUE KEY `uk_agent_runs_active_workspace_subject` (`active_workspace_subject_key`),
  ADD CONSTRAINT `chk_agent_runs_identity` CHECK ((
    (`subject_type` = _ascii'incident' AND `run_kind` in (_ascii'incident',_ascii'workspace') AND
      `incident_id` is not null AND `alert_id` is null AND `consultation_id` is null AND
      `cycle_no` > 0 AND `expected_incident_version` > 0 AND
      (`run_kind` = _ascii'incident' OR `configuration_revision_id` is not null)) OR
    (`subject_type` = _ascii'alert' AND `run_kind` = _ascii'workspace' AND `incident_id` is null AND
      `alert_id` is not null AND `consultation_id` is null AND `cycle_no` is null AND
      `expected_incident_version` = 0 AND `configuration_revision_id` is not null) OR
    (`subject_type` = _ascii'consultation' AND `run_kind` = _ascii'workspace' AND `incident_id` is null AND
      `alert_id` is null AND `consultation_id` is not null AND `cycle_no` is null AND
      `expected_incident_version` = 0 AND `configuration_revision_id` is not null)
  ));

-- Exactly one explicit recovery decision may own an Incident Cycle. The event
-- remains append-only and is also the public decision identity.
ALTER TABLE `incident_events`
  ADD COLUMN `recovery_decision_cycle_key` varchar(64) CHARACTER SET ascii COLLATE ascii_bin
    GENERATED ALWAYS AS ((case when (`event_type` = _ascii'incident_recovery_decided')
      then concat(`incident_id`,_ascii':',`cycle_no`) else NULL end)) STORED,
  ADD UNIQUE KEY `uk_incident_events_recovery_decision_cycle` (`recovery_decision_cycle_key`),
  ADD UNIQUE KEY `uk_incident_events_owner` (`id`,`incident_id`,`cycle_no`);

-- Operational recovery deliberately does not require GitHub, Argo CD, source,
-- image, or GitOps identity. It is bound instead to the active configuration,
-- operational scope, terminal Investigation, and explicit decision event.
ALTER TABLE `verification_runs`
  DROP INDEX `uk_verification_runs_trigger_attempt`,
  DROP CHECK `chk_verification_runs_complete`,
  DROP CHECK `chk_verification_runs_trigger`,
  DROP COLUMN `trigger_identity`,
  MODIFY COLUMN `target_revision` char(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  ADD COLUMN `configuration_revision_id` bigint unsigned DEFAULT NULL AFTER `trigger_signal_id`,
  ADD COLUMN `operational_scope_id` bigint unsigned DEFAULT NULL AFTER `configuration_revision_id`,
  ADD COLUMN `decision_event_id` bigint unsigned DEFAULT NULL AFTER `operational_scope_id`,
  ADD COLUMN `trigger_identity` varchar(64) CHARACTER SET ascii COLLATE ascii_bin
    GENERATED ALWAYS AS ((case
      when ((`trigger_type` = _ascii'post_delivery') and (`change_request_id` is not null)) then concat(_ascii'delivery:',`change_request_id`)
      when ((`trigger_type` = _ascii'no_change_signal') and (`trigger_signal_id` is not null)) then concat(_ascii'signal:',`trigger_signal_id`)
      when ((`trigger_type` = _ascii'operational_recovery') and (`decision_event_id` is not null)) then concat(_ascii'decision:',`decision_event_id`)
      else NULL end)) STORED,
  ADD UNIQUE KEY `uk_verification_runs_trigger_attempt` (`incident_id`,`cycle_no`,`trigger_identity`,`attempt`),
  ADD KEY `idx_verification_runs_configuration_revision` (`configuration_revision_id`,`created_at`,`id`),
  ADD KEY `idx_verification_runs_operational_scope` (`operational_scope_id`,`created_at`,`id`),
  ADD KEY `idx_verification_runs_decision_owner` (`decision_event_id`,`incident_id`,`cycle_no`),
  ADD CONSTRAINT `fk_verification_runs_configuration_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_verification_runs_operational_scope` FOREIGN KEY (`operational_scope_id`) REFERENCES `operational_scopes` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_verification_runs_decision_owner` FOREIGN KEY (`decision_event_id`,`incident_id`,`cycle_no`) REFERENCES `incident_events` (`id`,`incident_id`,`cycle_no`) ON DELETE RESTRICT,
  ADD CONSTRAINT `chk_verification_runs_complete` CHECK ((
    (`verification_contract_version` is null AND `verification_profile_id` is null AND
      `common_stability_window_ms` is null AND `common_success_since` is null AND
      `common_window_completed_at` is null) OR
    (`verification_contract_version` > 0 AND `verification_profile_version` > 0 AND
      `verification_profile_hash` is not null AND char_length(`verification_profile_hash`) = 64 AND
      `common_stability_window_ms` = 60000 AND `plan_json` is not null AND
      json_storage_size(`plan_json`) <= 16384 AND
      (`common_window_completed_at` is null OR `common_success_since` is not null) AND
      (`common_window_completed_at` is null OR `common_window_completed_at` >= `common_success_since`) AND
      (
        (`trigger_type` = _ascii'post_delivery' AND `verification_profile_id` = _ascii'golden-required-env/v1' AND
          `remediation_plan_id` is not null AND `change_request_id` is not null AND
          `trigger_signal_id` is null AND `decision_event_id` is null AND
          `target_revision` is not null AND char_length(`target_revision`) between 40 and 64 AND
          `source_revision` is not null AND char_length(`source_revision`) between 40 and 64 AND
          `image_digest` is not null AND char_length(`image_digest`) = 71 AND `image_digest` like _ascii'sha256:%' AND
          `gitops_revision` is not null AND char_length(`gitops_revision`) between 40 and 64) OR
        (`trigger_type` = _ascii'no_change_signal' AND `verification_profile_id` = _ascii'no-change/v1' AND
          `remediation_plan_id` is null AND `change_request_id` is null AND
          `trigger_signal_id` is not null AND `decision_event_id` is null AND
          `target_revision` is not null AND char_length(`target_revision`) between 40 and 64 AND
          `source_revision` is not null AND char_length(`source_revision`) between 40 and 64 AND
          `image_digest` is not null AND char_length(`image_digest`) = 71 AND `image_digest` like _ascii'sha256:%' AND
          `gitops_revision` is not null AND char_length(`gitops_revision`) between 40 and 64) OR
        (`trigger_type` = _ascii'operational_recovery' AND `verification_profile_id` = _ascii'operational-recovery/v1' AND
          `remediation_plan_id` is null AND `change_request_id` is null AND `trigger_signal_id` is null AND
          `configuration_revision_id` is not null AND `operational_scope_id` is not null AND
          `originating_agent_run_id` is not null AND `decision_event_id` is not null AND
          `target_revision` is null AND `source_revision` is null AND `image_digest` is null AND `gitops_revision` is null)
      )
    )
  )),
  ADD CONSTRAINT `chk_verification_runs_trigger` CHECK ((
    (`trigger_type` = _ascii'post_delivery' AND `change_request_id` is not null AND `trigger_signal_id` is null AND `decision_event_id` is null) OR
    (`trigger_type` = _ascii'no_change_signal' AND `change_request_id` is null AND `trigger_signal_id` is not null AND `decision_event_id` is null) OR
    (`trigger_type` = _ascii'operational_recovery' AND `change_request_id` is null AND `trigger_signal_id` is null AND `decision_event_id` is not null)
  ));

ALTER TABLE `verification_checks`
  DROP CHECK `chk_verification_checks_complete`,
  ADD CONSTRAINT `chk_verification_checks_complete` CHECK ((
    (`check_spec_schema_version` is null AND `profile_id` is null AND `template_id` is null AND
      `template_version` is null AND `comparison` is null AND `threshold` is null AND
      `source_identity` is null AND `initial_delay_ms` is null AND `min_samples` is null AND
      `sample_unit` is null AND `failure_mode` is null) OR
    (`check_spec_schema_version` > 0 AND `profile_id` in (
        _ascii'golden-required-env/v1',_ascii'no-change/v1',_ascii'operational-recovery/v1') AND
      `template_id` is not null AND char_length(`template_id`) > 0 AND
      `template_version` is not null AND char_length(`template_version`) > 0 AND
      `source_identity` is not null AND char_length(`source_identity`) > 0 AND
      `initial_delay_ms` is not null AND `initial_delay_ms` >= 0 AND
      `min_samples` is not null AND `min_samples` > 0 AND
      `sample_unit` is not null AND char_length(`sample_unit`) > 0 AND
      `failure_mode` in (_ascii'resets',_ascii'immediate') AND
      ((`comparison` is null AND `threshold` is null) OR
       (`comparison` in (_ascii'lt',_ascii'lte',_ascii'gt',_ascii'gte',_ascii'absent') AND `threshold` is not null)) AND
      (`required_check` = false OR `stability_window_ms` = 60000) AND
      `timeout_ms` >= `stability_window_ms` AND `poll_interval_ms` > 0)
  ));

ALTER TABLE `resolution_reports`
  DROP CHECK `chk_resolution_reports_identity`,
  DROP CHECK `chk_resolution_reports_path`,
  MODIFY COLUMN `initial_signal_id` bigint unsigned DEFAULT NULL,
  MODIFY COLUMN `source_revision` varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  MODIFY COLUMN `image_digest` varchar(71) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  MODIFY COLUMN `gitops_revision` varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  ADD COLUMN `configuration_revision_id` bigint unsigned DEFAULT NULL AFTER `verification_run_id`,
  ADD COLUMN `operational_scope_id` bigint unsigned DEFAULT NULL AFTER `configuration_revision_id`,
  ADD COLUMN `investigation_run_id` bigint unsigned DEFAULT NULL AFTER `operational_scope_id`,
  ADD COLUMN `decision_event_id` bigint unsigned DEFAULT NULL AFTER `investigation_run_id`,
  ADD KEY `idx_resolution_reports_configuration_revision` (`configuration_revision_id`,`generated_at`,`id`),
  ADD KEY `idx_resolution_reports_operational_scope` (`operational_scope_id`,`generated_at`,`id`),
  ADD KEY `idx_resolution_reports_investigation_owner` (`investigation_run_id`,`incident_id`,`cycle_no`),
  ADD KEY `idx_resolution_reports_decision_owner` (`decision_event_id`,`incident_id`,`cycle_no`),
  ADD CONSTRAINT `fk_resolution_reports_configuration_revision` FOREIGN KEY (`configuration_revision_id`) REFERENCES `configuration_revisions` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_resolution_reports_operational_scope` FOREIGN KEY (`operational_scope_id`) REFERENCES `operational_scopes` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_resolution_reports_investigation_owner` FOREIGN KEY (`investigation_run_id`,`incident_id`,`cycle_no`) REFERENCES `agent_runs` (`id`,`incident_id`,`cycle_no`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_resolution_reports_decision_owner` FOREIGN KEY (`decision_event_id`,`incident_id`,`cycle_no`) REFERENCES `incident_events` (`id`,`incident_id`,`cycle_no`) ON DELETE RESTRICT,
  ADD CONSTRAINT `chk_resolution_reports_identity` CHECK ((
    `report_schema_version` > 0 AND `cycle_no` > 0 AND `resolved_at` >= `cycle_started_at` AND
    `generated_at` >= `resolved_at` AND `common_window_completed_at` >= `common_window_started_at` AND
    timestampdiff(MICROSECOND,`common_window_started_at`,`common_window_completed_at`) >= 60000000 AND
    `verification_profile_id` in (_ascii'golden-required-env/v1',_ascii'no-change/v1',_ascii'operational-recovery/v1') AND
    char_length(`verification_profile_hash`) = 64 AND char_length(`content_hash`) = 64 AND
    ((`trigger_type` = _ascii'operational_recovery' AND `source_revision` is null AND
       `image_digest` is null AND `gitops_revision` is null) OR
     (`trigger_type` <> _ascii'operational_recovery' AND char_length(`source_revision`) between 40 and 64 AND
       char_length(`image_digest`) = 71 AND `image_digest` like _ascii'sha256:%' AND
       char_length(`gitops_revision`) between 40 and 64))
  )),
  ADD CONSTRAINT `chk_resolution_reports_path` CHECK ((
    (`trigger_type` = _ascii'post_delivery' AND
      `resolution_reason` in (_ascii'recovered_after_change',_ascii'recovered_after_remediation') AND
      `trigger_signal_id` is null AND `remediation_plan_id` is not null AND
      `remediation_decision_id` is not null AND `change_request_id` is not null AND
      `diagnosis_json` is not null AND `remediation_plan_json` is not null AND
      `remediation_decision_json` is not null AND `delivery_json` is not null AND
      `bad_gitops_revision` is not null AND char_length(`bad_gitops_revision`) between 40 and 64 AND
      `fix_gitops_revision` is not null AND char_length(`fix_gitops_revision`) between 40 and 64 AND
      `verification_profile_id` = _ascii'golden-required-env/v1' AND `decision_event_id` is null) OR
    (`trigger_type` = _ascii'no_change_signal' AND
      `resolution_reason` in (_ascii'recovered_before_diagnosis',_ascii'recovered_without_change') AND
      `trigger_signal_id` is not null AND `remediation_plan_id` is null AND
      `remediation_decision_id` is null AND `change_request_id` is null AND
      `remediation_plan_json` is null AND `remediation_decision_json` is null AND
      `delivery_json` is null AND `bad_gitops_revision` is null AND `fix_gitops_revision` is null AND
      `verification_profile_id` = _ascii'no-change/v1' AND `decision_event_id` is null) OR
    (`trigger_type` = _ascii'operational_recovery' AND
      `resolution_reason` = _ascii'recovered_without_change' AND `initial_signal_id` is null AND
      `trigger_signal_id` is null AND `remediation_plan_id` is null AND
      `remediation_decision_id` is null AND `change_request_id` is null AND
      `configuration_revision_id` is not null AND `operational_scope_id` is not null AND
      `investigation_run_id` is not null AND `decision_event_id` is not null AND
      `diagnosis_json` is not null AND `remediation_plan_json` is null AND
      `remediation_decision_json` is not null AND `delivery_json` is null AND
      `bad_gitops_revision` is null AND `fix_gitops_revision` is null AND
      `verification_profile_id` = _ascii'operational-recovery/v1')
  ));

-- The always-on recovery runner is isolated from the optional Provider-owned
-- five-task registry by exact task type filtering on the shared verify queue.
ALTER TABLE `async_tasks`
  DROP CHECK `chk_async_tasks_queue_type`,
  DROP CHECK `chk_async_tasks_subject_transition`,
  DROP CHECK `chk_async_tasks_type`,
  ADD CONSTRAINT `chk_async_tasks_queue_type` CHECK ((
    (`queue` = _ascii'investigate' AND `task_type` in (_ascii'investigation.advance',_ascii'remediation.prepare')) OR
    (`queue` = _ascii'deliver' AND `task_type` = _ascii'change.ensure_pr') OR
    (`queue` = _ascii'observe' AND `task_type` = _ascii'delivery.observe') OR
    (`queue` = _ascii'verify' AND `task_type` in (_ascii'verification.advance',_ascii'recovery.verify'))
  )),
  ADD CONSTRAINT `chk_async_tasks_subject_transition` CHECK ((
    (`task_type` = _ascii'investigation.advance' AND
      ((`subject_type` = _ascii'incident' AND `transition` = _ascii'investigation.start') OR
       (`subject_type` = _ascii'agent_run' AND `transition` = _ascii'investigation.step'))) OR
    (`task_type` = _ascii'remediation.prepare' AND `subject_type` = _ascii'agent_run' AND `transition` = _ascii'remediation.prepare') OR
    (`task_type` = _ascii'change.ensure_pr' AND `subject_type` in (_ascii'remediation_plan',_ascii'change_request') AND `transition` = _ascii'change.ensure_pr') OR
    (`task_type` = _ascii'delivery.observe' AND `subject_type` = _ascii'change_request' AND `transition` = _ascii'delivery.observe') OR
    (`task_type` = _ascii'verification.advance' AND `subject_type` = _ascii'verification_run' AND `transition` = _ascii'verification.advance') OR
    (`task_type` = _ascii'recovery.verify' AND `subject_type` = _ascii'verification_run' AND `transition` = _ascii'recovery.verify')
  )),
  ADD CONSTRAINT `chk_async_tasks_type` CHECK ((`task_type` in (
    _ascii'investigation.advance',_ascii'remediation.prepare',_ascii'change.ensure_pr',
    _ascii'delivery.observe',_ascii'verification.advance',_ascii'recovery.verify')));
