-- Fictional rows in old tables the importer can't map yet (it reports them as pending, and a real
-- import refuses). scripts/import-rehearsal.sh applies this file unless ALLOW_PENDING=1. Move a
-- table's rows into seed.sql once the importer carries it.
SET search_path TO opencozy;
-- Empty while the importer maps every table this rehearsal seeds.
