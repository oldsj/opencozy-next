-- For the imported journey (JOURNEY=1 scripts/import-rehearsal.sh), applied last to the old
-- database: what web/e2e/imported.spec.ts and scripts/smoke.sh need to drive the imported
-- household, and nothing that changes what the rehearsal reconciles (no points or banked time).
-- Fictional data only. The secrets are made for this fixture, and their other halves are in
-- web/e2e/imported-fixture.ts: they open nothing but this throwaway database.
SET search_path TO opencozy;

-- Avery's iPad gets a key the spec holds the private half of (the fixture's has none anywhere).
UPDATE device SET public_key = 'BCNztyVSdhrJcnbEdPaWtFCjaF53emj+PmzggZA5AEkXJK9OWah44870+ratWkXnX4a8QFb1yNdlWfClrBJ61V8='
WHERE id = '0190a000-0000-7000-8000-0000000000e1';

-- Any time of day: Avery's allowed hours and the Den TV's open hours would make the run depend
-- on the clock (iPad time outside her hours is refused; an open TV needs no one watching).
UPDATE child SET school_night_start = NULL, school_night_end = NULL, weekend_start = NULL, weekend_end = NULL
WHERE id = '0190a000-0000-7000-8000-0000000000c1';
UPDATE gated_tv SET open_start = NULL, open_end = NULL, open_days = NULL
WHERE id = '0190a000-0000-7000-8000-0000000009a1';

-- The router also gates games, as the smoke test asks of it.
UPDATE service_credential SET scopes = '{tv:gate,games:gate}'
WHERE id = '0190a000-0000-7000-8000-0000000000d2';

-- An assistant whose scope the importer maps (the fixture's waits on approvals:read), for the
-- smoke test's MCP check.
INSERT INTO assistant_credential (household_id, id, user_id, name, credential_hash, scopes, created_at, last_used_at, revoked_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000a01', 'fixtureOwnerUser01', 'Journey assistant',
   '81c308f0036fb70463b4fd282b1acfca9db767b8a027499ff896890d48e12f54', '{tv:read}', '2026-03-01T08:00:00Z', NULL, NULL);
