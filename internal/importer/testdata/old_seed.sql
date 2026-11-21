-- Fictional rows for the importer's fixture tests, in the old app's schema (old_schema.sql).
-- Names come from the old app's fictional dev seed; addresses use example.test. The credential
-- secrets and the password were made for this fixture and are in importer_test.go.
--
-- IDs are 0190a000-0000-7000-8000-000000000NNN. To keep branches from colliding, each group adds
-- new rows only in its own range (rows already here keep their IDs):
--   0000xx  households, children, adults, credentials (the base)
--   0001xx  the ledger (#59) and chores, routines, approvals (#65)
--   0002xx  calendars and Google connections (#60)
--   0003xx  rewards and redemptions (#68)
--   0004xx  child devices and passkeys (#70)
--   0005xx  banked iPad time, grants, requests (#75)
--   0006xx  banked TV time (#78)
--   0007xx  phone push (slice 11)
--   0009xx  TVs (#56)
SET search_path TO opencozy;

INSERT INTO household (id, name, time_zone, created_at, archived_at, lock_message, game_portal_domains, game_domains)
VALUES ('0190a000-0000-7000-8000-00000000000a', 'Example Household', 'Europe/Amsterdam', '2026-01-02T08:00:00Z', NULL, NULL, '{games.example.test}', '{blocks.example.test,puzzles.example.test}');

-- The owner signs in with a password; the second adult only with OIDC; the third account never
-- joined a household.
INSERT INTO auth_user (id, name, email, email_verified, image, created_at, updated_at) VALUES
  ('fixtureOwnerUser01', 'Alex Example', 'owner@example.test', true, NULL, '2026-01-02T08:00:00Z', '2026-01-02T08:00:00Z'),
  ('fixtureSecondUser2', 'Sam Example', 'sam@example.test', true, NULL, '2026-01-03T08:00:00Z', '2026-01-03T08:00:00Z'),
  ('fixtureNoHousehold', 'Pat Example', 'pat@example.test', false, NULL, '2026-01-04T08:00:00Z', '2026-01-04T08:00:00Z');

INSERT INTO auth_account (id, user_id, account_id, provider_id, access_token, refresh_token, id_token, access_token_expires_at, refresh_token_expires_at, scope, password, created_at, updated_at) VALUES
  ('fixtureAccount0001', 'fixtureOwnerUser01', 'fixtureOwnerUser01', 'credential', NULL, NULL, NULL, NULL, NULL, NULL, '933879335a7fc39b4d252873a7b57531:fbbbba97b965bc72ddc87f02f5e813c9ff3845e8108b54708467ac11203d95b056892c06d9d3540db1f04e7a09340c57f106f200fa2246a063797b68764c8df1', '2026-01-02T08:00:00Z', '2026-01-02T08:00:00Z'),
  ('fixtureAccount0002', 'fixtureSecondUser2', 'fixture-oidc-subject', 'oidc', 'fixture-access-token', 'fixture-refresh-token', NULL, NULL, NULL, 'openid email', NULL, '2026-01-03T08:00:00Z', '2026-01-03T08:00:00Z');

INSERT INTO household_owner (user_id, household_id, created_at) VALUES
  ('fixtureOwnerUser01', '0190a000-0000-7000-8000-00000000000a', '2026-01-02T08:00:00Z'),
  ('fixtureSecondUser2', '0190a000-0000-7000-8000-00000000000a', '2026-01-03T08:00:00Z');

INSERT INTO child (household_id, id, name, color, created_at, archived_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000c1', 'Avery', 'teal', '2026-01-02T09:00:00Z', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000c2', 'Rowan', 'sky', '2026-01-02T09:01:00Z', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000c3', 'Mika', 'sage', '2026-01-02T09:02:00Z', '2026-03-01T09:00:00Z');
-- Avery may start screen time 15:00-19:00 on school nights, 09:00-20:00 at weekends.
UPDATE child SET school_night_start = 900, school_night_end = 1140, weekend_start = 540, weekend_end = 1200
WHERE id = '0190a000-0000-7000-8000-0000000000c1';
-- Rowan's rule is half set (the old check let that through): carried as none.
UPDATE child SET school_night_start = 900 WHERE id = '0190a000-0000-7000-8000-0000000000c2';

-- Two service credentials whose scopes all have a new name (the router's tv:gate since slice 9),
-- and an assistant whose scope waits on a later slice.
INSERT INTO service_credential (household_id, id, name, credential_hash, scopes, created_at, revoked_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d1', 'Kitchen tablet', '93e227675412e705e32188b1bc512d6395f09714bb812c15c227138d4698e694', '{children:read}', '2026-01-05T08:00:00Z', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d2', 'Router', '9371ba2ad122acad9ca87e0137ee9e81012832460f2e29443c413f311a29a105', '{tv:gate}', '2026-01-05T08:01:00Z', NULL);
INSERT INTO service_credential_child (household_id, credential_id, child_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d1', '0190a000-0000-7000-8000-0000000000c1'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d2', '0190a000-0000-7000-8000-0000000000c2');
INSERT INTO assistant_credential (household_id, id, user_id, name, credential_hash, scopes, created_at, last_used_at, revoked_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000e1', 'fixtureOwnerUser01', 'Desktop assistant', '47d9a070fe7da7e7e22399db85bd24b62aff8560c86afdeca7957c4ba107c617', '{approvals:read}', '2026-01-06T08:00:00Z', NULL, NULL);

-- Slice 6: a learning app's credential and its rule (one live rule, one archived), and links
-- (one unlinked).
INSERT INTO service_credential (household_id, id, name, credential_hash, scopes, created_at, revoked_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d3', 'Example Learning App', 'a2e1420d7c78ceb4137c1e5c2dd2941ae130982e1e9594ce95ed281aa712db54', '{external:award}', '2026-01-05T08:02:00Z', NULL);
INSERT INTO service_credential_child (household_id, credential_id, child_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d3', '0190a000-0000-7000-8000-0000000000c1');
INSERT INTO award_rule (household_id, id, activity_key, points, daily_limit, version, archived_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000a1', 'build_house', 5, 20, 2, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000a2', 'read_book', 3, 9, 1, '2026-02-01T08:00:00Z');
INSERT INTO service_credential_award_rule (household_id, credential_id, rule_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000d3', '0190a000-0000-7000-8000-0000000000a1');
INSERT INTO external_identity_link (household_id, child_id, provider, external_id, linked_at, unlinked_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000c1', 'minecraft', 'Builder_42', '2026-01-06T08:00:00Z', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000c2', 'minecraft', 'Old_Miner', '2026-01-06T08:00:00Z', '2026-01-20T08:00:00Z');

-- Slice 14: recipes (one archived) and a meal plan that uses them.
INSERT INTO recipe (household_id, id, title, servings, ingredients, instructions, version, created_at, archived_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000f1', 'Lentil soup', 4,
   ARRAY['1 cup red lentils', '1 onion', '4 cups stock'], ARRAY['Soften the onion.', E'Add the lentils and stock.\nSimmer for 20 minutes.'], 2,
   '2026-01-05 18:00:00+00', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000f2', 'Pancakes', 2,
   ARRAY['1 cup flour', '1 egg', '1 cup milk'], ARRAY['Whisk everything.', 'Cook in a hot pan.'], 1,
   '2026-01-06 08:00:00+00', '2026-02-01 08:00:00+00');
INSERT INTO meal_plan_entry (household_id, id, local_day, meal_type, recipe_id, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000f3', '2026-01-12', 'dinner', '0190a000-0000-7000-8000-0000000000f1', 1, '2026-01-10 12:00:00+00'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000f4', '2026-01-13', 'breakfast', '0190a000-0000-7000-8000-0000000000f2', 3, '2026-01-10 12:00:00+00');

-- Slice 13a: a calendar link (its token is fictional), a recurring series and one the old app
-- had removed, an event (rebuilt, not carried), and assignments: the series is for one child, one
-- occurrence is for nobody, and a cleared one the old app ignored.
INSERT INTO calendar_source (household_id, id, provider, name, feed_url, show_on_display, version, created_at, last_success_at, feed_hash) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b1', 'ics', 'School', 'webcal://calendar.example.test/school.ics?token=fixture-feed-token', false, 2, '2026-01-07T08:00:00Z', '2026-01-08T08:00:00Z', repeat('a', 64));
INSERT INTO calendar_series (household_id, id, source_id, provider_uid, summary, all_day, recurring, status, content_hash, removed_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b2', '0190a000-0000-7000-8000-0000000000b1', 'swim@example.test', 'Swimming lesson', false, true, 'confirmed', repeat('b', 64), NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b3', '0190a000-0000-7000-8000-0000000000b1', 'trip@example.test', 'Class trip', true, false, 'confirmed', repeat('c', 64), '2026-01-09T08:00:00Z');
INSERT INTO calendar_occurrence (household_id, id, source_id, series_id, recurrence_key, all_day, start_date, end_date, summary, status, is_override, content_hash) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b4', '0190a000-0000-7000-8000-0000000000b1', '0190a000-0000-7000-8000-0000000000b3', 'single', true, '2026-01-09', '2026-01-10', 'Class trip', 'confirmed', false, repeat('d', 64));
INSERT INTO calendar_assignment (household_id, id, series_id, occurrence_key, version, active, cleared_from_version) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b5', '0190a000-0000-7000-8000-0000000000b2', NULL, 1, true, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b6', '0190a000-0000-7000-8000-0000000000b2', '20260114T163000Z', 1, true, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b7', '0190a000-0000-7000-8000-0000000000b2', '20260121T163000Z', 2, false, 1);
INSERT INTO calendar_assignment_person (household_id, assignment_id, child_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b5', '0190a000-0000-7000-8000-0000000000c1'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b7', '0190a000-0000-7000-8000-0000000000c2');

-- Slice 0b: the points ledger and what it points at (chores, routines, rewards and learning
-- awards, which later slices carry). Avery: a chore paid on approval; a chore paid, undone and
-- paid again; a reward spent, another spent and refunded; two learning awards, one undone: 11
-- points. Rowan: a routine paid, another paid and undone, a reward waiting: 0 points (3 after
-- import: a reward still waiting for an answer is not carried, so its points come back).
INSERT INTO chore_definition (household_id, id, assigned_child_id, title, points, schedule_kind, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000101', '0190a000-0000-7000-8000-0000000000c1', 'Feed the cat', 5, 'daily', 1, '2026-03-01T08:00:00Z');
INSERT INTO chore_occurrence (household_id, id, chore_id, assigned_child_id, local_day, time_zone_snapshot, title_snapshot, points_snapshot, schedule_kind_snapshot, definition_version, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000111', '0190a000-0000-7000-8000-000000000101', '0190a000-0000-7000-8000-0000000000c1', '2026-03-09', 'Europe/Amsterdam', 'Feed the cat', 5, 'daily', 1, 2, '2026-03-09T06:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000112', '0190a000-0000-7000-8000-000000000101', '0190a000-0000-7000-8000-0000000000c1', '2026-03-10', 'Europe/Amsterdam', 'Feed the cat', 5, 'daily', 1, 4, '2026-03-10T06:00:00Z');
INSERT INTO routine_definition (household_id, id, assigned_child_id, title, points, steps, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000102', '0190a000-0000-7000-8000-0000000000c2', 'Morning routine', 3,
   '[{"id": "0190a000-0000-7000-8000-000000000105", "title": "Brush teeth"}, {"id": "0190a000-0000-7000-8000-000000000106", "title": "Get dressed"}]', 1, '2026-03-01T08:00:00Z');
INSERT INTO routine_occurrence (household_id, id, routine_id, assigned_child_id, local_day, time_zone_snapshot, title_snapshot, points_snapshot, steps_snapshot, checked_step_ids, definition_version, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000121', '0190a000-0000-7000-8000-000000000102', '0190a000-0000-7000-8000-0000000000c2', '2026-03-09', 'Europe/Amsterdam', 'Morning routine', 3,
   '[{"id": "0190a000-0000-7000-8000-000000000105", "title": "Brush teeth"}, {"id": "0190a000-0000-7000-8000-000000000106", "title": "Get dressed"}]',
   ARRAY['0190a000-0000-7000-8000-000000000105', '0190a000-0000-7000-8000-000000000106']::uuid[], 1, 3, '2026-03-09T05:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000122', '0190a000-0000-7000-8000-000000000102', '0190a000-0000-7000-8000-0000000000c2', '2026-03-10', 'Europe/Amsterdam', 'Morning routine', 3,
   '[{"id": "0190a000-0000-7000-8000-000000000105", "title": "Brush teeth"}, {"id": "0190a000-0000-7000-8000-000000000106", "title": "Get dressed"}]',
   ARRAY['0190a000-0000-7000-8000-000000000105']::uuid[], 1, 4, '2026-03-10T05:00:00Z');
INSERT INTO reward (household_id, id, name, cost, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000103', 'Ice cream', 4, 1, '2026-03-01T08:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000104', 'Sticker', 3, 1, '2026-03-01T08:00:00Z');
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, fulfilled_at, refunded_points) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000131', '0190a000-0000-7000-8000-000000000103', '0190a000-0000-7000-8000-0000000000c1', 'Ice cream', 4, 1, 2, 'fulfilled', '2026-03-11T09:00:00Z', '2026-03-11T09:30:00Z', '2026-03-11T09:30:00Z', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000132', '0190a000-0000-7000-8000-000000000103', '0190a000-0000-7000-8000-0000000000c1', 'Ice cream', 4, 1, 2, 'canceled', '2026-03-11T10:00:00Z', '2026-03-11T11:00:00Z', NULL, 4),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000133', '0190a000-0000-7000-8000-000000000104', '0190a000-0000-7000-8000-0000000000c2', 'Sticker', 3, 1, 1, 'pending', '2026-03-11T12:00:00Z', NULL, NULL, NULL);

BEGIN;
INSERT INTO point_ledger (household_id, id, child_id, occurrence_id, occurrence_version, redemption_id, reward_entry_type, routine_occurrence_id, routine_entry_type, external_award_id, external_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000141', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000111', 2, NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000161', 5, 'Feed the cat', '2026-03-09T18:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000142', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000112', 2, NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000162', 5, 'Feed the cat', '2026-03-10T17:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000143', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000112', 3, NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000163', -5, 'Feed the cat', '2026-03-10T17:05:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000144', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000112', 4, NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000164', 5, 'Feed the cat', '2026-03-10T17:10:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000145', '0190a000-0000-7000-8000-0000000000c2', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000121', 'award', NULL, NULL, '0190a000-0000-7000-8000-000000000165', 3, 'Morning routine', '2026-03-09T07:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000146', '0190a000-0000-7000-8000-0000000000c2', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000122', 'award', NULL, NULL, '0190a000-0000-7000-8000-000000000166', 3, 'Morning routine', '2026-03-10T07:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000147', '0190a000-0000-7000-8000-0000000000c2', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000122', 'reversal', NULL, NULL, '0190a000-0000-7000-8000-000000000167', -3, 'Morning routine', '2026-03-10T07:30:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000148', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, '0190a000-0000-7000-8000-000000000131', 'spend', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000168', -4, 'Ice cream', '2026-03-11T09:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000149', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, '0190a000-0000-7000-8000-000000000132', 'spend', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000169', -4, 'Ice cream', '2026-03-11T10:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014a', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, '0190a000-0000-7000-8000-000000000132', 'refund', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-00000000016a', 4, 'Ice cream', '2026-03-11T11:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014b', '0190a000-0000-7000-8000-0000000000c2', NULL, NULL, '0190a000-0000-7000-8000-000000000133', 'spend', NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-00000000016b', -3, 'Sticker', '2026-03-11T12:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014c', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000151', 'award', '0190a000-0000-7000-8000-000000000171', 5, 'build_house', '2026-03-12T22:30:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014d', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000152', 'award', '0190a000-0000-7000-8000-000000000172', 5, 'build_house', '2026-03-13T08:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014e', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, NULL, NULL, NULL, NULL, '0190a000-0000-7000-8000-000000000152', 'reversal', '0190a000-0000-7000-8000-000000000173', -5, 'build_house', '2026-03-13T09:00:00Z');
-- The first learning award is at 23:30 in Amsterdam: household day 2026-03-12.
INSERT INTO external_award (household_id, id, source_event_id, child_id, rule_id, rule_snapshot, ledger_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000151', '0190a000-0000-7000-8000-000000000171', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-0000000000a1',
   '{"id": "0190a000-0000-7000-8000-0000000000a1", "activityKey": "build_house", "points": 5, "dailyLimit": 20, "version": 2}', '0190a000-0000-7000-8000-00000000014c'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000152', '0190a000-0000-7000-8000-000000000172', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-0000000000a1',
   '{"id": "0190a000-0000-7000-8000-0000000000a1", "activityKey": "build_house", "points": 5, "dailyLimit": 20, "version": 2}', '0190a000-0000-7000-8000-00000000014d');
COMMIT;
INSERT INTO chore_award_reversal (household_id, award_id, reversal_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000142', '0190a000-0000-7000-8000-000000000143');
INSERT INTO chore_active_award (household_id, occurrence_id, ledger_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000111', '0190a000-0000-7000-8000-000000000141'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000112', '0190a000-0000-7000-8000-000000000144');
INSERT INTO completion_approval (household_id, id, child_id, chore_occurrence_id, title_snapshot, points_snapshot, local_day, state, version, request_command_id, requested_at, decided_at, decided_by, ledger_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000181', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000111', 'Feed the cat', 5, '2026-03-09', 'approved', 2, '0190a000-0000-7000-8000-000000000182', '2026-03-09T17:00:00Z', '2026-03-09T18:00:00Z', 'fixtureOwnerUser01', '0190a000-0000-7000-8000-000000000141');

-- Slices 2/3: more chores and approvals. Rowan waters the plants on Mondays, Wednesdays and
-- Sundays, and is waiting for an answer on Wednesday's (an earlier ask was taken back). Avery
-- has a one-off chore never done. Rowan's Tuesday routine, undone, was then sent back.
INSERT INTO chore_definition (household_id, id, assigned_child_id, title, points, schedule_kind, scheduled_day, weekdays, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000107', '0190a000-0000-7000-8000-0000000000c2', 'Water the plants', 2, 'weekdays', NULL, '{1,3,7}', 3, '2026-03-02T08:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000108', '0190a000-0000-7000-8000-0000000000c1', 'Return library books', 4, 'once', '2026-03-20', NULL, 1, '2026-03-02T08:00:00Z');
INSERT INTO chore_occurrence (household_id, id, chore_id, assigned_child_id, local_day, time_zone_snapshot, title_snapshot, points_snapshot, schedule_kind_snapshot, weekdays_snapshot, definition_version, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000113', '0190a000-0000-7000-8000-000000000107', '0190a000-0000-7000-8000-0000000000c2', '2026-03-11', 'Europe/Amsterdam', 'Water the plants', 2, 'weekdays', '{1,3,7}', 3, 3, '2026-03-11T06:00:00Z');
INSERT INTO completion_approval (household_id, id, child_id, chore_occurrence_id, routine_occurrence_id, title_snapshot, points_snapshot, local_day, state, version, request_command_id, requested_at, decided_at, decided_by, reason) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000184', '0190a000-0000-7000-8000-0000000000c2', '0190a000-0000-7000-8000-000000000113', NULL, 'Water the plants', 2, '2026-03-11', 'withdrawn', 2, '0190a000-0000-7000-8000-000000000186', '2026-03-11T15:00:00Z', '2026-03-11T15:01:00Z', NULL, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000183', '0190a000-0000-7000-8000-0000000000c2', '0190a000-0000-7000-8000-000000000113', NULL, 'Water the plants', 2, '2026-03-11', 'pending', 1, '0190a000-0000-7000-8000-000000000187', '2026-03-11T16:00:00Z', NULL, NULL, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000185', '0190a000-0000-7000-8000-0000000000c2', NULL, '0190a000-0000-7000-8000-000000000122', 'Morning routine', 3, '2026-03-10', 'rejected', 2, '0190a000-0000-7000-8000-000000000188', '2026-03-10T08:00:00Z', '2026-03-10T08:10:00Z', 'fixtureSecondUser2', 'Bed not made');

-- Slice 5: more rewards. iPad time (30 minutes) Avery bought and an adult cancelled with all 2
-- points back; TV time, archived; a reward whose name and cost are over the new input limits;
-- and a parent's grant of 20 minutes (a redemption with no reward, no points).
INSERT INTO reward (household_id, id, name, cost, version, created_at, archived_at, screen_time_minutes, screen_time_target) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000109', 'iPad time', 2, 2, '2026-03-02T08:00:00Z', NULL, 30, 'personal'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000010a', 'TV time', 5, 1, '2026-03-02T08:00:00Z', '2026-03-05T08:00:00Z', 20, 'tv'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000010b', 'A trip to the big playground by the river, with a picnic and the long way home through the woods', 250000, 1, '2026-03-02T08:00:00Z', NULL, NULL, NULL);
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, fulfilled_at, refunded_points, screen_time_minutes_snapshot, screen_time_target_snapshot) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000136', '0190a000-0000-7000-8000-000000000109', '0190a000-0000-7000-8000-0000000000c1', 'iPad time', 2, 2, 3, 'canceled', '2026-03-12T08:00:00Z', '2026-03-12T08:30:00Z', '2026-03-12T08:00:00Z', 2, 30, 'personal'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000137', NULL, '0190a000-0000-7000-8000-0000000000c1', 'Extra time', 0, NULL, 1, 'fulfilled', '2026-03-12T09:00:00Z', '2026-03-12T09:00:00Z', '2026-03-12T09:00:00Z', NULL, 20, 'personal');
INSERT INTO point_ledger (household_id, id, child_id, redemption_id, reward_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000001a1', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000136', 'spend', '0190a000-0000-7000-8000-0000000001a3', -2, 'iPad time', '2026-03-12T08:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000001a2', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000136', 'refund', '0190a000-0000-7000-8000-0000000001a4', 2, 'iPad time', '2026-03-12T08:30:00Z');

-- Slice 13b (IDs ...0002xx): two Google accounts (fictional; the token opens with oldsecret's fixture secret), one
-- connected and one the old app already needed connected again. A calendar of each, a recurring
-- series on the first with an assignment for one child, and a calendar whose account is gone.
INSERT INTO calendar_google_connection (household_id, id, google_subject, account_email, status, scope, refresh_token_encrypted, access_token_encrypted, access_token_expires_at, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000201', '1001', 'Parent@Example.test', 'connected', 'openid email https://www.googleapis.com/auth/calendar.readonly',
   '872dc466ff84ba59ca64297f:b995293500d7e16c130cef443823d573:d5582b7fa0096d63824808adf3a34e0422fe027ae99fda0f6574e28a', 'fixture-access-token-ciphertext', '2026-01-08T09:00:00Z', '2026-01-07T09:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000202', '1002', 'other@example.test', 'needs_reauth', 'openid email https://www.googleapis.com/auth/calendar.readonly',
   'not-a-ciphertext', NULL, NULL, '2026-01-07T10:00:00Z');
INSERT INTO calendar_source (household_id, id, provider, name, show_on_display, version, created_at, google_connection_id, google_calendar_id, google_sync_token) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000211', 'google', 'Family', true, 3, '2026-01-07T09:05:00Z', '0190a000-0000-7000-8000-000000000201', 'family@group.calendar.example.test', 'fixture-sync-token'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000212', 'google', 'Work', false, 1, '2026-01-07T10:05:00Z', '0190a000-0000-7000-8000-000000000202', 'other@example.test', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000213', 'google', 'Gone', true, 1, '2026-01-07T11:05:00Z', '0190a000-0000-7000-8000-000000000299', 'gone@example.test', NULL);
INSERT INTO calendar_series (household_id, id, source_id, provider_uid, summary, all_day, recurring, status, content_hash, removed_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000221', '0190a000-0000-7000-8000-000000000211', 'fixtureevent0001', 'Piano', false, true, 'confirmed', repeat('e', 64), NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000222', '0190a000-0000-7000-8000-000000000213', 'fixtureevent0002', 'Gone', false, false, 'confirmed', repeat('f', 64), NULL);
INSERT INTO calendar_assignment (household_id, id, series_id, occurrence_key, version, active, cleared_from_version) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000231', '0190a000-0000-7000-8000-000000000221', NULL, 1, true, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000232', '0190a000-0000-7000-8000-000000000222', NULL, 1, true, NULL);
INSERT INTO calendar_assignment_person (household_id, assignment_id, child_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000231', '0190a000-0000-7000-8000-0000000000c2'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000232', '0190a000-0000-7000-8000-0000000000c1');

-- Slices 7 and 10a: Avery's iPad (paired, with a push token) and an old phone, revoked; the
-- owner's passkey.
INSERT INTO device (household_id, id, child_id, name, platform, public_key, paired_at, last_seen_at, revoked_at, push_token, push_environment, push_registered_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000e1', '0190a000-0000-7000-8000-0000000000c1', 'Avery''s iPad', 'ios', 'BAKbIfov6bjJE2K/0m8q8Ww+KaMnpkx31JcSaw+kLHv2uEH5EuIMK+Jdfi5cfDcJqgP7RHR0J+hZwVIDaIP4jTY=', '2026-02-01T10:00:00Z', '2026-03-12T07:00:00Z', NULL, 'abababababababababababababababababababababababababababababababab', 'production', '2026-02-01T10:01:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000e2', NULL, 'Old phone', 'android', 'BFFPwcZ97M1Hb43Y7YMTTQyNVZ1QJJOB1/ccjXhV7j+oIUMq5MSdLKL6nZ4xkEbTtk9jrRMsxhnRiH35+r7Ihgo=', '2026-01-10T10:00:00Z', NULL, '2026-02-01T09:00:00Z', NULL, NULL, NULL);
INSERT INTO parent_passkey (id, user_id, name, credential_id, public_key, counter, transports, backed_up, created_at, last_used_at) VALUES
  ('0190a000-0000-7000-8000-0000000000f1', 'fixtureOwnerUser01', 'Laptop', 'noRvXHIPIyi0Tl69DglRRGS2caWAs59BXOBb8vVIChI', 'eOV92IEGuIP6vrIPDqEFaA7sRGHMErD6lefA4WIzSOoXyU7EGswKFhDUzE2MjTgbZ9amBuo3X86tgZgJWA-_qGPY70krADB7YKfSDaU', 7, ARRAY['internal', 'hybrid'], true, '2026-02-02T10:00:00Z', '2026-03-10T08:00:00Z');

-- Slice 9: a TV with open hours and two MACs, a removed TV, two ended sessions (a child's
-- started from her device, a parents one a parent ended by opening the TV), the opening and
-- its close, an answered prompt (not carried), and TV time from a reward (waits for the
-- rewards import).
INSERT INTO gated_tv (household_id, id, name, created_at, archived_at, room, ha_entity_id, power, power_changed_at, attempts_since, last_attempt_at, open_start, open_end, open_until, open_days) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009a1', 'Den TV', '2026-01-10T08:00:00Z', NULL, 'Den', 'media_player.den_tv', 'off', '2026-01-11T20:00:00Z', NULL, NULL, 1260, 420, NULL, 62),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009a2', 'Old TV', '2026-01-10T08:01:00Z', '2026-01-12T08:00:00Z', NULL, NULL, 'unknown', NULL, '2026-01-11T09:00:00Z', '2026-01-11T09:02:00Z', NULL, NULL, NULL, NULL);
INSERT INTO gated_tv_mac (household_id, tv_id, mac) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009a1', '02:00:5e:10:00:01'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009a1', '02:00:5e:10:00:02');
INSERT INTO tv_session (household_id, id, tv_id, watcher, child_id, charged_seconds, started_at, ended_at, end_reason, started_by, ended_by, ends_by, warned_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009b1', '0190a000-0000-7000-8000-0000000009a1', 'child', '0190a000-0000-7000-8000-0000000000c1', 600, '2026-01-11T16:00:00Z', '2026-01-11T16:10:00Z', 'time_up', 'device:0190a000-0000-7000-8000-0000000009e1', NULL, '2026-01-11T18:00:00Z', '2026-01-11T16:05:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009b2', '0190a000-0000-7000-8000-0000000009a1', 'parents', NULL, 0, '2026-01-11T19:00:00Z', '2026-01-11T19:30:00Z', 'parent_override', 'fixtureOwnerUser01', 'fixtureOwnerUser01', NULL, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009b3', '0190a000-0000-7000-8000-0000000009a1', 'child', '0190a000-0000-7000-8000-0000000000c1', 120, '2026-01-12T16:00:00Z', NULL, NULL, 'device:0190a000-0000-7000-8000-0000000009e1', NULL, '2026-01-12T18:00:00Z', NULL);
INSERT INTO tv_open_event (household_id, id, tv_id, action, until, at, by, assistant_credential_id, ended_session_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009c1', '0190a000-0000-7000-8000-0000000009a1', 'open', '2026-01-11T21:30:00Z', '2026-01-11T19:30:00Z', 'fixtureOwnerUser01', NULL, '0190a000-0000-7000-8000-0000000009b2'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009c2', '0190a000-0000-7000-8000-0000000009a1', 'close', NULL, '2026-01-11T20:00:00Z', 'fixtureOwnerUser01', NULL, NULL);
INSERT INTO tv_prompt (household_id, id, tv_id, source, state, version, created_at, decided_at, decided_by, session_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009d1', '0190a000-0000-7000-8000-0000000009a1', 'home_assistant', 'answered', 2, '2026-01-11T18:59:00Z', '2026-01-11T19:00:00Z', 'fixtureOwnerUser01', '0190a000-0000-7000-8000-0000000009b2');
INSERT INTO reward (household_id, id, name, cost, version, created_at, archived_at, screen_time_minutes, screen_time_target) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009f1', 'TV time', 10, 1, '2026-01-10T08:00:00Z', NULL, 30, 'tv');
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, screen_time_minutes_snapshot, screen_time_target_snapshot, fulfilled_at, refunded_points, device_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009f2', '0190a000-0000-7000-8000-0000000009f1', '0190a000-0000-7000-8000-0000000000c1', 'TV time', 10, 1, 2, 'fulfilled', '2026-01-11T15:00:00Z', '2026-01-11T15:01:00Z', 30, 'tv', '2026-01-11T15:01:00Z', NULL, NULL);
-- The TV time cost Avery 10 points, earned the same day feeding the cat (so her balance is as
-- above: the old app always wrote a spend for a reward).
INSERT INTO chore_occurrence (household_id, id, chore_id, assigned_child_id, local_day, time_zone_snapshot, title_snapshot, points_snapshot, schedule_kind_snapshot, definition_version, version, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000115', '0190a000-0000-7000-8000-000000000101', '0190a000-0000-7000-8000-0000000000c1', '2026-01-11', 'Europe/Amsterdam', 'Feed the cat', 10, 'daily', 1, 2, '2026-01-11T06:00:00Z');
INSERT INTO point_ledger (household_id, id, child_id, occurrence_id, occurrence_version, redemption_id, reward_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000001d1', '0190a000-0000-7000-8000-0000000000c1', '0190a000-0000-7000-8000-000000000115', 2, NULL, NULL, '0190a000-0000-7000-8000-0000000001d3', 10, 'Feed the cat', '2026-01-11T14:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000001d2', '0190a000-0000-7000-8000-0000000000c1', NULL, NULL, '0190a000-0000-7000-8000-0000000009f2', 'spend', '0190a000-0000-7000-8000-0000000001d4', -10, 'TV time', '2026-01-11T15:00:00Z');
INSERT INTO chore_active_award (household_id, occurrence_id, ledger_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000115', '0190a000-0000-7000-8000-0000000001d1');
INSERT INTO tv_credit (household_id, redemption_id, child_id, target, minutes, seconds, used_seconds, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000009f2', '0190a000-0000-7000-8000-0000000000c1', 'tv', 30, 1800, 600, '2026-01-11T15:01:00Z');
