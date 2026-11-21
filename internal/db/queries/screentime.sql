-- iPad screen time (slice 8): allowed hours, banked credits, unlock windows and their cuts,
-- shield events and time requests.

-- name: GetChildHours :one
SELECT c.id, c.archived_at, c.school_night_start, c.school_night_end, c.weekend_start, c.weekend_end, h.time_zone
FROM child c JOIN household h ON h.id = c.household_id
WHERE c.household_id = $1 AND c.id = $2;

-- name: SetChildHours :exec
UPDATE child
SET school_night_start = sqlc.narg(school_night_start), school_night_end = sqlc.narg(school_night_end),
    weekend_start = sqlc.narg(weekend_start), weekend_end = sqlc.narg(weekend_end)
WHERE household_id = $1 AND id = $2;

-- name: UnspentCredits :many
-- Oldest first: spending draws on the oldest credit first.
SELECT * FROM screen_time_credit
WHERE household_id = $1 AND child_id = $2 AND used_seconds < seconds
ORDER BY created_at, id;

-- name: BankedSeconds :one
SELECT COALESCE(sum(seconds - used_seconds), 0)::bigint FROM screen_time_credit
WHERE household_id = $1 AND child_id = $2 AND used_seconds < seconds;

-- name: InsertCredit :one
INSERT INTO screen_time_credit (household_id, child_id, source, redemption_id, granted_by, granted_via, minutes, seconds, created_at)
VALUES ($1, $2, $3, $4, $5, sqlc.narg(granted_via), $6, $7, @now)
RETURNING *;

-- name: SetCreditUsed :exec
UPDATE screen_time_credit SET used_seconds = $3 WHERE household_id = $1 AND id = $2;

-- name: SetCreditSeconds :exec
UPDATE screen_time_credit SET seconds = $3 WHERE household_id = $1 AND id = $2;

-- name: GetCredit :one
SELECT * FROM screen_time_credit WHERE household_id = $1 AND id = $2;

-- name: CreditForRedemption :one
SELECT * FROM screen_time_credit WHERE household_id = $1 AND redemption_id = $2;

-- name: InsertWindow :one
INSERT INTO screen_time_window (household_id, child_id, credit_id, starts_at, ends_at, created_at)
VALUES ($1, $2, $3, $4, $5, @now)
RETURNING *;

-- name: OpenWindows :many
-- The child's windows still running or queued after @at, in queue order, each to its effective
-- end (a cut ends it early; a queued window that was cut never runs).
SELECT w.id, w.credit_id, w.starts_at, w.ends_at AS planned_end,
       LEAST(w.ends_at, COALESCE(c.cut_at, w.ends_at))::timestamptz AS ends_at,
       (c.window_id IS NOT NULL)::boolean AS cut
FROM screen_time_window w
LEFT JOIN screen_time_cut c ON c.household_id = w.household_id AND c.window_id = w.id
WHERE w.household_id = $1 AND w.child_id = $2
  AND w.ends_at > @at
  AND LEAST(w.ends_at, COALESCE(c.cut_at, w.ends_at)) > @at
  AND LEAST(w.ends_at, COALESCE(c.cut_at, w.ends_at)) > w.starts_at
ORDER BY w.starts_at, w.id;

-- name: InsertCut :exec
INSERT INTO screen_time_cut (household_id, window_id, cut_at, refunded_seconds, cut_by, created_at)
VALUES ($1, $2, $3, $4, $5, @now);

-- name: InsertScreenTimeEvent :execrows
INSERT INTO screen_time_event (household_id, id, child_id, device_id, kind, occurred_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, @now)
ON CONFLICT (household_id, id) DO NOTHING;

-- name: LatestTimeRequest :one
SELECT * FROM time_request
WHERE household_id = $1 AND child_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: InsertTimeRequest :one
INSERT INTO time_request (household_id, child_id, device_id, asked_at, created_at)
VALUES ($1, $2, $3, @now, @now)
RETURNING *;

-- name: RemindTimeRequest :one
UPDATE time_request SET asked_at = @now
WHERE household_id = $1 AND id = $2 AND state = 'pending'
RETURNING *;

-- name: LockTimeRequest :one
SELECT * FROM time_request WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- name: DecideTimeRequest :one
UPDATE time_request
SET state = $3, choice = $4, decided_at = @now, decided_by = $5, credit_id = $6, granted_until = $7
WHERE household_id = $1 AND id = $2 AND state = 'pending'
RETURNING *;

-- name: CreateTimeRequestApproval :one
INSERT INTO approval (household_id, child_id, kind, time_request_id, title, points)
VALUES ($1, $2, 'time_request', $3, $4, 0)
RETURNING *;

-- name: PendingApprovalForTimeRequest :one
SELECT * FROM approval WHERE household_id = $1 AND time_request_id = $2 AND state = 'pending';

-- name: OpenCredits :many
-- The child's credits with time left, oldest first, with who gave each: the oldest 200 (every
-- device poll reads it; bankedSeconds is still the whole bank).
SELECT k.id, k.source, k.redemption_id, r.title AS reward_title, k.granted_by, a.name AS granted_by_name,
       k.minutes, k.seconds, k.used_seconds, k.created_at
FROM screen_time_credit k
LEFT JOIN redemption r ON r.household_id = k.household_id AND r.id = k.redemption_id
LEFT JOIN adult a ON a.household_id = k.household_id AND a.id = k.granted_by
WHERE k.household_id = $1 AND k.child_id = $2 AND k.used_seconds < k.seconds
ORDER BY k.created_at, k.id
LIMIT 200;

-- name: LockCredit :one
SELECT * FROM screen_time_credit WHERE household_id = $1 AND child_id = $2 AND id = $3 FOR UPDATE;

-- name: ForfeitCredit :exec
UPDATE screen_time_credit
SET seconds = used_seconds, forfeited_at = @now, forfeited_by = $3, forfeited_seconds = seconds - used_seconds
WHERE household_id = $1 AND id = $2 AND used_seconds < seconds AND source <> 'redemption';

-- name: ChildScreenTimeEvents :many
-- A child's newest shield events, for their history, with the device's name.
SELECT e.id, e.kind, e.occurred_at, d.name AS device_name
FROM screen_time_event e
LEFT JOIN device d ON d.household_id = e.household_id AND d.id = e.device_id
WHERE e.household_id = $1 AND e.child_id = $2
ORDER BY e.occurred_at DESC, e.id DESC
LIMIT 200;

-- name: GetWebFilter :one
SELECT game_portal_domains, game_domains FROM household WHERE id = $1;

-- name: SetWebFilter :one
UPDATE household SET game_portal_domains = @game_portals::text[], game_domains = @games::text[]
WHERE id = $1
RETURNING game_portal_domains, game_domains;
