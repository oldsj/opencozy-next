-- +goose Up
-- Editing what adults manage: chores, routines, rewards and the household carry a version, so a
-- save made from a stale screen is refused instead of overwriting someone else's change.
ALTER TABLE chore ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version >= 1);
ALTER TABLE routine ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version >= 1);
ALTER TABLE reward ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version >= 1);
ALTER TABLE household ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK (version >= 1);

-- A redemption an adult has said yes to is given (fulfilled), or cancelled with its points back.
ALTER TABLE redemption DROP CONSTRAINT redemption_state_check;
ALTER TABLE redemption ADD CONSTRAINT redemption_state_check
    CHECK (state IN ('pending', 'approved', 'rejected', 'fulfilled', 'cancelled'));
ALTER TABLE redemption
    ADD COLUMN version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    ADD COLUMN fulfilled_at timestamptz,
    ADD COLUMN cancelled_at timestamptz,
    ADD CONSTRAINT redemption_fulfilled_at CHECK ((state = 'fulfilled') = (fulfilled_at IS NOT NULL) OR state = 'cancelled'),
    ADD CONSTRAINT redemption_cancelled_at CHECK ((state = 'cancelled') = (cancelled_at IS NOT NULL));
CREATE INDEX redemption_household_idx ON redemption (household_id, created_at DESC);

-- +goose Down
DROP INDEX redemption_household_idx;
ALTER TABLE redemption DROP CONSTRAINT redemption_cancelled_at, DROP CONSTRAINT redemption_fulfilled_at,
    DROP COLUMN cancelled_at, DROP COLUMN fulfilled_at, DROP COLUMN version;
UPDATE redemption SET state = 'approved' WHERE state IN ('fulfilled', 'cancelled');
ALTER TABLE redemption DROP CONSTRAINT redemption_state_check;
ALTER TABLE redemption ADD CONSTRAINT redemption_state_check CHECK (state IN ('pending', 'approved', 'rejected'));
ALTER TABLE household DROP COLUMN version;
ALTER TABLE reward DROP COLUMN version;
ALTER TABLE routine DROP COLUMN version;
ALTER TABLE chore DROP COLUMN version;
