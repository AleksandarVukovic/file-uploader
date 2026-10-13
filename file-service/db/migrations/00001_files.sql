-- +goose Up
CREATE TYPE file_status AS ENUM ('pending', 'uploaded', 'failed');

CREATE TABLE files (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL,
    filename VARCHAR(255) NOT NULL CHECK (char_length(filename) >= 5),
    content_type VARCHAR(100) NOT NULL,
    path VARCHAR(100) NOT NULL,
    root_dir VARCHAR(63) NOT NULL,
    size BIGINT NOT NULL CHECK (size BETWEEN 100 AND 10485760), -- 10Mb
    checksum_sha256 BYTEA NOT NULL CHECK (octet_length(checksum_sha256) = 32), -- raw SHA-256 digest
    status file_status NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT files_user_id_checksum_sha256_key UNIQUE (user_id, checksum_sha256)
);

CREATE INDEX files_user_id_created_at_idx ON files (user_id, created_at DESC);

-- +goose Down
DROP TABLE files;
DROP TYPE file_status;
