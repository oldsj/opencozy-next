-- name: CreateChore :one
INSERT INTO chore (household_id, child_id, title, points, days) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListChores :many
SELECT * FROM chore WHERE household_id = $1 AND archived_at IS NULL ORDER BY created_at LIMIT 500;

-- name: GetLiveChore :one
SELECT * FROM chore WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: ArchiveChore :execrows
UPDATE chore SET archived_at = sqlc.arg(now) WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: CreateRoutine :one
INSERT INTO routine (household_id, child_id, title, points, days) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: CreateRoutineStep :one
INSERT INTO routine_step (household_id, routine_id, title, position) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListRoutines :many
SELECT * FROM routine WHERE household_id = $1 AND archived_at IS NULL ORDER BY created_at LIMIT 200;

-- name: ListRoutineSteps :many
SELECT s.* FROM routine_step s JOIN routine r ON r.household_id = s.household_id AND r.id = s.routine_id
WHERE s.household_id = $1 AND r.archived_at IS NULL
ORDER BY s.routine_id, s.position;

-- name: GetLiveRoutine :one
SELECT * FROM routine WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: ArchiveRoutine :execrows
UPDATE routine SET archived_at = sqlc.arg(now) WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: EnsureOccurrence :exec
INSERT INTO occurrence (id, household_id, child_id, kind, definition_id, day, points)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (definition_id, day) DO NOTHING;

-- name: LockOccurrence :one
SELECT * FROM occurrence WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- name: UpdateOccurrence :one
UPDATE occurrence
SET state = $3, checked_steps = $4, points = $5, rejected_reason = sqlc.narg(rejected_reason), updated_at = clock_timestamp()
WHERE household_id = $1 AND id = $2
RETURNING *;

-- name: ListOccurrences :many
SELECT * FROM occurrence WHERE household_id = $1 AND day = $2;

-- name: CreateApproval :one
INSERT INTO approval (household_id, child_id, kind, occurrence_id, title, points)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: EarnedSince :many
SELECT child_id, COALESCE(SUM(delta), 0)::bigint AS earned FROM point_ledger
WHERE household_id = $1 AND kind = 'award' AND created_at >= $2
GROUP BY child_id;

-- name: Balances :many
SELECT child_id, COALESCE(SUM(delta), 0)::bigint AS balance FROM point_ledger
WHERE household_id = $1 GROUP BY child_id;

-- name: ListStepsOfRoutine :many
SELECT * FROM routine_step WHERE household_id = $1 AND routine_id = $2 ORDER BY position;
