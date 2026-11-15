-- +goose Up
-- Chores, routines, the day's occurrences of them, and approval rows. A child (on the wall
-- display) completes a chore or the last step of a routine; that makes the occurrence pending and
-- creates an approval, which an adult decides in the inbox (slice 3). Points land on approval.

-- A child's colour on every screen (the design system's ProfileColor).
ALTER TABLE child ADD COLUMN color text NOT NULL DEFAULT 'sky'
    CHECK (color IN ('rose', 'sage', 'sky', 'butter', 'lilac', 'peach', 'teal', 'sand'));

-- days is a weekday bitmask: bit 0 is Sunday ... bit 6 Saturday; 127 is every day.
CREATE TABLE chore (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    child_id     uuid NOT NULL,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    points       integer NOT NULL CHECK (points BETWEEN 0 AND 1000),
    days         smallint NOT NULL DEFAULT 127 CHECK (days BETWEEN 1 AND 127),
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);
CREATE INDEX chore_child_idx ON chore (household_id, child_id) WHERE archived_at IS NULL;

CREATE TABLE routine (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    child_id     uuid NOT NULL,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    points       integer NOT NULL CHECK (points BETWEEN 0 AND 1000),
    days         smallint NOT NULL DEFAULT 127 CHECK (days BETWEEN 1 AND 127),
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);
CREATE INDEX routine_child_idx ON routine (household_id, child_id) WHERE archived_at IS NULL;

CREATE TABLE routine_step (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    routine_id   uuid NOT NULL,
    title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    position     smallint NOT NULL CHECK (position BETWEEN 0 AND 19),
    UNIQUE (routine_id, position),
    FOREIGN KEY (household_id, routine_id) REFERENCES routine (household_id, id)
);

-- One chore or routine on one household day. The id is a UUIDv5 of the definition and the day,
-- so the plan a read computes and the row a command creates agree. points is the definition's
-- at the time it was completed.
CREATE TABLE occurrence (
    id              uuid PRIMARY KEY,
    household_id    uuid NOT NULL,
    child_id        uuid NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('chore', 'routine')),
    definition_id   uuid NOT NULL,
    day             date NOT NULL,
    state           text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'pending', 'complete')),
    points          integer NOT NULL CHECK (points BETWEEN 0 AND 1000),
    checked_steps   uuid[] NOT NULL DEFAULT '{}',
    -- An adult's reason for sending it back (slice 3); cleared when the child tries again.
    rejected_reason text CHECK (length(rejected_reason) BETWEEN 1 AND 200),
    updated_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (household_id, id),
    UNIQUE (definition_id, day),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);
CREATE INDEX occurrence_day_idx ON occurrence (household_id, day);

-- A request for an adult's decision. Decided once (slice 3): the first decision wins.
CREATE TABLE approval (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id  uuid NOT NULL,
    child_id      uuid NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('completion', 'routine')),
    occurrence_id uuid NOT NULL,
    title         text NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    points        integer NOT NULL CHECK (points BETWEEN 0 AND 1000),
    state         text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'rejected')),
    reason        text CHECK (length(reason) BETWEEN 1 AND 200),
    version       integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    decided_at    timestamptz,
    decided_by    uuid,
    created_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, occurrence_id) REFERENCES occurrence (household_id, id),
    FOREIGN KEY (household_id, decided_by) REFERENCES adult (household_id, id),
    CHECK ((state = 'pending') = (decided_at IS NULL))
);
-- At most one open request per occurrence.
CREATE UNIQUE INDEX approval_one_pending ON approval (occurrence_id) WHERE state = 'pending';
CREATE INDEX approval_pending_idx ON approval (household_id, created_at) WHERE state = 'pending';

-- +goose Down
DROP TABLE approval;
DROP TABLE occurrence;
DROP TABLE routine_step;
DROP TABLE routine;
DROP TABLE chore;
ALTER TABLE child DROP COLUMN color;
