-- Fictional rows in old tables the importer can't map yet (it reports them as pending, and a real
-- import refuses). scripts/import-rehearsal.sh applies this file unless ALLOW_PENDING=1. Move a
-- table's rows into seed.sql once the importer carries it.
SET search_path TO opencozy;

-- A TV with its MAC address, and an ended session in which Avery watched for 10 minutes
-- (waits on slice 9's schema, #56).
INSERT INTO gated_tv (household_id, id, name, created_at, room, power) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000301', 'Living room TV', '2026-03-01T08:00:00Z', 'Living room', 'off');
INSERT INTO gated_tv_mac (household_id, tv_id, mac) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000301', '02:00:5e:10:00:01');
INSERT INTO tv_session (household_id, id, tv_id, watcher, child_id, charged_seconds, started_at, ended_at, end_reason, started_by, ended_by) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000302', '0190b000-0000-7000-8000-000000000301', 'child', '0190a000-0000-7000-8000-0000000000c1', 600,
   '2026-03-12T16:00:00Z', '2026-03-12T16:10:00Z', 'tv_off', 'child', 'system');

-- Banked TV time: Rowan bought 20 minutes of TV time (5 points, with its spend) and used 10
-- (waits on the TV-credit import, after slice 9).
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, fulfilled_at, screen_time_minutes_snapshot, screen_time_target_snapshot) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000311', '0190a000-0000-7000-8000-00000000010a', '0190a000-0000-7000-8000-0000000000c2', 'TV time', 5, 1, 1, 'fulfilled',
   '2026-03-04T15:00:00Z', '2026-03-04T15:00:00Z', '2026-03-04T15:00:00Z', 20, 'tv');
INSERT INTO point_ledger (household_id, id, child_id, redemption_id, reward_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000312', '0190a000-0000-7000-8000-0000000000c2', '0190b000-0000-7000-8000-000000000311', 'spend',
   '0190b000-0000-7000-8000-000000000313', -5, 'TV time', '2026-03-04T15:00:00Z');
INSERT INTO tv_credit (household_id, redemption_id, child_id, target, minutes, seconds, used_seconds, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190b000-0000-7000-8000-000000000311', '0190a000-0000-7000-8000-0000000000c2', 'tv', 20, 1200, 600, '2026-03-04T15:00:00Z');
