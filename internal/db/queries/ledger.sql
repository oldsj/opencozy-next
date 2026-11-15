-- name: InsertLedgerEntry :one
INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id, reverses_id, adult_id, credential_id, note)
VALUES (@household_id, @child_id, @delta, @kind, @source_kind, @source_id, sqlc.narg(reverses_id), sqlc.narg(adult_id), sqlc.narg(credential_id), sqlc.narg(note))
RETURNING *;

-- name: GetLedgerEntry :one
SELECT * FROM point_ledger WHERE household_id = $1 AND id = $2;

-- name: ChildBalance :one
SELECT COALESCE(SUM(delta), 0)::bigint AS balance FROM point_ledger
WHERE household_id = $1 AND child_id = $2;

-- name: GetActiveAward :one
SELECT ledger_id FROM active_award
WHERE household_id = $1 AND source_kind = $2 AND source_id = $3;

-- name: InsertActiveAward :exec
INSERT INTO active_award (household_id, source_kind, source_id, ledger_id) VALUES ($1, $2, $3, $4);

-- name: DeleteActiveAward :one
DELETE FROM active_award
WHERE household_id = $1 AND source_kind = $2 AND source_id = $3
RETURNING ledger_id;

-- name: RefundedAmount :one
SELECT COALESCE(SUM(delta), 0)::bigint AS refunded FROM point_ledger
WHERE household_id = $1 AND reverses_id = $2 AND kind = 'refund';
