-- +goose Up
-- Q4.U-D-03: no-op for sqlite — telemt_instances already carries
-- ON DELETE CASCADE via 0012_cascade_fk.sql. Migration files are kept
-- in lockstep across drivers so the postgres-side change shares the
-- same number.
SELECT 1;

-- +goose Down
SELECT 1;
