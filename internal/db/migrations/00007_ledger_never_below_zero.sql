-- +goose Up
-- A child's balance never goes below zero. internal/ledger checks it under the child's lock; this
-- trigger makes the database refuse it too, for any writer. It takes the same child row lock
-- (ledger callers already hold it, so no new lock order), then checks the sum with the new row.
-- +goose StatementBegin
CREATE FUNCTION point_ledger_never_below_zero() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    bal bigint;
BEGIN
    IF NEW.delta >= 0 THEN
        RETURN NEW;
    END IF;
    PERFORM 1 FROM child WHERE household_id = NEW.household_id AND id = NEW.child_id FOR UPDATE;
    SELECT COALESCE(SUM(delta), 0) INTO bal FROM point_ledger
    WHERE household_id = NEW.household_id AND child_id = NEW.child_id;
    IF bal + NEW.delta < 0 THEN
        RAISE EXCEPTION 'point_ledger: balance of child % would go below zero', NEW.child_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER point_ledger_never_below_zero BEFORE INSERT ON point_ledger
    FOR EACH ROW EXECUTE FUNCTION point_ledger_never_below_zero();

-- +goose Down
DROP TRIGGER point_ledger_never_below_zero ON point_ledger;
DROP FUNCTION point_ledger_never_below_zero();
