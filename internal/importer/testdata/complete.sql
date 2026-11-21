-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
DELETE FROM tv_credit;
DELETE FROM assistant_credential;
ALTER TABLE household DROP COLUMN lock_message;
ALTER TABLE child DROP COLUMN color;
ALTER TABLE household_owner DROP COLUMN push_muted_until;
-- Rewards wait on their slice; the ledger's spends and refunds point at them, so those go too
-- (ledger_test.go checks the whole ledger inside a transaction). The history triggers refuse
-- deletes, so this fixture script turns triggers off while it removes them.
SET session_replication_role = replica;
DELETE FROM point_ledger WHERE redemption_id IS NOT NULL;
DELETE FROM reward_redemption;
DELETE FROM reward;
SET session_replication_role = DEFAULT;
