-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
DELETE FROM tv_credit;
DELETE FROM assistant_credential;
ALTER TABLE household DROP COLUMN lock_message;
ALTER TABLE child DROP COLUMN color;
ALTER TABLE household_owner DROP COLUMN push_muted_until;
