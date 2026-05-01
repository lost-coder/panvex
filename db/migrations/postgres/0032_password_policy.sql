-- +goose Up
-- S-01: configurable minimum password length stored on the panel_settings
-- singleton row. Default 10 mirrors NIST SP 800-63B "memorized secret" floor;
-- existing user passwords are NOT invalidated — policy applies only on
-- create/change.
ALTER TABLE panel_settings
    ADD COLUMN password_min_length INTEGER NOT NULL DEFAULT 10
    CHECK (password_min_length BETWEEN 8 AND 128);

-- +goose Down
ALTER TABLE panel_settings DROP COLUMN password_min_length;
