-- +goose Up
-- Q4.U-D-02: SQLite cannot ADD CONSTRAINT on an existing table; the
-- equivalent guard is a CHECK in CREATE TABLE. Adding it inline would
-- require rebuilding three tables under SQLite, which is invasive
-- enough that we defer to schema-rebuild work in the typed-config
-- sweep. Postgres carries the canonical guard; SQLite relies on the
-- application layer (jobs.IsValidStatus etc.) until then.
SELECT 1;

-- +goose Down
SELECT 1;
