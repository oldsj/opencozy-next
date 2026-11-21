-- Actor resolution. Each lookup returns a row only while the credential is live: not expired, not
-- revoked, its household not archived and (where it has one) its adult not removed.

-- name: CreateAdultSession :exec
-- authenticated_at is now, or when the provider says the person last signed in there (a
-- single sign-on's auth_time), never later than now.
INSERT INTO adult_session (token_hash, adult_id, expires_at, authenticated_at)
VALUES ($1, $2, $3, LEAST(COALESCE(sqlc.narg(authenticated_at)::timestamptz, now()), now()));

-- name: GetAdultSession :one
SELECT a.id AS adult_id, a.household_id, s.authenticated_at
FROM adult_session s
JOIN adult a ON a.id = s.adult_id
JOIN household h ON h.id = a.household_id
WHERE s.token_hash = $1 AND s.expires_at > now()
  AND a.removed_at IS NULL AND h.archived_at IS NULL;

-- name: ConfirmAdultSession :exec
UPDATE adult_session SET authenticated_at = now() WHERE token_hash = $1;

-- name: DeleteAdultSession :exec
DELETE FROM adult_session WHERE token_hash = $1;

-- name: DeleteExpiredAdultSessions :execrows
DELETE FROM adult_session WHERE expires_at <= now();

-- name: GetCredentialByHash :one
SELECT c.id, c.household_id, c.kind, c.scopes, c.all_children, c.adult_id
FROM credential c
JOIN household h ON h.id = c.household_id
LEFT JOIN adult a ON a.id = c.adult_id
WHERE c.token_hash = $1 AND c.revoked_at IS NULL
  AND (c.expires_at IS NULL OR c.expires_at > now())
  AND h.archived_at IS NULL
  AND (c.kind <> 'assistant' OR a.removed_at IS NULL);

-- name: ListCredentialChildIDs :many
SELECT child_id FROM credential_child
WHERE household_id = $1 AND credential_id = $2
ORDER BY child_id;

-- name: GetPhoneSessionByHash :one
SELECT p.id, p.household_id, p.adult_id
FROM phone_session p
JOIN adult a ON a.household_id = p.household_id AND a.id = p.adult_id
JOIN household h ON h.id = p.household_id
WHERE p.token_hash = $1 AND p.revoked_at IS NULL AND p.expires_at > now()
  AND a.removed_at IS NULL AND h.archived_at IS NULL;

-- name: GetDisplayByHash :one
SELECT d.id, d.household_id, d.scopes
FROM display d
JOIN household h ON h.id = d.household_id
WHERE d.token_hash = $1 AND d.revoked_at IS NULL AND d.expires_at > now()
  AND h.archived_at IS NULL;
