-- Slice 11: parent phone sessions and push targets.

-- name: CreatePhoneSession :one
INSERT INTO phone_session (household_id, adult_id, name, token_hash, created_at, expires_at)
VALUES (@household_id, @adult_id, @name, @token_hash, @created_at, @expires_at)
RETURNING *;

-- name: GetPhoneSession :one
SELECT * FROM phone_session WHERE household_id = $1 AND id = $2;

-- name: ListPhoneSessions :many
-- The household's live phone sessions, with the adult each belongs to.
SELECT p.id, p.adult_id, a.name AS adult_name, p.name, p.created_at, p.expires_at, p.last_used_at,
       (p.push_token IS NOT NULL)::boolean AS push_registered
FROM phone_session p
JOIN adult a ON a.household_id = p.household_id AND a.id = p.adult_id
WHERE p.household_id = $1 AND p.revoked_at IS NULL AND p.expires_at > clock_timestamp() AND a.removed_at IS NULL
ORDER BY p.created_at, p.id
LIMIT 200;

-- name: RevokePhoneSession :execrows
UPDATE phone_session
SET revoked_at = COALESCE(revoked_at, clock_timestamp()), push_token = NULL, push_environment = NULL, push_registered_at = NULL
WHERE household_id = $1 AND id = $2 AND revoked_at IS NULL AND expires_at > clock_timestamp();

-- name: TouchPhoneSession :exec
-- Coarse "last used" for Manage: at most one write per session every 5 minutes.
UPDATE phone_session SET last_used_at = clock_timestamp()
WHERE household_id = $1 AND id = $2
  AND (last_used_at IS NULL OR last_used_at < clock_timestamp() - interval '5 minutes');

-- name: ReleasePhonePushToken :exec
-- An APNs token names one app install, so it is unique across every household: registering it
-- takes it from whichever phone session or device held it before (the same phone signed into
-- another household, or a phone that was a child's device). These two are the only push queries
-- not limited to one household: they read nothing and only clear the token.
UPDATE phone_session SET push_token = NULL, push_environment = NULL, push_registered_at = NULL
WHERE push_token = @token AND id <> @keep_id;

-- name: ReleaseDevicePushToken :exec
UPDATE device SET push_token = NULL, push_environment = NULL, push_registered_at = NULL
WHERE push_token = @token AND id <> @keep_id;

-- name: SetPhonePushToken :one
UPDATE phone_session SET push_token = @token, push_environment = @environment, push_registered_at = clock_timestamp()
WHERE household_id = @household_id AND id = @id AND revoked_at IS NULL
RETURNING push_registered_at::timestamptz;

-- name: ClearPhonePushToken :exec
-- Without a token argument it clears whatever is registered; with one, only that token (a
-- refusal from APNs must not clear a token registered since).
UPDATE phone_session SET push_token = NULL, push_environment = NULL, push_registered_at = NULL
WHERE household_id = @household_id AND id = @id AND (sqlc.narg(token)::text IS NULL OR push_token = sqlc.narg(token));

-- name: SetDevicePushToken :one
UPDATE device SET push_token = @token, push_environment = @environment, push_registered_at = clock_timestamp()
WHERE household_id = @household_id AND id = @id AND revoked_at IS NULL
RETURNING push_registered_at::timestamptz;

-- name: ClearDevicePushToken :exec
UPDATE device SET push_token = NULL, push_environment = NULL, push_registered_at = NULL
WHERE household_id = @household_id AND id = @id AND (sqlc.narg(token)::text IS NULL OR push_token = sqlc.narg(token));

-- name: PushAdults :many
-- The adults of a household with at least one phone that can receive a push.
SELECT DISTINCT a.id FROM adult a
JOIN phone_session p ON p.household_id = a.household_id AND p.adult_id = a.id
WHERE a.household_id = $1 AND a.removed_at IS NULL
  AND p.revoked_at IS NULL AND p.expires_at > clock_timestamp() AND p.push_token IS NOT NULL
ORDER BY a.id;

-- name: AdultPushTargets :many
-- An adult's phones that can receive a push, and whether the adult has muted alerts.
SELECT p.id, p.push_token::text AS push_token, p.push_environment::text AS push_environment,
       COALESCE(a.push_muted_until > clock_timestamp(), false)::boolean AS muted
FROM phone_session p
JOIN adult a ON a.household_id = p.household_id AND a.id = p.adult_id
JOIN household h ON h.id = p.household_id
WHERE p.household_id = $1 AND p.adult_id = $2 AND p.push_token IS NOT NULL
  AND p.revoked_at IS NULL AND p.expires_at > clock_timestamp()
  AND a.removed_at IS NULL AND h.archived_at IS NULL
ORDER BY p.id;

-- name: ChildPushTargets :many
-- A child's paired devices that can receive a push.
SELECT d.id, d.push_token::text AS push_token, d.push_environment::text AS push_environment
FROM device d
JOIN child c ON c.household_id = d.household_id AND c.id = d.child_id
JOIN household h ON h.id = d.household_id
WHERE d.household_id = $1 AND d.child_id = $2 AND d.push_token IS NOT NULL AND d.revoked_at IS NULL
  AND c.archived_at IS NULL AND h.archived_at IS NULL
ORDER BY d.id;

-- name: CountPendingInbox :one
-- The badge: everything waiting for an adult (the inbox), as ListPendingApprovals counts it.
SELECT count(*)::integer FROM approval a
JOIN child c ON c.household_id = a.household_id AND c.id = a.child_id
WHERE a.household_id = $1 AND a.state = 'pending' AND c.archived_at IS NULL;

-- name: SetPushMute :one
-- Old #87: an adult pauses their own phone alerts until a time, or (NULL) turns them back on.
UPDATE adult SET push_muted_until = sqlc.narg(until)
WHERE household_id = @household_id AND id = @id AND removed_at IS NULL
RETURNING push_muted_until;
