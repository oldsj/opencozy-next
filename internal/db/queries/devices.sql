-- name: DeleteExpiredDeviceState :exec
WITH p AS (DELETE FROM device_pairing WHERE expires_at <= clock_timestamp()),
     l AS (DELETE FROM device_web_link WHERE expires_at <= clock_timestamp()),
     n AS (DELETE FROM device_nonce WHERE seen_at <= clock_timestamp() - interval '300 seconds')
DELETE FROM device_web_session WHERE expires_at <= clock_timestamp();

-- name: CreateDevicePairing :one
INSERT INTO device_pairing (household_id, child_id, token_hash, code_hash, expires_at)
VALUES ($1, $2, $3, $4, clock_timestamp() + interval '5 minutes')
RETURNING id, expires_at;

-- name: LockDevicePairing :one
-- An open offer for a live child in a live household, by its token or its code.
SELECT p.id, p.household_id, p.child_id
FROM device_pairing p
JOIN household h ON h.id = p.household_id
JOIN child c ON c.household_id = p.household_id AND c.id = p.child_id
WHERE (p.token_hash = sqlc.narg(token_hash) OR p.code_hash = sqlc.narg(code_hash))
  AND p.expires_at > clock_timestamp() AND h.archived_at IS NULL AND c.archived_at IS NULL
FOR UPDATE OF p;

-- name: DeleteDevicePairing :exec
DELETE FROM device_pairing WHERE id = $1;

-- name: CreateDevice :one
-- No row when the key is already paired.
INSERT INTO device (household_id, child_id, name, platform, public_key)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (public_key) DO NOTHING
RETURNING id;

-- name: GetSigningDevice :one
-- A device that may act: not revoked, its household and child live.
SELECT d.id, d.household_id, d.child_id::uuid AS child_id, d.public_key
FROM device d
JOIN household h ON h.id = d.household_id
JOIN child c ON c.household_id = d.household_id AND c.id = d.child_id
WHERE d.id = $1 AND d.revoked_at IS NULL AND h.archived_at IS NULL AND c.archived_at IS NULL;

-- name: RecordDeviceNonce :execrows
-- One row when the nonce is new (or older than 300 seconds), none when it is a replay. A request
-- is acceptable for 120 seconds (timestamp ±60 s); 300 leaves room for the app's and the
-- database's clocks to differ.
INSERT INTO device_nonce (device_id, nonce) VALUES ($1, $2)
ON CONFLICT (device_id, nonce) DO UPDATE SET seen_at = clock_timestamp()
WHERE device_nonce.seen_at <= clock_timestamp() - interval '300 seconds';

-- name: TouchDevice :exec
UPDATE device SET last_seen_at = clock_timestamp()
WHERE id = $1 AND (last_seen_at IS NULL OR last_seen_at < clock_timestamp() - interval '1 minute');

-- name: GetDeviceView :one
SELECT d.id, d.name, d.platform, d.paired_at, d.last_seen_at,
       c.id AS child_id, c.name AS child_name, c.color AS child_color,
       h.name AS household_name, h.time_zone, h.lock_message
FROM device d
JOIN household h ON h.id = d.household_id
LEFT JOIN child c ON c.household_id = d.household_id AND c.id = d.child_id AND c.archived_at IS NULL
WHERE d.household_id = $1 AND d.id = $2 AND d.revoked_at IS NULL;

-- name: ListDevices :many
SELECT d.id, d.name, d.platform, d.paired_at, d.last_seen_at,
       c.id AS child_id, c.name AS child_name, c.color AS child_color
FROM device d
LEFT JOIN child c ON c.household_id = d.household_id AND c.id = d.child_id AND c.archived_at IS NULL
WHERE d.household_id = $1 AND d.revoked_at IS NULL
ORDER BY d.paired_at DESC, d.id
LIMIT 200;

-- name: UpdateDevice :execrows
UPDATE device
SET name = coalesce(sqlc.narg(name), name), child_id = coalesce(sqlc.narg(child_id), child_id)
WHERE household_id = $1 AND id = $2 AND revoked_at IS NULL;

-- name: RevokeDevice :execrows
UPDATE device SET revoked_at = sqlc.arg(now), push_token = NULL, push_environment = NULL, push_registered_at = NULL
WHERE household_id = $1 AND id = $2 AND revoked_at IS NULL;

-- name: DeleteDeviceWebSessions :exec
DELETE FROM device_web_session WHERE device_id = $1;

-- name: GetLockMessage :one
SELECT lock_message FROM household WHERE id = $1;

-- name: SetLockMessage :exec
UPDATE household SET lock_message = $2 WHERE id = $1;

-- name: CreateDeviceWebLink :one
INSERT INTO device_web_link (token_hash, device_id, expires_at)
VALUES ($1, $2, clock_timestamp() + interval '60 seconds')
RETURNING expires_at;

-- name: UseDeviceWebLink :one
DELETE FROM device_web_link WHERE token_hash = $1 AND expires_at > clock_timestamp()
RETURNING device_id;

-- name: CreateDeviceWebSession :one
INSERT INTO device_web_session (token_hash, device_id, expires_at)
VALUES ($1, $2, clock_timestamp() + interval '24 hours')
RETURNING expires_at;

-- name: GetDeviceWebSession :one
-- A live session of a device that may act.
SELECT s.device_id, s.expires_at, d.household_id, d.child_id::uuid AS child_id
FROM device_web_session s
JOIN device d ON d.id = s.device_id
JOIN household h ON h.id = d.household_id
JOIN child c ON c.household_id = d.household_id AND c.id = d.child_id
WHERE s.token_hash = $1 AND s.expires_at > clock_timestamp()
  AND d.revoked_at IS NULL AND h.archived_at IS NULL AND c.archived_at IS NULL;
