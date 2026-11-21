-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
DELETE FROM tv_credit;
ALTER TABLE reward_redemption DISABLE TRIGGER reward_redemption_protect; -- the fixture database only
DELETE FROM reward_redemption;
DELETE FROM reward;
DELETE FROM assistant_credential;
ALTER TABLE household DROP COLUMN lock_message, DROP COLUMN game_portal_domains, DROP COLUMN game_domains;
ALTER TABLE child DROP COLUMN color;
