-- Removes what later slices will map, so a real import of the seed can commit.
SET search_path TO opencozy;
ALTER TABLE household_owner DROP COLUMN push_muted_until;
