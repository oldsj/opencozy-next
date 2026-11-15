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

-- name: ChildHistory :many
-- A child's newest entries, with what each was for and who decided it.
SELECT l.id, l.kind, l.delta, l.created_at, l.note, l.reverses_id, l.source_kind,
       rv.id AS reversed_by,
       COALESCE(ch.title, rt.title, ap.title, rd.title, la.activity_key, l.source_kind)::text AS title,
       ad.name AS adult_name
FROM point_ledger l
LEFT JOIN point_ledger rv ON rv.reverses_id = l.id AND rv.kind = 'reversal'
LEFT JOIN occurrence oc ON l.source_kind = 'occurrence' AND oc.household_id = l.household_id AND oc.id = l.source_id
LEFT JOIN chore ch ON ch.household_id = oc.household_id AND ch.id = oc.definition_id
LEFT JOIN routine rt ON rt.household_id = oc.household_id AND rt.id = oc.definition_id
LEFT JOIN LATERAL (
    SELECT a.title FROM approval a WHERE a.household_id = l.household_id AND a.occurrence_id = oc.id ORDER BY a.created_at DESC LIMIT 1
) ap ON true
LEFT JOIN redemption rd ON l.source_kind = 'redemption' AND rd.household_id = l.household_id AND rd.id = l.source_id
LEFT JOIN learning_award la ON l.source_kind = 'learning' AND la.household_id = l.household_id AND la.id = l.source_id
LEFT JOIN adult ad ON ad.household_id = l.household_id AND ad.id = l.adult_id
WHERE l.household_id = $1 AND l.child_id = $2
ORDER BY l.created_at DESC, l.id DESC
LIMIT 200;
