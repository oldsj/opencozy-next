-- Fictional rows for the importer's fixture tests, in the old app's schema (old_schema.sql).
-- Names come from the old app's fictional dev seed; addresses use example.test. The credential
-- secrets and the password were made for this fixture and are in importer_test.go.
SET search_path TO opencozy;

INSERT INTO household (id, name, time_zone, created_at, archived_at, lock_message, game_portal_domains, game_domains)
VALUES ('0190a000-0000-7000-8000-00000000000a', 'Example Household', 'Europe/Amsterdam', '2026-01-02T08:00:00Z', NULL, NULL, '{}', '{}');

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
