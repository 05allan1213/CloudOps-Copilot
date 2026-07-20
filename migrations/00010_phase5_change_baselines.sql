-- +goose Up
-- +goose NO TRANSACTION

-- The exact post-image is an approval-bound delivery input. Version-1 complete
-- rows admitted by 00009 remain valid with a NULL post_image. New writers opt
-- into plan_content_schema_version = 2 and must provide the bounded blob.
ALTER TABLE remediation_plans
    DROP CHECK chk_remediation_plans_v3_complete,
    ADD COLUMN post_image MEDIUMBLOB NULL AFTER bounded_diff,
    ADD CONSTRAINT chk_remediation_plans_v3_complete CHECK (
        (
            plan_content_schema_version IS NULL
            AND incident_version IS NULL
            AND created_by_agent_run_id IS NULL
            AND diagnosis_hash IS NULL
            AND target_base_branch IS NULL
            AND last_known_good_sha IS NULL
            AND base_blob_sha IS NULL
            AND file_mode IS NULL
            AND target_resource_json IS NULL
            AND target_field_ref IS NULL
            AND expected_post_image_hash IS NULL
            AND expected_tree_hash IS NULL
            AND canonical_change_manifest_json IS NULL
            AND bounded_diff IS NULL
            AND post_image IS NULL
            AND policy_version IS NULL
            AND policy_snapshot_json IS NULL
            AND verification_plan_hash IS NULL
            AND evidence_bindings_json IS NULL
            AND evidence_set_hash IS NULL
            AND expires_at IS NULL
        )
        OR
        (
            domain_schema_version = 3
            AND plan_content_schema_version IN (1, 2)
            AND incident_version > 0
            AND created_by_agent_run_id IS NOT NULL
            AND diagnosis_hash IS NOT NULL
            AND CHAR_LENGTH(diagnosis_hash) = 64
            AND operation_type = 'restore_required_env'
            AND target_repository IS NOT NULL
            AND CHAR_LENGTH(target_repository) > 0
            AND target_base_branch IS NOT NULL
            AND CHAR_LENGTH(target_base_branch) > 0
            AND target_base_revision IS NOT NULL
            AND CHAR_LENGTH(target_base_revision) BETWEEN 40 AND 64
            AND last_known_good_sha IS NOT NULL
            AND CHAR_LENGTH(last_known_good_sha) BETWEEN 40 AND 64
            AND base_blob_sha IS NOT NULL
            AND CHAR_LENGTH(base_blob_sha) BETWEEN 40 AND 64
            AND file_mode = '100644'
            AND target_path IS NOT NULL
            AND CHAR_LENGTH(target_path) > 0
            AND target_resource_json IS NOT NULL
            AND JSON_STORAGE_SIZE(target_resource_json) <= 4096
            AND target_field_ref IS NOT NULL
            AND CHAR_LENGTH(target_field_ref) > 0
            AND expected_before_hash IS NOT NULL
            AND CHAR_LENGTH(expected_before_hash) = 64
            AND expected_post_image_hash IS NOT NULL
            AND CHAR_LENGTH(expected_post_image_hash) = 64
            AND expected_tree_hash IS NOT NULL
            AND CHAR_LENGTH(expected_tree_hash) BETWEEN 40 AND 64
            AND canonical_change_manifest_json IS NOT NULL
            AND JSON_STORAGE_SIZE(canonical_change_manifest_json) <= 4096
            AND proposed_patch_hash IS NOT NULL
            AND CHAR_LENGTH(proposed_patch_hash) = 64
            AND bounded_diff IS NOT NULL
            AND OCTET_LENGTH(bounded_diff) BETWEEN 1 AND 65536
            AND (
                (plan_content_schema_version = 1 AND post_image IS NULL)
                OR
                (plan_content_schema_version = 2
                 AND post_image IS NOT NULL
                 AND OCTET_LENGTH(post_image) BETWEEN 1 AND 262144)
            )
            AND policy_version IS NOT NULL
            AND CHAR_LENGTH(policy_version) > 0
            AND policy_snapshot_json IS NOT NULL
            AND JSON_STORAGE_SIZE(policy_snapshot_json) <= 16384
            AND policy_snapshot_hash IS NOT NULL
            AND CHAR_LENGTH(policy_snapshot_hash) = 64
            AND verification_plan_json IS NOT NULL
            AND JSON_STORAGE_SIZE(verification_plan_json) <= 16384
            AND verification_plan_hash IS NOT NULL
            AND CHAR_LENGTH(verification_plan_hash) = 64
            AND evidence_bindings_json IS NOT NULL
            AND JSON_STORAGE_SIZE(evidence_bindings_json) <= 16384
            AND evidence_set_hash IS NOT NULL
            AND CHAR_LENGTH(evidence_set_hash) = 64
            AND canonical_plan_hash IS NOT NULL
            AND CHAR_LENGTH(canonical_plan_hash) = 64
            AND hash_schema_version > 0
            AND expires_at IS NOT NULL
            AND expires_at > created_at
        )
    );

ALTER TABLE agent_runs
    ADD UNIQUE KEY uk_agent_runs_v3_owner (id, incident_id, cycle_no);

-- The V3 evaluator distinguishes an explicit check timeout from a run-level
-- timeout. Extend the legacy status check without rewriting existing rows.
ALTER TABLE verification_checks
    DROP CHECK chk_verification_checks_status,
    ADD CONSTRAINT chk_verification_checks_status CHECK (
        status IN ('pending','running','passed','failed','timed_out','unavailable','invalid','cancelled')
    );

CREATE TABLE deployment_baselines (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    baseline_schema_version SMALLINT UNSIGNED NOT NULL,
    target_identity_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    cluster VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    environment VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    namespace VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workload_kind VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workload_name VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    container_name VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    repository VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    base_branch VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_path VARCHAR(1024) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    image_digest CHAR(71) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    gitops_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    config_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    verification_policy_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    verification_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    verified_at DATETIME(6) NOT NULL,
    superseded_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    active_target_key VARBINARY(32) GENERATED ALWAYS AS (
        CASE WHEN status = 'active' THEN UNHEX(target_identity_hash) ELSE NULL END
    ) STORED,
    PRIMARY KEY (id),
    UNIQUE KEY uk_deployment_baselines_public_id (public_id),
    UNIQUE KEY uk_deployment_baselines_active_target (active_target_key),
    UNIQUE KEY uk_deployment_baselines_revision (target_identity_hash, gitops_revision, config_hash),
    KEY idx_deployment_baselines_target_status (target_identity_hash, status, verified_at, id),
    CONSTRAINT chk_deployment_baselines_identity CHECK (
        domain_schema_version = 3
        AND baseline_schema_version > 0
        AND CHAR_LENGTH(target_identity_hash) = 64
        AND workload_kind = 'Deployment'
        AND CHAR_LENGTH(cluster) > 0
        AND CHAR_LENGTH(environment) > 0
        AND CHAR_LENGTH(namespace) > 0
        AND CHAR_LENGTH(workload_name) > 0
        AND CHAR_LENGTH(container_name) > 0
        AND CHAR_LENGTH(repository) > 0
        AND CHAR_LENGTH(base_branch) > 0
        AND CHAR_LENGTH(target_path) > 0
    ),
    CONSTRAINT chk_deployment_baselines_revisions CHECK (
        CHAR_LENGTH(source_revision) BETWEEN 40 AND 64
        AND CHAR_LENGTH(gitops_revision) BETWEEN 40 AND 64
        AND CHAR_LENGTH(image_digest) = 71
        AND image_digest LIKE 'sha256:%'
        AND CHAR_LENGTH(config_hash) = 64
        AND CHAR_LENGTH(verification_hash) = 64
        AND CHAR_LENGTH(verification_policy_version) > 0
    ),
    CONSTRAINT chk_deployment_baselines_status CHECK (
        status IN ('active','superseded')
        AND row_version > 0
        AND ((status = 'active' AND superseded_at IS NULL)
             OR (status = 'superseded' AND superseded_at IS NOT NULL
                 AND superseded_at >= verified_at))
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE baseline_observations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    observation_schema_version SMALLINT UNSIGNED NOT NULL,
    baseline_id BIGINT UNSIGNED NOT NULL,
    sequence_no INT UNSIGNED NOT NULL,
    observation_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_identity VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    observed_json JSON NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    dedupe_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    observed_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_baseline_observations_public_id (public_id),
    UNIQUE KEY uk_baseline_observations_sequence (baseline_id, sequence_no),
    UNIQUE KEY uk_baseline_observations_dedupe (baseline_id, dedupe_key),
    KEY idx_baseline_observations_time (baseline_id, observed_at, id),
    CONSTRAINT fk_baseline_observations_baseline
        FOREIGN KEY (baseline_id) REFERENCES deployment_baselines (id) ON DELETE RESTRICT,
    CONSTRAINT chk_baseline_observations_identity CHECK (
        domain_schema_version = 3
        AND observation_schema_version > 0
        AND sequence_no > 0
        AND observation_type IN (
            'argocd_revision','kubernetes_readiness','alert_state','metric','log','trace','config_blob'
        )
        AND CHAR_LENGTH(source_identity) > 0
        AND CHAR_LENGTH(content_hash) = 64
        AND CHAR_LENGTH(dedupe_key) = 64
    ),
    CONSTRAINT chk_baseline_observations_payload CHECK (
        JSON_STORAGE_SIZE(observed_json) <= 16384
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE change_candidates (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    candidate_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    agent_run_id BIGINT UNSIGNED NOT NULL,
    change_ref VARCHAR(512) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    repository VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    commit_sha VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    gitops_revision VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    image_digest CHAR(71) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    target_path VARCHAR(1024) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    category VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    change_time DATETIME(6) NOT NULL,
    supporting_evidence_json JSON NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_change_candidates_public_id (public_id),
    UNIQUE KEY uk_change_candidates_run_content (agent_run_id, content_hash),
    UNIQUE KEY uk_change_candidates_v3_owner (id, incident_id, cycle_no),
    KEY idx_change_candidates_incident_cycle (incident_id, cycle_no, change_time, id),
    KEY idx_change_candidates_revision (repository, gitops_revision, id),
    CONSTRAINT fk_change_candidates_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_change_candidates_agent_owner
        FOREIGN KEY (agent_run_id, incident_id, cycle_no)
        REFERENCES agent_runs (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT chk_change_candidates_identity CHECK (
        domain_schema_version = 3
        AND candidate_schema_version > 0
        AND cycle_no > 0
        AND CHAR_LENGTH(change_ref) > 0
        AND source_type IN ('github_commit','github_pull_request','ci','image','argocd')
        AND CHAR_LENGTH(repository) > 0
        AND CHAR_LENGTH(commit_sha) BETWEEN 40 AND 64
        AND CHAR_LENGTH(gitops_revision) BETWEEN 40 AND 64
        AND CHAR_LENGTH(image_digest) = 71
        AND image_digest LIKE 'sha256:%'
        AND CHAR_LENGTH(target_path) > 0
        AND category IN ('confirmed_match','high_confidence','low_confidence','excluded','no_data')
        AND CHAR_LENGTH(content_hash) = 64
    ),
    CONSTRAINT chk_change_candidates_evidence CHECK (
        JSON_TYPE(supporting_evidence_json) = 'ARRAY'
        AND JSON_STORAGE_SIZE(supporting_evidence_json) <= 16384
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE change_candidate_assessments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    assessment_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    candidate_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    supporting_evidence_json JSON NOT NULL,
    contradicting_evidence_json JSON NOT NULL,
    validator_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    policy_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    supersedes_assessment_id BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_change_candidate_assessments_public_id (public_id),
    UNIQUE KEY uk_change_candidate_assessments_content (candidate_id, content_hash),
    UNIQUE KEY uk_change_candidate_assessments_superseder (supersedes_assessment_id),
    UNIQUE KEY uk_change_candidate_assessments_owner (id, candidate_id),
    KEY idx_change_candidate_assessments_latest (candidate_id, created_at, id),
    KEY idx_change_candidate_assessments_incident_cycle (incident_id, cycle_no, created_at, id),
    CONSTRAINT fk_change_candidate_assessments_candidate_owner
        FOREIGN KEY (candidate_id, incident_id, cycle_no)
        REFERENCES change_candidates (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT fk_change_candidate_assessments_supersedes
        FOREIGN KEY (supersedes_assessment_id, candidate_id)
        REFERENCES change_candidate_assessments (id, candidate_id) ON DELETE RESTRICT,
    CONSTRAINT chk_change_candidate_assessments_identity CHECK (
        domain_schema_version = 3
        AND assessment_schema_version > 0
        AND cycle_no > 0
        AND status IN ('matched','excluded','unknown')
        AND CHAR_LENGTH(validator_version) > 0
        AND CHAR_LENGTH(policy_hash) = 64
        AND CHAR_LENGTH(content_hash) = 64
    ),
    CONSTRAINT chk_change_candidate_assessments_evidence CHECK (
        JSON_TYPE(supporting_evidence_json) = 'ARRAY'
        AND JSON_TYPE(contradicting_evidence_json) = 'ARRAY'
        AND JSON_STORAGE_SIZE(supporting_evidence_json) <= 16384
        AND JSON_STORAGE_SIZE(contradicting_evidence_json) <= 16384
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
