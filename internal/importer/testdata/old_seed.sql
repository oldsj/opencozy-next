-- Fictional rows for the importer's fixture tests, in the old app's schema (old_schema.sql).
-- Names come from the old app's fictional dev seed; addresses use example.test. The credential
-- secrets and the password were made for this fixture and are in importer_test.go.
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

-- One service credential whose scopes all have a new name, one that waits on a later slice
-- (tv:gate), and an assistant whose scope waits too.
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

-- Slice 13b: two Google accounts (fictional; the token opens with oldsecret's fixture secret), one
-- connected and one the old app already needed connected again. A calendar of each, a recurring
-- series on the first with an assignment for one child, and a calendar whose account is gone.
INSERT INTO calendar_google_connection (household_id, id, google_subject, account_email, status, scope, refresh_token_encrypted, access_token_encrypted, access_token_expires_at, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000101', '1001', 'Parent@Example.test', 'connected', 'openid email https://www.googleapis.com/auth/calendar.readonly',
   '872dc466ff84ba59ca64297f:b995293500d7e16c130cef443823d573:d5582b7fa0096d63824808adf3a34e0422fe027ae99fda0f6574e28a', 'fixture-access-token-ciphertext', '2026-01-08T09:00:00Z', '2026-01-07T09:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000102', '1002', 'other@example.test', 'needs_reauth', 'openid email https://www.googleapis.com/auth/calendar.readonly',
   'not-a-ciphertext', NULL, NULL, '2026-01-07T10:00:00Z');
INSERT INTO calendar_source (household_id, id, provider, name, show_on_display, version, created_at, google_connection_id, google_calendar_id, google_sync_token) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000111', 'google', 'Family', true, 3, '2026-01-07T09:05:00Z', '0190a000-0000-7000-8000-000000000101', 'family@group.calendar.example.test', 'fixture-sync-token'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000112', 'google', 'Work', false, 1, '2026-01-07T10:05:00Z', '0190a000-0000-7000-8000-000000000102', 'other@example.test', NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000113', 'google', 'Gone', true, 1, '2026-01-07T11:05:00Z', '0190a000-0000-7000-8000-000000000199', 'gone@example.test', NULL);
INSERT INTO calendar_series (household_id, id, source_id, provider_uid, summary, all_day, recurring, status, content_hash, removed_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000121', '0190a000-0000-7000-8000-000000000111', 'fixtureevent0001', 'Piano', false, true, 'confirmed', repeat('e', 64), NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000122', '0190a000-0000-7000-8000-000000000113', 'fixtureevent0002', 'Gone', false, false, 'confirmed', repeat('f', 64), NULL);
INSERT INTO calendar_assignment (household_id, id, series_id, occurrence_key, version, active, cleared_from_version) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000131', '0190a000-0000-7000-8000-000000000121', NULL, 1, true, NULL),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000132', '0190a000-0000-7000-8000-000000000122', NULL, 1, true, NULL);
INSERT INTO calendar_assignment_person (household_id, assignment_id, child_id) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000131', '0190a000-0000-7000-8000-0000000000c2'),
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000132', '0190a000-0000-7000-8000-0000000000c1');
