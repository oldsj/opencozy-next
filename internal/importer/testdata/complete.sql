-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
DELETE FROM service_credential_child WHERE credential_id = '0190a000-0000-7000-8000-0000000000d2';
DELETE FROM service_credential WHERE id = '0190a000-0000-7000-8000-0000000000d2';
DELETE FROM assistant_credential;
ALTER TABLE household DROP COLUMN lock_message, DROP COLUMN game_portal_domains, DROP COLUMN game_domains;
ALTER TABLE child DROP COLUMN color;
ALTER TABLE household_owner DROP COLUMN push_muted_until;
-- Chores, routines, approvals and rewards wait on their slices; the ledger points at them, so it
-- goes too (ledger_test.go checks it inside a transaction). The history triggers refuse deletes,
-- so this fixture script turns triggers off while it removes them.
SET session_replication_role = replica;
DELETE FROM completion_approval;
DELETE FROM chore_active_award;
DELETE FROM chore_award_reversal;
DELETE FROM external_award;
DELETE FROM point_ledger;
DELETE FROM chore_occurrence;
DELETE FROM chore_definition;
DELETE FROM routine_occurrence;
DELETE FROM routine_definition;
DELETE FROM reward_redemption;
DELETE FROM reward;
SET session_replication_role = DEFAULT;
