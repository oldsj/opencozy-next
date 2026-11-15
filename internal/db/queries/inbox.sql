-- name: ListPendingApprovals :many
SELECT a.id, a.kind, a.child_id, c.name AS child_name, c.color AS child_color, a.title, a.points, a.created_at
FROM approval a JOIN child c ON c.household_id = a.household_id AND c.id = a.child_id
WHERE a.household_id = $1 AND a.state = 'pending'
ORDER BY a.created_at, a.id
LIMIT 200;

-- name: GetApproval :one
SELECT * FROM approval WHERE household_id = $1 AND id = $2;

-- name: LockApproval :one
SELECT * FROM approval WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- name: DecideApproval :one
UPDATE approval
SET state = $3, reason = sqlc.narg(reason), decided_at = @now, decided_by = $4, version = version + 1
WHERE household_id = $1 AND id = $2 AND state = 'pending'
RETURNING *;
