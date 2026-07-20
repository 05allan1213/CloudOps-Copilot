-- +goose Up
-- +goose NO TRANSACTION

-- Slots four and five are explicit operator Decisions. The authorization is
-- append-only; consumption is represented by the one-to-one lineage foreign
-- keys on the records created from the authorized investigation.
CREATE TABLE incident_cycle_budget_authorizations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    authorization_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    budget_kind VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    slot_no TINYINT UNSIGNED NOT NULL,
    actor_provider VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_login VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
    actor_role VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(1024) NOT NULL,
    request_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_authenticated_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_cycle_budget_authorizations_public_id (public_id),
    UNIQUE KEY uk_cycle_budget_authorizations_slot (incident_id, cycle_no, budget_kind, slot_no),
    UNIQUE KEY uk_cycle_budget_authorizations_request (incident_id, cycle_no, request_id),
    UNIQUE KEY uk_cycle_budget_authorizations_owner (id, incident_id, cycle_no),
    KEY idx_cycle_budget_authorizations_cycle (incident_id, cycle_no, created_at, id),
    CONSTRAINT fk_cycle_budget_authorizations_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT chk_cycle_budget_authorizations_identity CHECK (
        domain_schema_version = 3
        AND authorization_schema_version > 0
        AND cycle_no > 0
        AND budget_kind = 'agent_run'
        AND slot_no IN (4, 5)
        AND actor_provider = 'github'
        AND actor_role = 'operator'
        AND CHAR_LENGTH(TRIM(actor_login)) BETWEEN 1 AND 128
        AND CHAR_LENGTH(TRIM(reason)) BETWEEN 1 AND 1024
        AND CHAR_LENGTH(TRIM(request_id)) BETWEEN 1 AND 128
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE agent_runs
    ADD COLUMN business_budget_authorization_id BIGINT UNSIGNED NULL AFTER expected_incident_version,
    ADD UNIQUE KEY uk_agent_runs_v3_budget_authorization (business_budget_authorization_id),
    ADD KEY idx_agent_runs_v3_budget_owner (business_budget_authorization_id, incident_id, cycle_no),
    ADD CONSTRAINT fk_agent_runs_v3_budget_authorization
        FOREIGN KEY (business_budget_authorization_id, incident_id, cycle_no)
        REFERENCES incident_cycle_budget_authorizations (id, incident_id, cycle_no) ON DELETE RESTRICT;

ALTER TABLE remediation_plans
    ADD COLUMN business_budget_authorization_id BIGINT UNSIGNED NULL AFTER created_by_agent_run_id,
    ADD UNIQUE KEY uk_remediation_plans_v3_budget_authorization (business_budget_authorization_id),
    ADD KEY idx_remediation_plans_v3_budget_owner (business_budget_authorization_id, incident_id, cycle_no),
    ADD CONSTRAINT fk_remediation_plans_v3_budget_authorization
        FOREIGN KEY (business_budget_authorization_id, incident_id, cycle_no)
        REFERENCES incident_cycle_budget_authorizations (id, incident_id, cycle_no) ON DELETE RESTRICT;

ALTER TABLE verification_runs
    ADD COLUMN originating_agent_run_id BIGINT UNSIGNED NULL AFTER cycle_no,
    ADD COLUMN business_budget_authorization_id BIGINT UNSIGNED NULL AFTER originating_agent_run_id,
    ADD UNIQUE KEY uk_verification_runs_v3_budget_authorization (business_budget_authorization_id),
    ADD KEY idx_verification_runs_v3_budget_owner (business_budget_authorization_id, incident_id, cycle_no),
    ADD KEY idx_verification_runs_v3_originating_run (originating_agent_run_id, incident_id, cycle_no),
    ADD CONSTRAINT fk_verification_runs_v3_originating_run
        FOREIGN KEY (originating_agent_run_id, incident_id, cycle_no)
        REFERENCES agent_runs (id, incident_id, cycle_no) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_verification_runs_v3_budget_authorization
        FOREIGN KEY (business_budget_authorization_id, incident_id, cycle_no)
        REFERENCES incident_cycle_budget_authorizations (id, incident_id, cycle_no) ON DELETE RESTRICT;

-- +goose Down
-- +goose NO TRANSACTION

ALTER TABLE verification_runs
    DROP FOREIGN KEY fk_verification_runs_v3_budget_authorization,
    DROP FOREIGN KEY fk_verification_runs_v3_originating_run,
    DROP INDEX idx_verification_runs_v3_originating_run,
    DROP INDEX idx_verification_runs_v3_budget_owner,
    DROP INDEX uk_verification_runs_v3_budget_authorization,
    DROP COLUMN business_budget_authorization_id,
    DROP COLUMN originating_agent_run_id;

ALTER TABLE remediation_plans
    DROP FOREIGN KEY fk_remediation_plans_v3_budget_authorization,
    DROP INDEX idx_remediation_plans_v3_budget_owner,
    DROP INDEX uk_remediation_plans_v3_budget_authorization,
    DROP COLUMN business_budget_authorization_id;

ALTER TABLE agent_runs
    DROP FOREIGN KEY fk_agent_runs_v3_budget_authorization,
    DROP INDEX idx_agent_runs_v3_budget_owner,
    DROP INDEX uk_agent_runs_v3_budget_authorization,
    DROP COLUMN business_budget_authorization_id;

DROP TABLE IF EXISTS incident_cycle_budget_authorizations;
