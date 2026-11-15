-- Slice 0d: job leases (internal/jobs), the push outbox (internal/push) and live events
-- (internal/live).

-- name: TakeLease :one
-- Takes the lease when it is free, expired or already ours, and extends it. No row means another
-- replica holds it.
INSERT INTO job_lease (name, holder, expires_at)
VALUES (@name, @holder, clock_timestamp() + make_interval(secs => @ttl_seconds::float8))
ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
WHERE job_lease.expires_at < clock_timestamp() OR job_lease.holder = EXCLUDED.holder
RETURNING expires_at;

-- name: FenceLease :one
-- The first statement of every job transaction. FOR SHARE keeps another replica from taking the
-- lease over until this transaction ends.
SELECT expires_at FROM job_lease
WHERE name = @name AND holder = @holder AND expires_at > clock_timestamp()
FOR SHARE;

-- name: ReleaseLease :exec
DELETE FROM job_lease WHERE name = @name AND holder = @holder;

-- name: EnqueuePush :exec
INSERT INTO push_outbox (household_id, adult_id, child_id, dedupe_key, payload)
VALUES (@household_id, sqlc.narg(adult_id), sqlc.narg(child_id), @dedupe_key, @payload)
ON CONFLICT (household_id, dedupe_key) DO NOTHING;

-- name: NotifyPushOutbox :exec
SELECT pg_notify('push_outbox', '');

-- name: ClaimPushes :many
SELECT * FROM push_outbox
WHERE next_attempt_at <= clock_timestamp()
ORDER BY next_attempt_at
LIMIT @max_rows
FOR UPDATE SKIP LOCKED;

-- name: DeletePush :exec
DELETE FROM push_outbox WHERE id = @id;

-- name: RetryPush :exec
UPDATE push_outbox
SET attempts = attempts + 1, next_attempt_at = clock_timestamp() + make_interval(secs => @delay_seconds::float8)
WHERE id = @id;

-- name: NotifyLive :exec
SELECT pg_notify('opencozy_live', @payload::text);
