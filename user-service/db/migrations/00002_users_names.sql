-- +goose Up
ALTER TABLE users
    ADD COLUMN first_name VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN last_name VARCHAR(100) NOT NULL DEFAULT '';

ALTER TABLE users
    ALTER COLUMN first_name DROP DEFAULT,
    ALTER COLUMN last_name DROP DEFAULT;

-- +goose Down
ALTER TABLE users
    DROP COLUMN last_name,
    DROP COLUMN first_name;
