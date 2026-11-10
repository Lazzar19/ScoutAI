-- +goose Up
-- No application tables yet. This migration exists so startup always has a version to apply.
SELECT 1;

-- +goose Down
SELECT 1;
