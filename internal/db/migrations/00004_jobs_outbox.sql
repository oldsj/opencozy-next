-- +goose Up
-- Slice 0d: background job leases and the push outbox (design §8).

-- One row per background job. A replica runs a job only while it holds the lease, and each run
-- re-checks it inside its own transaction (internal/jobs), so a replica whose lease was taken
-- over writes nothing.
CREATE TABLE job_lease (
    name       text PRIMARY KEY CHECK (name ~ '^[a-z][a-z0-9-]{0,62}$'),
    holder     text NOT NULL CHECK (length(holder) BETWEEN 1 AND 128),
    expires_at timestamptz NOT NULL
);

-- Pushes waiting to be sent. Each is written in the same transaction as its event and sent after
-- commit, at least once. Rows hold names and task titles, so a row is deleted as soon as it is
-- sent or finally refused. A push goes to an adult's phones or a child's devices.
CREATE TABLE push_outbox (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id    uuid NOT NULL REFERENCES household (id),
    adult_id        uuid,
    child_id        uuid,
    dedupe_key      text NOT NULL CHECK (length(dedupe_key) BETWEEN 1 AND 200),
    payload         jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4096),
    attempts        integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, dedupe_key),
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    CHECK ((adult_id IS NULL) <> (child_id IS NULL))
);
CREATE INDEX push_outbox_next_attempt_at_idx ON push_outbox (next_attempt_at);

-- +goose Down
DROP TABLE push_outbox;
DROP TABLE job_lease;
