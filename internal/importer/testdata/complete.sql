-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
DELETE FROM service_credential_child WHERE credential_id = '0190a000-0000-7000-8000-0000000000d2';
DELETE FROM service_credential WHERE id = '0190a000-0000-7000-8000-0000000000d2';
DELETE FROM assistant_credential;
ALTER TABLE household DROP COLUMN lock_message, DROP COLUMN game_portal_domains, DROP COLUMN game_domains;
ALTER TABLE child DROP COLUMN color, DROP COLUMN school_night_start, DROP COLUMN school_night_end, DROP COLUMN weekend_start, DROP COLUMN weekend_end;
