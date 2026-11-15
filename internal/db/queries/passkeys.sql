-- name: ListPasskeys :many
SELECT * FROM passkey WHERE household_id = $1 AND adult_id = $2 ORDER BY created_at, id;

-- name: CreatePasskey :one
INSERT INTO passkey (household_id, adult_id, credential_id, public_key, attestation_type, aaguid,
                     sign_count, transports, backup_eligible, backed_up, name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: DeletePasskey :execrows
DELETE FROM passkey WHERE id = $1 AND household_id = $2 AND adult_id = $3;

-- name: UsePasskey :exec
UPDATE passkey SET sign_count = $2, backed_up = $3, last_used_at = now() WHERE credential_id = $1;

-- name: GetAdultByUserHandle :one
SELECT * FROM adult WHERE webauthn_user_handle = $1 AND removed_at IS NULL;

-- name: CreateChallenge :one
INSERT INTO webauthn_challenge (purpose, adult_id, session, expires_at)
VALUES ($1, $2, $3, now() + interval '5 minutes')
RETURNING id;

-- name: TakeChallenge :one
-- Works once: the row is gone after this, whatever the ceremony's outcome.
DELETE FROM webauthn_challenge
WHERE id = $1 AND purpose = $2 AND expires_at > now()
RETURNING adult_id, session;

-- name: DeleteExpiredChallenges :execrows
DELETE FROM webauthn_challenge WHERE expires_at <= now();
