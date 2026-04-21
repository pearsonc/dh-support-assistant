-- +goose Up
-- clients: distinct client entities derived from Business service parsing
-- (see Phase 0 Field Inventory — Company is 17% populated and UNRELIABLE;
-- business_service is the client dimension, 100% populated). first_seen_at
-- records when the ingest first saw this client, independent of ServiceNow
-- timestamps on any individual ticket.
CREATE TABLE clients (
    id             BIGSERIAL PRIMARY KEY,
    client_name    TEXT        NOT NULL UNIQUE,
    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE clients;
