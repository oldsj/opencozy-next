-- name: ListRewards :many
SELECT * FROM reward WHERE household_id = $1 AND archived_at IS NULL ORDER BY cost, title, id;

-- name: CreateReward :one
INSERT INTO reward (household_id, title, cost, kind, minutes) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetLiveReward :one
SELECT * FROM reward WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: ArchiveReward :execrows
UPDATE reward SET archived_at = @now WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: CreateRedemption :one
INSERT INTO redemption (household_id, child_id, reward_id, title, cost, minutes, state)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: SetRedemptionLedger :exec
UPDATE redemption SET ledger_id = $3 WHERE household_id = $1 AND id = $2;

-- name: LockRedemption :one
SELECT * FROM redemption WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- name: SetRedemptionState :exec
UPDATE redemption SET state = $3 WHERE household_id = $1 AND id = $2;

-- name: CreateRedemptionApproval :one
INSERT INTO approval (household_id, child_id, kind, redemption_id, title, points)
VALUES ($1, $2, 'redemption', $3, $4, $5)
RETURNING *;
