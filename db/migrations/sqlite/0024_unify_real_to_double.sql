-- +goose Up
-- Q5.U-D-01: no-op for sqlite — REAL is already 8-byte IEEE-754 there.
-- The companion postgres migration promotes its REAL columns to
-- DOUBLE PRECISION. Migration numbers stay in lockstep across drivers.
SELECT 1;

-- +goose Down
SELECT 1;
