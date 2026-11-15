-- name: DeleteExpiredPairings :exec
DELETE FROM display_pairing WHERE expires_at <= clock_timestamp();

-- name: CountPairings :one
SELECT count(*) FROM display_pairing WHERE expires_at > clock_timestamp();

-- name: CreatePairing :one
INSERT INTO display_pairing (claim_hash, code_hash, expires_at)
VALUES ($1, $2, clock_timestamp() + interval '10 minutes')
RETURNING expires_at;

-- name: LockOpenPairingByCode :one
SELECT id FROM display_pairing
WHERE code_hash = $1 AND display_id IS NULL AND expires_at > clock_timestamp()
FOR UPDATE;

-- name: ApprovePairing :exec
UPDATE display_pairing SET household_id = $2, display_id = $3 WHERE id = $1;

-- name: LockPairingByClaim :one
SELECT id, household_id, display_id FROM display_pairing
WHERE claim_hash = $1 AND expires_at > clock_timestamp()
FOR UPDATE;

-- name: DeletePairing :exec
DELETE FROM display_pairing WHERE id = $1;

-- name: CreateDisplay :one
INSERT INTO display (household_id, name, scopes, expires_at)
VALUES ($1, $2, $3, sqlc.arg(now)::timestamptz + interval '400 days')
RETURNING *;

-- name: IssueDisplayToken :one
UPDATE display d SET token_hash = $2
FROM household h
WHERE d.id = $1 AND h.id = d.household_id AND d.token_hash IS NULL AND d.revoked_at IS NULL
  AND d.expires_at > clock_timestamp() AND h.archived_at IS NULL
RETURNING d.*;

-- name: ListDisplays :many
SELECT * FROM display
WHERE household_id = $1 AND revoked_at IS NULL AND expires_at > clock_timestamp()
ORDER BY created_at;

-- name: RevokeDisplay :execrows
UPDATE display SET revoked_at = sqlc.arg(now)
WHERE household_id = $1 AND id = $2 AND revoked_at IS NULL;
