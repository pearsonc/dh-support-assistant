-- +goose Up
-- tickets: one row per ServiceNow incident. UNIQUE(ticket_external_id)
-- enforces [Rule: Ingest idempotency] at the row level; re-ingesting the
-- same Number skips insertion (sub-phase 1.4 writer). Column set sourced
-- from the Phase 0 Field Inventory; nullability tracks observed population
-- rates: assigned_to (79%), company_raw (17% — AUDIT ONLY, never used for
-- client scoping), category/subcategory (81%). Jira columns are reserved
-- as nullable text; population grows as DH adopts Jira linkage.
--
-- All timestamps are TIMESTAMPTZ. ServiceNow exports "YYYY-MM-DD HH:MM:SS"
-- strings without zone; ingest (sub-phase 1.4) will parse as UTC and
-- reconsider if timezone drift is observed on re-export comparison.
CREATE TABLE tickets (
    id                   BIGSERIAL PRIMARY KEY,
    ticket_external_id   TEXT        NOT NULL UNIQUE,
    short_description    TEXT        NOT NULL,
    description          TEXT        NOT NULL,
    state                TEXT        NOT NULL,
    priority             TEXT        NOT NULL,
    severity             TEXT        NOT NULL,
    urgency              TEXT        NOT NULL,
    impact               TEXT        NOT NULL,
    assigned_to          TEXT,
    assignment_group     TEXT        NOT NULL,
    opened_at            TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL,
    opened_by            TEXT        NOT NULL,
    updated_by           TEXT        NOT NULL,
    caller               TEXT        NOT NULL,
    company_raw          TEXT,
    business_service_id  BIGINT      NOT NULL REFERENCES business_services(id),
    created_at           TIMESTAMPTZ NOT NULL,
    created_by           TEXT        NOT NULL,
    category             TEXT,
    subcategory          TEXT,
    due_date             TIMESTAMPTZ NOT NULL,
    jira_id              TEXT,
    jira_key             TEXT,
    jira_url             TEXT,
    jira_project         TEXT,
    jira_status          TEXT,
    source_import_id     BIGINT      NOT NULL REFERENCES imports(id)
);
CREATE INDEX idx_tickets_business_service ON tickets(business_service_id);
CREATE INDEX idx_tickets_source_import    ON tickets(source_import_id);
CREATE INDEX idx_tickets_state            ON tickets(state);
CREATE INDEX idx_tickets_updated_at       ON tickets(updated_at);
CREATE INDEX idx_tickets_opened_at        ON tickets(opened_at);

-- +goose Down
DROP TABLE tickets;
