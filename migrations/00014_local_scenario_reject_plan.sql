-- +goose Up
-- +goose NO TRANSACTION

ALTER TABLE `remediation_plans`
  DROP CHECK `chk_remediation_plans_complete`,
  ADD COLUMN `source_type` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'gitops' AFTER `operation_type`,
  ADD COLUMN `runtime_base_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL AFTER `target_base_revision`,
  ADD CONSTRAINT `chk_remediation_plans_complete` CHECK ((
    ((`plan_content_schema_version` is null) and (`incident_version` is null) and (`created_by_agent_run_id` is null) and
      (`diagnosis_hash` is null) and (`target_base_branch` is null) and (`last_known_good_sha` is null) and
      (`base_blob_sha` is null) and (`file_mode` is null) and (`target_resource_json` is null) and
      (`target_field_ref` is null) and (`expected_post_image_hash` is null) and (`expected_tree_hash` is null) and
      (`canonical_change_manifest_json` is null) and (`bounded_diff` is null) and (`post_image` is null) and
      (`policy_version` is null) and (`policy_snapshot_json` is null) and (`verification_plan_hash` is null) and
      (`evidence_bindings_json` is null) and (`evidence_set_hash` is null) and (`expires_at` is null))
    OR
    ((`plan_content_schema_version` in (1,2)) and (`source_type` = _ascii'gitops') and (`incident_version` > 0) and
      (`created_by_agent_run_id` is not null) and (char_length(`diagnosis_hash`) = 64) and
      (`operation_type` = _utf8mb4'restore_required_env') and (char_length(`target_repository`) > 0) and
      (char_length(`target_base_branch`) > 0) and (char_length(`target_base_revision`) between 40 and 64) and
      (char_length(`last_known_good_sha`) between 40 and 64) and (char_length(`base_blob_sha`) between 40 and 64) and
      (`file_mode` = _ascii'100644') and (`runtime_base_hash` is null) and (char_length(`target_path`) > 0) and
      (json_storage_size(`target_resource_json`) <= 4096) and (char_length(`target_field_ref`) > 0) and
      (char_length(`expected_before_hash`) = 64) and (char_length(`expected_post_image_hash`) = 64) and
      (char_length(`expected_tree_hash`) between 40 and 64) and (json_storage_size(`canonical_change_manifest_json`) <= 4096) and
      (char_length(`proposed_patch_hash`) = 64) and (length(`bounded_diff`) between 1 and 65536) and
      (((`plan_content_schema_version` = 1) and (`post_image` is null)) or
       ((`plan_content_schema_version` = 2) and (length(`post_image`) between 1 and 262144))) and
      (char_length(`policy_version`) > 0) and (json_storage_size(`policy_snapshot_json`) <= 16384) and
      (char_length(`policy_snapshot_hash`) = 64) and (json_storage_size(`verification_plan_json`) <= 16384) and
      (char_length(`verification_plan_hash`) = 64) and (json_storage_size(`evidence_bindings_json`) <= 16384) and
      (char_length(`evidence_set_hash`) = 64) and (char_length(`canonical_plan_hash`) = 64) and
      (`hash_schema_version` > 0) and (`expires_at` > `created_at`))
    OR
    ((`plan_content_schema_version` = 3) and (`source_type` = _ascii'local_scenario') and (`incident_version` > 0) and
      (`created_by_agent_run_id` is not null) and (char_length(`diagnosis_hash`) = 64) and
      (`operation_type` = _utf8mb4'restore_required_env') and (`target_repository` = _utf8mb4'') and
      (`target_base_branch` is null) and (`target_base_revision` = _utf8mb4'') and
      (`last_known_good_sha` is null) and (`base_blob_sha` is null) and (`file_mode` is null) and
      (char_length(`runtime_base_hash`) = 64) and (char_length(`target_path`) > 0) and
      (json_storage_size(`target_resource_json`) <= 4096) and (char_length(`target_field_ref`) > 0) and
      (char_length(`expected_before_hash`) = 64) and (char_length(`expected_post_image_hash`) = 64) and
      (`expected_tree_hash` = _ascii'') and (json_storage_size(`canonical_change_manifest_json`) <= 4096) and
      (char_length(`proposed_patch_hash`) = 64) and (length(`bounded_diff`) between 1 and 65536) and
      (length(`post_image`) between 1 and 262144) and (char_length(`policy_version`) > 0) and
      (json_storage_size(`policy_snapshot_json`) <= 16384) and (char_length(`policy_snapshot_hash`) = 64) and
      (json_storage_size(`verification_plan_json`) <= 16384) and (char_length(`verification_plan_hash`) = 64) and
      (json_storage_size(`evidence_bindings_json`) <= 16384) and (char_length(`evidence_set_hash`) = 64) and
      (char_length(`canonical_plan_hash`) = 64) and (`hash_schema_version` > 0) and (`expires_at` > `created_at`))
  )),
  ADD CONSTRAINT `chk_remediation_plans_source_type` CHECK ((`source_type` in (_ascii'gitops',_ascii'local_scenario')));

ALTER TABLE `remediation_decisions`
  DROP CHECK `chk_remediation_decisions_hashes`,
  ADD COLUMN `plan_source_type` varchar(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'gitops' AFTER `decision_schema_version`,
  MODIFY COLUMN `approved_base_sha` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  MODIFY COLUMN `approved_tree_hash` varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  ADD CONSTRAINT `chk_remediation_decisions_hashes` CHECK ((
    (`approved_hash_schema_version` > 0) and (char_length(`approved_plan_hash`) = 64) and
    (char_length(`approved_post_image_hash`) = 64) and (char_length(`approved_patch_hash`) = 64) and
    (char_length(`approved_policy_hash`) = 64) and (char_length(`approved_verification_hash`) = 64) and
    (char_length(`approved_evidence_set_hash`) = 64) and
    (((`plan_source_type` = _ascii'gitops') and (char_length(`approved_base_sha`) between 40 and 64) and
      (char_length(`approved_tree_hash`) between 40 and 64)) or
     ((`plan_source_type` = _ascii'local_scenario') and (`decision_schema_version` = 2) and
      (`decision` = _ascii'rejected') and (`approved_base_sha` = _ascii'') and (`approved_tree_hash` = _ascii'')))
  )),
  ADD CONSTRAINT `chk_remediation_decisions_source_type` CHECK ((`plan_source_type` in (_ascii'gitops',_ascii'local_scenario')));
