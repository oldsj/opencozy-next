-- name: ListAwardRules :many
SELECT * FROM award_rule
WHERE household_id = $1 AND (sqlc.arg(include_archived)::boolean OR archived_at IS NULL)
ORDER BY activity_key, created_at
LIMIT 500;

-- name: CountLiveAwardRules :one
SELECT count(*) FROM award_rule WHERE household_id = $1 AND archived_at IS NULL;

-- name: GetAwardRule :one
SELECT * FROM award_rule WHERE household_id = $1 AND id = $2;

-- name: LiveAwardRuleIDs :many

SELECT id FROM award_rule WHERE household_id = @household_id::uuid AND id = ANY(@ids::uuid[]) AND archived_at IS NULL
ORDER BY id FOR UPDATE;

-- name: LiveRuleForActivity :one
SELECT * FROM award_rule WHERE household_id = $1 AND activity_key = $2 AND archived_at IS NULL FOR UPDATE;

-- name: ActivityTaken :one
SELECT EXISTS (SELECT 1 FROM award_rule WHERE household_id = $1 AND activity_key = $2 AND archived_at IS NULL AND id <> sqlc.arg(except_id)::uuid)::boolean;

-- name: CreateAwardRule :one
INSERT INTO award_rule (household_id, activity_key, points, daily_limit) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateAwardRule :one
UPDATE award_rule SET activity_key = $3, points = $4, daily_limit = $5, version = version + 1,
    archived_at = CASE WHEN sqlc.arg(archive)::boolean THEN sqlc.arg(now)::timestamptz ELSE NULL END
WHERE household_id = $1 AND id = $2 AND version = sqlc.arg(expected_version) AND archived_at IS NULL
RETURNING *;

-- name: ListLearningLinks :many
SELECT l.* FROM learning_link l JOIN child c ON c.household_id = l.household_id AND c.id = l.child_id
WHERE l.household_id = $1 AND l.unlinked_at IS NULL AND c.archived_at IS NULL
ORDER BY l.provider, l.external_id
LIMIT 500;

-- name: LinkTaken :one
SELECT EXISTS (SELECT 1 FROM learning_link WHERE household_id = $1 AND provider = $2 AND external_id = $3 AND unlinked_at IS NULL)::boolean;

-- name: CreateLearningLink :one
INSERT INTO learning_link (household_id, child_id, provider, external_id) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UnlinkLearner :execrows
UPDATE learning_link SET unlinked_at = greatest(sqlc.arg(now)::timestamptz, linked_at)
WHERE household_id = $1 AND id = $2 AND unlinked_at IS NULL;

-- name: LinkedChild :one

SELECT l.child_id, (c.archived_at IS NOT NULL)::boolean AS archived
FROM learning_link l JOIN child c ON c.household_id = l.household_id AND c.id = l.child_id
WHERE l.household_id = $1 AND l.provider = $2 AND l.external_id = $3 AND l.unlinked_at IS NULL;

-- name: GetLearningAwardByEvent :one
SELECT * FROM learning_award WHERE household_id = $1 AND source_event_id = $2;

-- name: PointsAwardedOnDay :one
SELECT coalesce(sum(points), 0)::bigint FROM learning_award
WHERE household_id = $1 AND child_id = $2 AND rule_id = $3 AND day = $4;

-- name: InsertLearningAward :one
INSERT INTO learning_award (id, household_id, source_event_id, child_id, rule_id, rule, points, day,
    provider, external_id, activity_key, credential_id, ledger_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;
