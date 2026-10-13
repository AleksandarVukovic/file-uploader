-- name: InsertFile :one
INSERT INTO files (id, user_id, filename, content_type, path, root_dir, size, checksum_sha256)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateFileStatus :one
UPDATE files
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteFile :exec
DELETE FROM files
WHERE id = $1;

-- name: GetPendingFilesOlderThan :many
SELECT * FROM files
WHERE status = 'pending' AND created_at > $1 AND created_at < $2
ORDER BY created_at
LIMIT 100;
