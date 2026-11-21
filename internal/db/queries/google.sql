-- Google calendar (slice 13b): connections, connects in progress, and Google sources.

-- name: CreateGoogleOAuthState :exec
INSERT INTO google_oauth_state (id, state_hash, household_id, adult_id, verifier, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteExpiredGoogleOAuthStates :exec
DELETE FROM google_oauth_state WHERE household_id = $1 AND expires_at < now();

-- name: TakeGoogleOAuthState :one
DELETE FROM google_oauth_state
WHERE state_hash = $1 AND household_id = $2 AND adult_id = $3 AND expires_at > now()
RETURNING id, verifier;

-- name: ListGoogleOAuthStates :many
SELECT id, household_id, verifier FROM google_oauth_state ORDER BY id;

-- name: SetGoogleOAuthStateVerifier :exec
UPDATE google_oauth_state SET verifier = $2 WHERE id = $1;

-- name: CountGoogleConnections :one
SELECT count(*) FROM calendar_google_connection WHERE household_id = $1;

-- name: GetGoogleConnectionBySubject :one
SELECT * FROM calendar_google_connection WHERE household_id = $1 AND subject = $2 FOR UPDATE;

-- name: GetGoogleConnection :one
SELECT * FROM calendar_google_connection WHERE household_id = $1 AND id = $2;

-- name: CreateGoogleConnection :one
INSERT INTO calendar_google_connection (id, household_id, subject, email, scope, refresh_token)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ReconnectGoogleConnection :one
UPDATE calendar_google_connection SET email = $3, scope = $4, refresh_token = $5, needs_reconnect = false,
    version = version + 1
WHERE household_id = $1 AND id = $2
RETURNING *;

-- name: ListGoogleConnections :many
SELECT c.id, c.email, c.needs_reconnect, c.version, c.created_at,
    (SELECT count(*) FROM calendar_source s WHERE s.household_id = c.household_id AND s.google_connection_id = c.id)::integer AS calendars
FROM calendar_google_connection c
WHERE c.household_id = $1
ORDER BY lower(c.email), c.id;

-- name: DeleteGoogleConnection :one
DELETE FROM calendar_google_connection WHERE household_id = $1 AND id = $2 AND version = @expected_version
RETURNING refresh_token;

-- Both take the token that was read: a reconnect in between (a new token) wins, and these do
-- nothing.

-- name: GoogleConnectionNeedsReconnect :exec
UPDATE calendar_google_connection SET needs_reconnect = true
WHERE household_id = $1 AND id = $2 AND refresh_token = @read_token;

-- name: SetGoogleRefreshToken :exec
UPDATE calendar_google_connection SET refresh_token = @new_token
WHERE household_id = $1 AND id = $2 AND refresh_token = @read_token;

-- name: ListGoogleRefreshTokens :many
SELECT id, household_id, refresh_token::text FROM calendar_google_connection WHERE refresh_token IS NOT NULL ORDER BY id;

-- name: WakeGoogleSources :exec
UPDATE calendar_source SET next_sync_at = now() WHERE household_id = $1 AND google_connection_id = $2;

-- name: CreateGoogleCalendarSource :one
INSERT INTO calendar_source (id, household_id, provider, name, color, show_on_display, google_connection_id, google_calendar_id)
VALUES ($1, $2, 'google', $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListFollowedGoogleCalendars :many
SELECT id, google_calendar_id FROM calendar_source
WHERE household_id = $1 AND google_connection_id = $2
ORDER BY id;

-- name: SetGoogleSyncToken :exec
UPDATE calendar_source SET google_sync_token = sqlc.narg(sync_token) WHERE id = $1;
