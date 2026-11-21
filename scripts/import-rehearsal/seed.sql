-- The import rehearsal's own fictional rows, in the old app's schema at d7fead8, applied after the
-- importer's fixture (internal/importer/testdata/old_seed.sql) and on top of its household.
-- Everything here is something the importer carries or deliberately drops today. What it can't
-- map yet is in pending.sql. Fictional data only: example.test, made-up hashes.
SET search_path TO opencozy;

-- A daily chore for Rowan, each day approved and paid, for volume and timing: :days days (DAYS in
-- the script, a year by default) up to 2026-03-08. Ids are derived from the day, so every run seeds
-- the same rows.
INSERT INTO chore_definition (household_id, id, assigned_child_id, title, points, schedule_kind, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000101', '0190a000-0000-7000-8000-0000000000c2', 'Set the table', 1, 'daily', 1, '2016-01-01T08:00:00Z');

CREATE TEMP TABLE rehearsal_day AS
SELECT d::date AS day,
       md5('rehearsal-occurrence-' || d::date)::uuid AS occurrence_id,
       md5('rehearsal-ledger-' || d::date)::uuid AS ledger_id,
       md5('rehearsal-command-' || d::date)::uuid AS command_id,
       md5('rehearsal-approval-' || d::date)::uuid AS approval_id,
       md5('rehearsal-request-' || d::date)::uuid AS request_id,
       d::date + time '17:00' AT TIME ZONE 'Europe/Amsterdam' AS asked_at
FROM generate_series(date '2026-03-08' - (:days - 1), date '2026-03-08', interval '1 day') AS d;

INSERT INTO chore_occurrence (household_id, id, chore_id, assigned_child_id, local_day, time_zone_snapshot, title_snapshot, points_snapshot, schedule_kind_snapshot, definition_version, version, created_at)
SELECT '0190a000-0000-7000-8000-00000000000a', occurrence_id, '0190b000-0000-7000-8000-000000000101', '0190a000-0000-7000-8000-0000000000c2',
       day, 'Europe/Amsterdam', 'Set the table', 1, 'daily', 1, 2, asked_at - interval '10 hours'
FROM rehearsal_day;

INSERT INTO point_ledger (household_id, id, child_id, occurrence_id, occurrence_version, command_id, delta, description_snapshot, created_at)
SELECT '0190a000-0000-7000-8000-00000000000a', ledger_id, '0190a000-0000-7000-8000-0000000000c2', occurrence_id, 2, command_id, 1, 'Set the table', asked_at + interval '1 hour'
FROM rehearsal_day;

INSERT INTO chore_active_award (household_id, occurrence_id, ledger_id)
SELECT '0190a000-0000-7000-8000-00000000000a', occurrence_id, ledger_id FROM rehearsal_day;

INSERT INTO completion_approval (household_id, id, child_id, chore_occurrence_id, title_snapshot, points_snapshot, local_day, state, version, request_command_id, requested_at, decided_at, decided_by, ledger_id)
SELECT '0190a000-0000-7000-8000-00000000000a', approval_id, '0190a000-0000-7000-8000-0000000000c2', occurrence_id, 'Set the table', 1, day,
       'approved', 2, request_id, asked_at, asked_at + interval '1 hour', 'fixtureOwnerUser01', ledger_id
FROM rehearsal_day;

-- Not carried, on purpose: an open parent web session, an open phone session with a push token,
-- a kid web session, and a "one more minute" event.
INSERT INTO auth_session (id, user_id, token, expires_at, ip_address, user_agent, created_at, updated_at) VALUES
  ('rehearsalSession01', 'fixtureOwnerUser01', 'rehearsal-session-token', '2026-04-01T08:00:00Z', '192.0.2.10', 'Rehearsal browser', '2026-03-12T08:00:00Z', '2026-03-12T08:00:00Z');
INSERT INTO parent_session (id, household_id, user_id, name, credential_hash, created_at, expires_at, last_used_at, push_token, push_environment, push_registered_at) VALUES
  ('0190b000-0000-7000-8000-000000000201', '0190a000-0000-7000-8000-00000000000a', 'fixtureOwnerUser01', 'Alex''s iPhone', repeat('1', 64),
   '2026-03-01T08:00:00Z', '2026-05-01T08:00:00Z', '2026-03-12T08:00:00Z', repeat('cd', 32), 'production', '2026-03-01T08:01:00Z');
INSERT INTO device_web_session (household_id, id, device_id, child_id, exchange_hash, created_at, exchange_expires_at, exchanged_at, credential_hash, expires_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000202', '0190a000-0000-7000-8000-0000000000e1', '0190a000-0000-7000-8000-0000000000c1', repeat('2', 64),
   '2026-03-12T07:00:00Z', '2026-03-12T07:00:30Z', '2026-03-12T07:00:05Z', repeat('3', 64), '2026-03-13T07:00:00Z');
INSERT INTO screen_time_event (household_id, id, child_id, device_id, kind, occurred_at, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000203', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-0000000000e1', 'one_more_minute', '2026-03-12T16:00:00Z', '2026-03-12T16:00:01Z');

-- Banked iPad time (carried as one balance per child; the fixture has Avery's): a parent gave
-- Rowan 30 minutes, 10 of them spent in one window, so 1,200 seconds are banked. An adult declined
-- one of Avery's requests (answered requests aren't carried; the fixture has a waiting one).
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, fulfilled_at, screen_time_minutes_snapshot, screen_time_target_snapshot) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000331', NULL, '0190a000-0000-7000-8000-0000000000c2', 'Extra time', 0, NULL, 1, 'fulfilled',
   '2026-03-13T09:00:00Z', '2026-03-13T09:00:00Z', '2026-03-13T09:00:00Z', 30, 'personal');
INSERT INTO personal_credit (household_id, redemption_id, child_id, minutes, seconds, used_seconds, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000331', '0190a000-0000-7000-8000-0000000000c2', 30, 1800, 600, '2026-03-13T09:00:00Z');
INSERT INTO screen_time_grant (household_id, id, redemption_id, child_id, target, minutes, starts_at, ends_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000332', '0190b000-0000-7000-8000-000000000331', '0190a000-0000-7000-8000-0000000000c2', 'personal', 10,
   '2026-03-13T15:00:00Z', '2026-03-13T15:10:00Z');
INSERT INTO time_request (household_id, id, child_id, device_id, state, version, created_at, decided_at, decided_by, choice) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000322', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-0000000000e1', 'declined', 2,
   '2026-03-11T17:00:00Z', '2026-03-11T17:05:00Z', 'fixtureOwnerUser01', 'no');
