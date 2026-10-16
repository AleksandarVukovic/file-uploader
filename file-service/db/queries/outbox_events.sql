-- name: InsertEvent :one
INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUnpublishedEvents :many
SELECT * FROM outbox_events
WHERE published_at IS NULL AND aggregate_type = $1
ORDER BY created_at
LIMIT $2
FOR UPDATE SKIP LOCKED;

-- name: MarkEventPublished :exec
UPDATE outbox_events
SET published_at = now()
WHERE id = $1 AND published_at IS NULL;