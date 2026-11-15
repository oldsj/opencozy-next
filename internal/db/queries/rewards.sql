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
UPDATE redemption SET state = $3, version = version + 1 WHERE household_id = $1 AND id = $2;

-- name: CreateRedemptionApproval :one
INSERT INTO approval (household_id, child_id, kind, redemption_id, title, points)
VALUES ($1, $2, 'redemption', $3, $4, $5)
RETURNING *;

-- name: UpdateReward :one
UPDATE reward SET title = $3, cost = $4, kind = $5, minutes = $6, available = $7, version = version + 1
WHERE household_id = $1 AND id = $2 AND archived_at IS NULL AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: ListRedemptions :many
-- What adults see: the newest first, with the points that came back.
SELECT r.*, c.name AS child_name, c.color AS child_color,
       COALESCE((SELECT sum(l.delta) FROM point_ledger l
                 WHERE l.household_id = r.household_id AND l.reverses_id = r.ledger_id AND l.kind = 'refund'), 0)::bigint AS refunded_points
FROM redemption r
JOIN child c ON c.household_id = r.household_id AND c.id = r.child_id
WHERE r.household_id = $1
  AND (sqlc.narg(state)::text IS NULL OR r.state = sqlc.narg(state)::text)
ORDER BY r.created_at DESC, r.id DESC
LIMIT 200;

-- name: GetRedemptionRow :one
SELECT r.*, c.name AS child_name, c.color AS child_color,
       COALESCE((SELECT sum(l.delta) FROM point_ledger l
                 WHERE l.household_id = r.household_id AND l.reverses_id = r.ledger_id AND l.kind = 'refund'), 0)::bigint AS refunded_points
FROM redemption r
JOIN child c ON c.household_id = r.household_id AND c.id = r.child_id
WHERE r.household_id = $1 AND r.id = $2;

-- name: FulfilRedemption :exec
UPDATE redemption SET state = 'fulfilled', fulfilled_at = sqlc.arg(now), version = version + 1
WHERE household_id = $1 AND id = $2;

-- name: CancelRedemption :exec
UPDATE redemption SET state = 'cancelled', cancelled_at = sqlc.arg(now), version = version + 1
WHERE household_id = $1 AND id = $2;
