-- +goose Up
-- Rewards a child can spend points on, and their redemptions. Points are spent when the child
-- asks (a ledger redemption); an adult then approves, or says no and the points are refunded.

CREATE TABLE reward (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    cost         integer NOT NULL CHECK (cost BETWEEN 1 AND 100000),
    kind         text NOT NULL CHECK (kind IN ('item', 'screen_time')),
    minutes      integer CHECK (minutes BETWEEN 1 AND 1440),
    available    boolean NOT NULL DEFAULT true,
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (household_id, id),
    CHECK ((kind = 'screen_time') = (minutes IS NOT NULL))
);
CREATE INDEX reward_live_idx ON reward (household_id) WHERE archived_at IS NULL;

CREATE TABLE redemption (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    child_id     uuid NOT NULL,
    reward_id    uuid NOT NULL,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    cost         integer NOT NULL CHECK (cost BETWEEN 1 AND 100000),
    minutes      integer CHECK (minutes BETWEEN 1 AND 1440),
    state        text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'rejected')),
    -- The ledger row that spent the points; set in the same transaction as the insert.
    ledger_id    uuid REFERENCES point_ledger (id),
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, reward_id) REFERENCES reward (household_id, id)
);
CREATE INDEX redemption_child_idx ON redemption (household_id, child_id, created_at);

-- An approval is for an occurrence (a completion or routine) or a redemption.
ALTER TABLE approval DROP CONSTRAINT approval_kind_check;
ALTER TABLE approval ADD CONSTRAINT approval_kind_check CHECK (kind IN ('completion', 'routine', 'redemption'));
ALTER TABLE approval ALTER COLUMN occurrence_id DROP NOT NULL;
ALTER TABLE approval ADD COLUMN redemption_id uuid;
ALTER TABLE approval ADD CONSTRAINT approval_redemption_fk
    FOREIGN KEY (household_id, redemption_id) REFERENCES redemption (household_id, id);
ALTER TABLE approval ADD CONSTRAINT approval_subject_check
    CHECK ((kind = 'redemption') = (redemption_id IS NOT NULL AND occurrence_id IS NULL)
       AND (kind = 'redemption' OR occurrence_id IS NOT NULL));
CREATE UNIQUE INDEX approval_one_pending_redemption ON approval (redemption_id) WHERE state = 'pending';

-- +goose Down
DROP INDEX approval_one_pending_redemption;
ALTER TABLE approval DROP CONSTRAINT approval_subject_check;
ALTER TABLE approval DROP CONSTRAINT approval_redemption_fk;
ALTER TABLE approval DROP COLUMN redemption_id;
DELETE FROM approval WHERE occurrence_id IS NULL;
ALTER TABLE approval ALTER COLUMN occurrence_id SET NOT NULL;
ALTER TABLE approval DROP CONSTRAINT approval_kind_check;
ALTER TABLE approval ADD CONSTRAINT approval_kind_check CHECK (kind IN ('completion', 'routine'));
DROP TABLE redemption;
DROP TABLE reward;
