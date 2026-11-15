-- Every query on household data takes the household ID from the actor, never from the request.

-- name: CountHouseholds :one
SELECT count(*) FROM household;

-- name: CreateHousehold :one
INSERT INTO household (name, time_zone) VALUES ($1, $2) RETURNING *;

-- name: GetHousehold :one
SELECT * FROM household WHERE id = $1;

-- name: ListHouseholds :many
SELECT * FROM household ORDER BY created_at LIMIT 100;

-- name: CreateAdult :one
INSERT INTO adult (household_id, email, name, password_hash, password_scheme, webauthn_user_handle)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAdultByEmail :one
SELECT a.* FROM adult a
JOIN household h ON h.id = a.household_id
WHERE a.email = $1 AND a.removed_at IS NULL AND h.archived_at IS NULL;

-- name: GetAdult :one
SELECT * FROM adult WHERE household_id = $1 AND id = $2 AND removed_at IS NULL;

-- name: SetAdultPassword :exec
UPDATE adult SET password_hash = $3, password_scheme = $4
WHERE household_id = $1 AND id = $2;
