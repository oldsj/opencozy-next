-- name: LockHousehold :one
SELECT id FROM household WHERE id = $1 FOR UPDATE;

-- name: Now :one
SELECT clock_timestamp()::timestamptz AS now;

-- name: GetCommand :one
SELECT request_hash, status, content_type, body FROM command_log
WHERE household_id = $1 AND idempotency_key = $2 AND created_at > clock_timestamp() - interval '30 days';

-- name: DeleteExpiredCommand :exec
-- An expired row for this key may not have been swept yet; command_log rows never change, so it
-- goes before the new answer is recorded.
DELETE FROM command_log
WHERE household_id = $1 AND idempotency_key = $2 AND created_at < clock_timestamp() - interval '30 days';

-- name: RecordCommand :exec
INSERT INTO command_log (household_id, idempotency_key, request_hash, status, content_type, body)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteExpiredCommands :execrows
DELETE FROM command_log WHERE created_at < clock_timestamp() - interval '30 days';
