-- name: ListChildren :many
SELECT * FROM child
WHERE household_id = $1 AND (sqlc.arg(include_archived)::boolean OR archived_at IS NULL)
ORDER BY created_at
LIMIT 50;

-- name: GetChild :one
SELECT * FROM child WHERE household_id = $1 AND id = $2;

-- name: CountChildren :one
SELECT count(*) FROM child WHERE household_id = $1 AND archived_at IS NULL;

-- name: CreateChild :one
INSERT INTO child (household_id, name, color) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateChild :one
UPDATE child
SET name = $4,
    archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN coalesce(archived_at, sqlc.arg(now)::timestamptz) ELSE NULL END,
    version = version + 1
WHERE household_id = $1 AND id = $2 AND version = $3
RETURNING *;

-- name: LockLiveChild :one
SELECT id FROM child WHERE household_id = $1 AND id = $2 AND archived_at IS NULL FOR UPDATE;

-- name: LockAnyChild :one
SELECT id FROM child WHERE household_id = $1 AND id = $2 FOR UPDATE;
