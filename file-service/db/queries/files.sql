-- name: InsertFile :one
INSERT INTO files (id, user_id, filename, content_type, path, root_dir, size, checksum_sha256)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;
