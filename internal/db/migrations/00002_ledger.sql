-- +goose Up
-- Points. The ledger is append-only: a balance is SUM(delta) per child, and every award,
-- reversal, redemption and refund is a row written in the same transaction as its state change.
-- The triggers below belong to the migration role; the runtime role cannot drop or replace them,
-- and internal/db.Grant also withholds UPDATE, DELETE and TRUNCATE.

-- +goose StatementBegin
CREATE FUNCTION refuse_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only: % refused', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'restrict_violation';
END
$$;
-- +goose StatementEnd

CREATE TABLE point_ledger (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id  uuid NOT NULL,
    child_id      uuid NOT NULL,
    delta         integer NOT NULL CHECK (delta <> 0 AND delta BETWEEN -1000000 AND 1000000),
    -- award and redemption start something; reversal undoes an award, refund a redemption.
    kind          text NOT NULL CHECK (kind IN ('award', 'reversal', 'redemption', 'refund', 'adjustment')),
    -- What the points are for: a chore occurrence, a reward redemption, a learning event...
    source_kind   text NOT NULL CHECK (source_kind ~ '^[a-z][a-z_]{0,39}$'),
    source_id     uuid NOT NULL,
    -- The row a reversal or refund undoes. A row is undone at most once.
    reverses_id   uuid UNIQUE REFERENCES point_ledger (id),
    -- Who caused it: an adult, a credential, or neither (a job).
    adult_id      uuid,
    credential_id uuid,
    note          text CHECK (length(note) BETWEEN 1 AND 200),
    created_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id),
    FOREIGN KEY (household_id, credential_id) REFERENCES credential (household_id, id),
    CHECK ((kind IN ('reversal', 'refund')) = (reverses_id IS NOT NULL)),
    CHECK (kind NOT IN ('award', 'refund') OR delta > 0),
    CHECK (kind NOT IN ('reversal', 'redemption') OR delta < 0)
);
CREATE INDEX point_ledger_child_idx ON point_ledger (household_id, child_id, created_at);
CREATE INDEX point_ledger_source_idx ON point_ledger (source_kind, source_id);

-- A reversal or refund must undo exactly the row it names: same child, same source, the
-- opposite amount, an award for a reversal and a redemption for a refund.
-- +goose StatementBegin
CREATE FUNCTION point_ledger_check_undo() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    orig point_ledger;
BEGIN
    IF NEW.reverses_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT * INTO orig FROM point_ledger WHERE id = NEW.reverses_id;
    IF orig.household_id <> NEW.household_id OR orig.child_id <> NEW.child_id
        OR orig.source_kind <> NEW.source_kind OR orig.source_id <> NEW.source_id
        OR orig.delta <> -NEW.delta
        OR orig.kind <> (CASE NEW.kind WHEN 'reversal' THEN 'award' ELSE 'redemption' END) THEN
        RAISE EXCEPTION 'point_ledger: % does not undo row %', NEW.kind, NEW.reverses_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER point_ledger_check_undo BEFORE INSERT ON point_ledger
    FOR EACH ROW EXECUTE FUNCTION point_ledger_check_undo();
CREATE TRIGGER point_ledger_append_only BEFORE UPDATE OR DELETE ON point_ledger
    FOR EACH ROW EXECUTE FUNCTION refuse_change();
CREATE TRIGGER point_ledger_no_truncate BEFORE TRUNCATE ON point_ledger
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_change();

-- The current, unreversed award for each source. An undone chore can be completed and awarded
-- again, so the ledger cannot be unique on its source; this row is inserted with the award and
-- deleted with its reversal, both under the child's lock, and the key stops a second payment.
CREATE TABLE active_award (
    source_kind  text NOT NULL,
    source_id    uuid NOT NULL,
    household_id uuid NOT NULL,
    ledger_id    uuid NOT NULL UNIQUE REFERENCES point_ledger (id),
    PRIMARY KEY (source_kind, source_id)
);

-- command_log is write-once: a recorded answer never changes, and rows go only when they expire.
-- +goose StatementBegin
CREATE FUNCTION command_log_expiry_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND OLD.created_at < clock_timestamp() - interval '30 days' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'command_log is write-once: % refused', TG_OP
        USING ERRCODE = 'restrict_violation';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER command_log_write_once BEFORE UPDATE OR DELETE ON command_log
    FOR EACH ROW EXECUTE FUNCTION command_log_expiry_only();
CREATE TRIGGER command_log_no_truncate BEFORE TRUNCATE ON command_log
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_change();

-- +goose Down
DROP TRIGGER command_log_no_truncate ON command_log;
DROP TRIGGER command_log_write_once ON command_log;
DROP FUNCTION command_log_expiry_only();
DROP TABLE active_award;
DROP TABLE point_ledger;
DROP FUNCTION point_ledger_check_undo();
DROP FUNCTION refuse_change();
