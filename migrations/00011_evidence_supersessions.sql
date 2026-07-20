-- +goose Up
-- +goose NO TRANSACTION

-- Evidence facts remain append-only. Corrections are represented by a new
-- Evidence row plus this immutable relation; the superseded row is never
-- rewritten. Both owner FKs pin the relation to one Incident cycle.
ALTER TABLE evidence_items
    ADD UNIQUE KEY uk_evidence_items_v3_owner (id, incident_id, cycle_no);

CREATE TABLE evidence_supersessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    domain_schema_version SMALLINT UNSIGNED NOT NULL,
    relation_schema_version SMALLINT UNSIGNED NOT NULL,
    incident_id BIGINT UNSIGNED NOT NULL,
    cycle_no BIGINT UNSIGNED NOT NULL,
    superseded_evidence_id BIGINT UNSIGNED NOT NULL,
    superseding_evidence_id BIGINT UNSIGNED NOT NULL,
    reason_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_evidence_supersessions_public_id (public_id),
    UNIQUE KEY uk_evidence_supersessions_superseded (
        superseded_evidence_id, incident_id, cycle_no
    ),
    UNIQUE KEY uk_evidence_supersessions_superseding (
        superseding_evidence_id, incident_id, cycle_no
    ),
    KEY idx_evidence_supersessions_cycle (
        incident_id, cycle_no, created_at, id
    ),
    CONSTRAINT fk_evidence_supersessions_incident
        FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE RESTRICT,
    CONSTRAINT fk_evidence_supersessions_superseded_owner
        FOREIGN KEY (superseded_evidence_id, incident_id, cycle_no)
        REFERENCES evidence_items (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT fk_evidence_supersessions_superseding_owner
        FOREIGN KEY (superseding_evidence_id, incident_id, cycle_no)
        REFERENCES evidence_items (id, incident_id, cycle_no) ON DELETE RESTRICT,
    CONSTRAINT chk_evidence_supersessions_identity CHECK (
        domain_schema_version = 3
        AND relation_schema_version > 0
        AND cycle_no > 0
        AND superseding_evidence_id > superseded_evidence_id
        AND CHAR_LENGTH(reason_code) > 0
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
