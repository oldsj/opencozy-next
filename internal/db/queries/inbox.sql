-- name: ListPendingApprovals :many
-- Oldest first. A request for more time is as old as its latest ask: asking again moves it.
SELECT a.id, a.kind, a.child_id, c.name AS child_name, c.color AS child_color, a.title, a.points,
       COALESCE(tr.asked_at, a.created_at)::timestamptz AS created_at
FROM approval a JOIN child c ON c.household_id = a.household_id AND c.id = a.child_id
LEFT JOIN time_request tr ON tr.household_id = a.household_id AND tr.id = a.time_request_id
WHERE a.household_id = $1 AND a.state = 'pending' AND c.archived_at IS NULL
ORDER BY COALESCE(tr.asked_at, a.created_at), a.id
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
