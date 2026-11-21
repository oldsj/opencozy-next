-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
DELETE FROM tv_credit;
ALTER TABLE reward_redemption DISABLE TRIGGER reward_redemption_protect; -- the fixture database only
DELETE FROM reward_redemption;
DELETE FROM reward;
ALTER TABLE household DROP COLUMN lock_message;
ALTER TABLE child DROP COLUMN color;
