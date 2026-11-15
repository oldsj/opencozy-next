-- name: ListCredentials :many
SELECT * FROM credential
WHERE household_id = $1 AND revoked_at IS NULL
ORDER BY created_at
LIMIT 200;

-- name: GetCredential :one
SELECT * FROM credential WHERE household_id = $1 AND id = $2;

-- name: CreateCredential :one
INSERT INTO credential (household_id, kind, name, token_hash, scopes, all_children, adult_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: AddCredentialChild :exec
INSERT INTO credential_child (household_id, credential_id, child_id) VALUES ($1, $2, $3);

-- name: RevokeCredential :execrows
UPDATE credential SET revoked_at = sqlc.arg(now)::timestamptz
WHERE household_id = $1 AND id = $2 AND revoked_at IS NULL;

-- name: ListHouseholdCredentialChildren :many
SELECT credential_id, child_id FROM credential_child
WHERE household_id = $1
ORDER BY credential_id, child_id;
