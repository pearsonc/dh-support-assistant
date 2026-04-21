-- +goose Up
-- imports: one row per ingested file. import_hash is a SHA-256 over the
-- NORMALISED file content (trimmed lines, LF newlines, column order fixed)
-- per [Rule: Ingest idempotency]. Re-ingesting a file that produces the
-- same normalised hash short-circuits the pipeline.
CREATE TABLE imports (
    id           BIGSERIAL PRIMARY KEY,
    import_hash  TEXT        NOT NULL UNIQUE,
    file_name    TEXT        NOT NULL,
    row_count    INTEGER     NOT NULL,
    ingested_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE imports;
