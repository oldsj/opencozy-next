-- Every query takes the owner, so one user can never read or change another's notes.

-- name: ListNotes :many
SELECT * FROM notes
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT 500;

-- name: CreateNote :one
INSERT INTO notes (user_id, title, body)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateNote :one
UPDATE notes
SET title = $3, body = $4, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteNote :execrows
DELETE FROM notes WHERE id = $1 AND user_id = $2;
