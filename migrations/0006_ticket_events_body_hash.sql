-- +goose Up
-- Widen the ticket_events idempotency key to include a hash of the body.
--
-- The 0005 constraint UNIQUE(ticket_id, event_ts, author_name) proved too
-- tight against real ServiceNow journal exports: the Phase 0 sample
-- produced 232 pairs with identical (ticket, second-resolution timestamp,
-- author) but genuinely distinct body text — workflow automation emits
-- multiple entries per second during state transitions and notification
-- fan-out. Collapsing those into one row is 8.3% silent data loss and
-- would poison downstream scoring / relationship analysis.
--
-- Adding body_hash as a fourth component of the UNIQUE key preserves both
-- the idempotency guarantee (same body re-observed still collapses) and
-- the true-burst history (distinct bodies at the same second survive).
-- md5 is cryptographically uninteresting but fine for collision-resistance
-- against accidental duplicates at 32 chars / 128 bits; the ingest never
-- exposes body_hash externally.
--
-- The ADD COLUMN default + UPDATE + DROP DEFAULT pattern keeps this
-- migration safe against any backfill state (currently zero rows — the
-- 0005 writer failures meant the table was effectively a green-field).
ALTER TABLE ticket_events ADD COLUMN body_hash TEXT;
UPDATE ticket_events SET body_hash = md5(body);
ALTER TABLE ticket_events ALTER COLUMN body_hash SET NOT NULL;

ALTER TABLE ticket_events DROP CONSTRAINT ticket_events_ticket_id_event_ts_author_name_key;
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_idem_key UNIQUE (ticket_id, event_ts, author_name, body_hash);

-- +goose Down
ALTER TABLE ticket_events DROP CONSTRAINT ticket_events_idem_key;
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_ticket_id_event_ts_author_name_key UNIQUE (ticket_id, event_ts, author_name);
ALTER TABLE ticket_events DROP COLUMN body_hash;
