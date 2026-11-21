-- name: ListCalendarSources :many
SELECT * FROM calendar_source WHERE household_id = $1 ORDER BY lower(name), id;

-- name: CountCalendarSources :one
SELECT count(*) FROM calendar_source WHERE household_id = $1;

-- name: GetCalendarSource :one
SELECT * FROM calendar_source WHERE household_id = $1 AND id = $2;

-- name: CalendarFeedTaken :one
SELECT EXISTS (SELECT 1 FROM calendar_source WHERE household_id = $1 AND feed_url_mac = ANY(@macs::text[]));

-- name: CreateCalendarSource :one
INSERT INTO calendar_source (id, household_id, provider, name, color, show_on_display, feed_url, feed_url_mac)
VALUES ($1, $2, 'ics', $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateCalendarSource :one
UPDATE calendar_source SET name = $3, color = $4, show_on_display = $5, version = version + 1
WHERE household_id = $1 AND id = $2 AND version = @expected_version
RETURNING *;

-- name: DeleteCalendarSource :execrows
DELETE FROM calendar_source WHERE household_id = $1 AND id = $2 AND version = @expected_version;

-- name: ListFeedURLs :many
SELECT id, household_id, feed_url, feed_url_mac FROM calendar_source WHERE feed_url IS NOT NULL ORDER BY id;

-- name: SetFeedURL :exec
UPDATE calendar_source SET feed_url = $2, feed_url_mac = $3 WHERE id = $1;

-- Sync. ClaimDueFeeds pushes next_sync_at forward before the fetch, so a second run (or a
-- replica that took the lease over) does not fetch the same feed meanwhile.
-- name: ClaimDueFeeds :many
WITH due AS (
    SELECT s.id FROM calendar_source s
    WHERE s.provider IN ('ics', 'google') AND s.next_sync_at <= now()
    ORDER BY s.next_sync_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE calendar_source s SET next_sync_at = now() + interval '10 minutes', last_attempt_at = now()
FROM due, household h
WHERE s.id = due.id AND h.id = s.household_id
RETURNING s.id, s.household_id, s.provider, s.feed_url, s.http_etag, s.http_last_modified, s.feed_hash, s.window_start,
    s.google_connection_id, s.google_calendar_id, s.google_sync_token, h.time_zone;

-- name: LockCalendarSource :one
SELECT id, feed_hash, window_start FROM calendar_source WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- name: FeedSynced :exec
UPDATE calendar_source SET last_success_at = now(), error_code = sqlc.narg(error_code), consecutive_failures = 0,
    next_sync_at = now() + interval '15 minutes', http_etag = sqlc.narg(etag), http_last_modified = sqlc.narg(last_modified),
    feed_hash = coalesce(sqlc.narg(feed_hash), feed_hash), window_start = coalesce(sqlc.narg(window_start), window_start)
WHERE id = $1;

-- name: FeedFailed :exec
UPDATE calendar_source SET error_code = $2, consecutive_failures = consecutive_failures + 1,
    next_sync_at = now() + make_interval(mins => least(15 * (1 << least(consecutive_failures, 5)), 360))
WHERE id = $1;

-- name: UpsertSeries :many
INSERT INTO calendar_series (household_id, source_id, uid, recurring, last_seen_at)
SELECT @household_id::uuid, @source_id::uuid, u.uid, u.recurring, @now::timestamptz
FROM (SELECT unnest(@uids::text[]) AS uid, unnest(@recurring::boolean[]) AS recurring) AS u
ON CONFLICT (source_id, uid) DO UPDATE SET recurring = EXCLUDED.recurring, last_seen_at = EXCLUDED.last_seen_at
RETURNING id, uid;

-- name: PruneSeries :exec
DELETE FROM calendar_series
WHERE source_id = @source_id::uuid AND NOT (id = ANY(@keep::uuid[])) AND last_seen_at + interval '30 days' < @now::timestamptz;

-- name: UpsertEvents :many
INSERT INTO calendar_event (household_id, source_id, series_id, recurrence_key, all_day, starts_at, ends_at,
    start_date, end_date, title, location, description, status)
SELECT @household_id::uuid, @source_id::uuid, e.series_id, e.key, e.all_day,
    CASE WHEN e.all_day THEN NULL ELSE e.starts_at END, CASE WHEN e.all_day THEN NULL ELSE e.ends_at END,
    NULLIF(e.start_date, '')::date, NULLIF(e.end_date, '')::date,
    NULLIF(e.title, ''), NULLIF(e.location, ''), NULLIF(e.description, ''), e.status
FROM (SELECT unnest(@series_ids::uuid[]) AS series_id, unnest(@keys::text[]) AS key, unnest(@all_day::boolean[]) AS all_day,
    unnest(@starts_at::timestamptz[]) AS starts_at, unnest(@ends_at::timestamptz[]) AS ends_at,
    unnest(@start_dates::text[]) AS start_date, unnest(@end_dates::text[]) AS end_date, unnest(@titles::text[]) AS title,
    unnest(@locations::text[]) AS location, unnest(@descriptions::text[]) AS description, unnest(@statuses::text[]) AS status) AS e
ON CONFLICT (series_id, recurrence_key) DO UPDATE SET all_day = EXCLUDED.all_day, starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at, start_date = EXCLUDED.start_date, end_date = EXCLUDED.end_date, title = EXCLUDED.title,
    location = EXCLUDED.location, description = EXCLUDED.description, status = EXCLUDED.status
RETURNING id;

-- name: DeleteEventsNotIn :exec
DELETE FROM calendar_event WHERE source_id = $1 AND NOT (id = ANY(@keep::uuid[]));

-- Reading. Times overlap [from, to); all-day dates overlap [from_date, to_date).
-- name: ListCalendarEvents :many
SELECT e.id, e.series_id, e.recurrence_key, e.all_day, e.starts_at, e.ends_at, e.start_date, e.end_date,
    e.title, e.location, e.status, s.id AS calendar_id, s.name AS calendar_name, s.color AS calendar_color
FROM calendar_event e JOIN calendar_source s ON s.household_id = e.household_id AND s.id = e.source_id
WHERE e.household_id = $1 AND (s.show_on_display OR NOT @display_only::boolean)
    AND ((NOT e.all_day AND e.starts_at < @to_time::timestamptz
            AND (e.ends_at > @from_time::timestamptz OR e.ends_at = e.starts_at AND e.starts_at >= @from_time::timestamptz))
        OR (e.all_day AND e.start_date < @to_date::date AND e.end_date > @from_date::date))
ORDER BY e.id
LIMIT 3000;

-- name: GetCalendarEvent :one
SELECT e.*, s.name AS calendar_name, s.color AS calendar_color, s.show_on_display, r.recurring
FROM calendar_event e
    JOIN calendar_source s ON s.household_id = e.household_id AND s.id = e.source_id
    JOIN calendar_series r ON r.household_id = e.household_id AND r.id = e.series_id
WHERE e.household_id = $1 AND e.id = $2;

-- The live children assigned to these series: rows with child_id NULL are assignments to nobody.
-- name: ListAssignments :many
SELECT a.series_id, a.recurrence_key, c.id AS child_id
FROM calendar_assignment a
    LEFT JOIN calendar_assignment_child ac ON ac.assignment_id = a.id
    LEFT JOIN child c ON c.household_id = ac.household_id AND c.id = ac.child_id AND c.archived_at IS NULL
WHERE a.household_id = $1 AND a.series_id = ANY(@series_ids::uuid[])
ORDER BY a.series_id, a.recurrence_key, c.id;

-- name: UpsertAssignment :one
INSERT INTO calendar_assignment (household_id, series_id, recurrence_key) VALUES ($1, $2, $3)
ON CONFLICT (series_id, recurrence_key) DO UPDATE SET recurrence_key = EXCLUDED.recurrence_key
RETURNING id;

-- name: ClearAssignmentChildren :exec
DELETE FROM calendar_assignment_child WHERE assignment_id = $1;

-- name: AddAssignmentChildren :exec
INSERT INTO calendar_assignment_child (household_id, assignment_id, child_id)
SELECT $1, $2, unnest(@child_ids::uuid[]);

-- name: DeleteOccurrenceAssignments :exec
DELETE FROM calendar_assignment WHERE household_id = $1 AND series_id = $2 AND recurrence_key <> '*';

-- name: CountLiveChildren :one
SELECT count(*) FROM child WHERE household_id = $1 AND archived_at IS NULL AND id = ANY(@ids::uuid[]);

-- An unchanged feed keeps a too_many_events flag; any other error is cleared.
-- name: FeedUnchanged :exec
UPDATE calendar_source SET last_success_at = now(), consecutive_failures = 0, next_sync_at = now() + interval '15 minutes',
    error_code = CASE WHEN error_code = 'too_many_events' THEN error_code END,
    http_etag = sqlc.narg(etag), http_last_modified = sqlc.narg(last_modified)
WHERE id = $1;
