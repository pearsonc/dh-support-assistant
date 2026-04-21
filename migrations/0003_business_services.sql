-- +goose Up
-- business_services: one row per distinct ServiceNow "Business service"
-- value. Parser format: "{Cloud|Az} {Client}-{CountryCode} {Product}"
-- (Phase 0 Field Inventory). client_id is nullable because internal
-- services (e.g. "dunnhumby Enterprise Monitoring") have no hyphenated
-- client token and are not scoped to a client. country_code and product
-- are nullable for the same reason: the parser may yield only a platform
-- + raw token count below the full 4-token pattern.
CREATE TABLE business_services (
    id            BIGSERIAL PRIMARY KEY,
    raw_value     TEXT   NOT NULL UNIQUE,
    platform      TEXT   NOT NULL,
    client_id     BIGINT REFERENCES clients(id),
    country_code  TEXT,
    product       TEXT
);
CREATE INDEX idx_business_services_client ON business_services(client_id);

-- +goose Down
DROP TABLE business_services;
