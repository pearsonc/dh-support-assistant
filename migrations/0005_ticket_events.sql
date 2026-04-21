-- +goose Up
-- ticket_events: one row per parsed journal entry from the merged
-- "Comments and Work notes" column (Phase 0 Comment History Approach A).
-- UNIQUE(ticket_id, event_ts, author_name) is the idempotency key: the
-- same entry re-observed from a later export is a no-op. ON DELETE
-- CASCADE keeps event history consistent if a ticket is ever hard-deleted
-- (not a normal path — tickets are immutable once ingested — but this
-- protects against stray manual deletions during schema iteration).
--
-- author_context carries the parenthesised suffix from the header regex
-- (^timestamp - AuthorName (AuthorContext)$) — typically a role like
-- "Service Desk" or a system tag. Type distinction (Comment vs Work note)
-- is NOT recoverable from the merged column (Phase 0 finding); every
-- entry is stored as an untyped journal row. Phase 6 will add type
-- provenance via Approach B (separate sys_journal_field export).
CREATE TABLE ticket_events (
    id                BIGSERIAL PRIMARY KEY,
    ticket_id         BIGINT      NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    event_ts          TIMESTAMPTZ NOT NULL,
    author_name       TEXT        NOT NULL,
    author_context    TEXT        NOT NULL,
    body              TEXT        NOT NULL,
    source_import_id  BIGINT      NOT NULL REFERENCES imports(id),
    UNIQUE (ticket_id, event_ts, author_name)
);
CREATE INDEX idx_ticket_events_ticket ON ticket_events(ticket_id);
CREATE INDEX idx_ticket_events_ts     ON ticket_events(event_ts);

-- +goose Down
DROP TABLE ticket_events;
