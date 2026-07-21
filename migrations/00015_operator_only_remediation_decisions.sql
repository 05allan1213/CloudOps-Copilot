-- +goose Up
-- +goose NO TRANSACTION

-- V3 has only viewer and operator product roles. The earlier expand migration
-- temporarily accepted the legacy local-admin role so compatibility rows could
-- coexist. Active V3 Decision construction already rejects that role; tighten
-- the database boundary as well. MySQL validates existing rows while adding
-- the replacement CHECK, so any unconverted admin Decision fails migration
-- closed and must be handled by the Phase 7A legacy archive/converter.
ALTER TABLE remediation_decisions
    DROP CHECK chk_remediation_decisions_identity,
    ADD CONSTRAINT chk_remediation_decisions_identity CHECK (
        domain_schema_version = 3
        AND decision_schema_version > 0
        AND cycle_no > 0
        AND plan_version > 0
        AND decision IN ('approved', 'rejected')
        AND actor_provider = 'github'
        AND CHAR_LENGTH(actor_login) > 0
        AND actor_role = 'operator'
        AND CHAR_LENGTH(request_id) > 0
        AND expires_at > request_authenticated_at
    );
