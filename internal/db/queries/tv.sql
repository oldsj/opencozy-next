-- The TV gate (slice 9). Every query takes the actor's household.

-- name: ListLiveTvs :many
SELECT * FROM tv WHERE household_id = $1 AND archived_at IS NULL ORDER BY name, id;

-- name: CountLiveTvs :one
SELECT count(*) FROM tv WHERE household_id = $1 AND archived_at IS NULL;

-- name: GetLiveTv :one
SELECT * FROM tv WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: GetLiveTvByEntity :one
SELECT * FROM tv WHERE household_id = $1 AND ha_entity_id = $2 AND archived_at IS NULL;

-- name: ListTvNames :many
-- Every TV, removed ones too, for history.
SELECT id, name FROM tv WHERE household_id = $1;

-- name: ListTvMacs :many
SELECT m.tv_id, m.mac FROM tv_mac m
JOIN tv ON tv.household_id = m.household_id AND tv.id = m.tv_id AND tv.archived_at IS NULL
WHERE m.household_id = $1
ORDER BY m.mac;

-- name: TvMacOwners :many
SELECT m.mac, m.tv_id, tv.name FROM tv_mac m
JOIN tv ON tv.household_id = m.household_id AND tv.id = m.tv_id AND tv.archived_at IS NULL
WHERE m.household_id = $1 AND m.mac = ANY(sqlc.arg(macs)::text[])
ORDER BY m.mac;

-- name: CreateTv :one
INSERT INTO tv (household_id, name, room, ha_entity_id, open_start, open_end, open_days)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateTv :one
UPDATE tv
SET name = $4, room = $5, ha_entity_id = $6, open_start = $7, open_end = $8, open_days = $9, version = version + 1
WHERE household_id = $1 AND id = $2 AND version = $3 AND archived_at IS NULL
RETURNING *;

-- name: ArchiveTv :exec
UPDATE tv SET archived_at = sqlc.arg(now)::timestamptz, version = version + 1
WHERE household_id = $1 AND id = $2 AND archived_at IS NULL;

-- name: DeleteTvMacs :exec
DELETE FROM tv_mac WHERE household_id = $1 AND tv_id = $2;

-- name: AddTvMac :exec
INSERT INTO tv_mac (household_id, tv_id, mac) VALUES ($1, $2, $3);

-- name: SetTvOpenUntil :exec
UPDATE tv SET open_until = $3 WHERE household_id = $1 AND id = $2;

-- name: SetTvPower :exec
-- A new power report. Turning off also forgets the router's attempt run.
UPDATE tv
SET power = sqlc.arg(power)::text, power_changed_at = sqlc.arg(now)::timestamptz,
    attempts_since = CASE WHEN sqlc.arg(power)::text = 'off' THEN NULL ELSE attempts_since END,
    last_attempt_at = CASE WHEN sqlc.arg(power)::text = 'off' THEN NULL ELSE last_attempt_at END
WHERE household_id = $1 AND id = $2;

-- name: ForgetTvPower :execrows
-- The report no longer describes the TV (its entity changed or was cleared): power is unknown
-- again and the attempt run starts over.
UPDATE tv
SET power = 'unknown', power_changed_at = sqlc.arg(now)::timestamptz, attempts_since = NULL, last_attempt_at = NULL
WHERE household_id = $1 AND id = $2 AND power <> 'unknown';

-- name: SetTvAttempts :exec
UPDATE tv SET attempts_since = $3, last_attempt_at = $4 WHERE household_id = $1 AND id = $2;

-- name: ListUnspentTvCredits :many
-- Credits with time left, oldest first per child.
SELECT * FROM tv_credit
WHERE household_id = $1 AND seconds > used_seconds
ORDER BY child_id, created_at, id;

-- name: AddTvCredit :one
INSERT INTO tv_credit (household_id, child_id, redemption_id, granted_seconds, seconds, created_at)
VALUES ($1, $2, $3, sqlc.arg(seconds)::integer, sqlc.arg(seconds)::integer, sqlc.arg(now)::timestamptz)
RETURNING *;

-- name: GetTvCreditByRedemption :one
SELECT * FROM tv_credit WHERE household_id = $1 AND redemption_id = $2;

-- name: SpendTvCredit :exec
UPDATE tv_credit SET used_seconds = $3 WHERE household_id = $1 AND id = $2;

-- name: CancelTvCredit :exec
UPDATE tv_credit SET seconds = used_seconds WHERE household_id = $1 AND id = $2;

-- name: ListOpenTvSessions :many
SELECT * FROM tv_session WHERE household_id = $1 AND ended_at IS NULL ORDER BY started_at, id;

-- name: StartTvSession :one
INSERT INTO tv_session (household_id, tv_id, watcher, child_id, ends_by, started_at, started_by_adult, started_by_device)
VALUES ($1, $2, $3, $4, $5, sqlc.arg(now)::timestamptz, $6, $7)
RETURNING *;

-- name: ChargeTvSession :exec
UPDATE tv_session SET charged_seconds = $3 WHERE household_id = $1 AND id = $2 AND ended_at IS NULL;

-- name: EndTvSession :one
UPDATE tv_session SET ended_at = $3, end_reason = $4, ended_by_adult = $5
WHERE household_id = $1 AND id = $2 AND ended_at IS NULL
RETURNING *;

-- name: RecentTvSessions :many
SELECT * FROM tv_session WHERE household_id = $1 AND ended_at IS NOT NULL
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(max)::integer;

-- name: GetTvSessions :many
SELECT * FROM tv_session WHERE household_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: LastTvSessionEnd :one
SELECT coalesce(max(ended_at), 'epoch')::timestamptz FROM tv_session WHERE household_id = $1 AND tv_id = $2;

-- name: ListPendingTvPrompts :many
SELECT p.* FROM tv_prompt p
JOIN tv ON tv.household_id = p.household_id AND tv.id = p.tv_id AND tv.archived_at IS NULL
WHERE p.household_id = $1 AND p.state = 'pending'
ORDER BY p.created_at, p.id;

-- name: GetTvPrompt :one
SELECT * FROM tv_prompt WHERE household_id = $1 AND id = $2;

-- name: RaiseTvPrompt :one
INSERT INTO tv_prompt (household_id, tv_id, source, created_at) VALUES ($1, $2, $3, sqlc.arg(now)::timestamptz)
RETURNING *;

-- name: RemindTvPrompt :exec
UPDATE tv_prompt SET created_at = sqlc.arg(now)::timestamptz WHERE household_id = $1 AND id = $2 AND state = 'pending';

-- name: ResolveTvPrompt :one
-- Decide a TV's pending prompt, if it has one.
UPDATE tv_prompt
SET state = $3, version = 2, decided_at = sqlc.arg(now)::timestamptz, decided_by_adult = $4, decided_by_device = $5, session_id = $6
WHERE household_id = $1 AND tv_id = $2 AND state = 'pending'
RETURNING *;

-- name: LastTvPromptDismissed :one
SELECT coalesce(max(decided_at), 'epoch')::timestamptz FROM tv_prompt
WHERE household_id = $1 AND tv_id = $2 AND state = 'dismissed';

-- name: RecordTvOpenEvent :exec
INSERT INTO tv_open_event (household_id, tv_id, action, until, at, by_adult, via_credential, ended_session_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListTvOpenEvents :many
SELECT e.*, tv.name AS tv_name, a.name AS by_name
FROM tv_open_event e
JOIN tv ON tv.household_id = e.household_id AND tv.id = e.tv_id
LEFT JOIN adult a ON a.household_id = e.household_id AND a.id = e.by_adult
WHERE e.household_id = $1
ORDER BY e.at DESC, e.id DESC
LIMIT sqlc.arg(max)::integer;

-- name: ChildrenWithDevices :many
-- Live children with at least one paired, unrevoked device of their own.
SELECT DISTINCT child_id::uuid FROM device
WHERE household_id = $1 AND child_id IS NOT NULL AND revoked_at IS NULL;

-- name: ListTvWorkHouseholds :many
-- Households the settlement job looks at: an open session or a pending prompt.
SELECT household_id FROM tv_session WHERE ended_at IS NULL
UNION
SELECT household_id FROM tv_prompt WHERE state = 'pending';
