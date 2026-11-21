-- +goose Up
-- iPad screen time (slice 8). Minutes a child gets (a screen-time reward bought with points, a
-- parent's grant, or a parent's yes to "Ask for more time") are banked as a credit. Nothing runs
-- until the time is spent: spending writes unlock windows, back to back after any window already
-- queued, which the child's devices read. Windows are append-only; a parent ending time early
-- writes a cut instead, and every reader uses a window's effective end. Allowed hours limit only
-- a child's own spending. TV time is the TV gate's (slice 9).

-- Allowed hours, in minutes after local midnight: school nights (Sunday to Thursday) and weekends
-- (Friday and Saturday), each from start to a later end on the same day. All four or none.
ALTER TABLE child
    ADD COLUMN school_night_start smallint CHECK (school_night_start BETWEEN 0 AND 1439),
    ADD COLUMN school_night_end   smallint CHECK (school_night_end BETWEEN 1 AND 1439),
    ADD COLUMN weekend_start      smallint CHECK (weekend_start BETWEEN 0 AND 1439),
    ADD COLUMN weekend_end        smallint CHECK (weekend_end BETWEEN 1 AND 1439),
    ADD CONSTRAINT child_allowed_hours CHECK (
        (school_night_start IS NULL AND school_night_end IS NULL AND weekend_start IS NULL AND weekend_end IS NULL)
        OR (school_night_start < school_night_end AND weekend_start < weekend_end));

-- Which screen a screen-time reward gives minutes on. The redemption keeps a snapshot.
ALTER TABLE reward ADD COLUMN screen_time_target text CHECK (screen_time_target IN ('ipad', 'tv'));
UPDATE reward SET screen_time_target = 'ipad' WHERE kind = 'screen_time';
ALTER TABLE reward ADD CONSTRAINT reward_screen_time_target
    CHECK ((kind = 'screen_time') = (screen_time_target IS NOT NULL));
ALTER TABLE redemption ADD COLUMN screen_time_target text CHECK (screen_time_target IN ('ipad', 'tv'));
UPDATE redemption SET screen_time_target = 'ipad' WHERE minutes IS NOT NULL;
ALTER TABLE redemption ADD CONSTRAINT redemption_screen_time_target
    CHECK ((minutes IS NOT NULL) = (screen_time_target IS NOT NULL));

-- Banked iPad time. seconds is what the credit holds (lowered when a cancelled redemption forfeits
-- its unused rest); used_seconds what has been spent into windows (lowered when a parent ends time
-- early and gives the unused time back).
CREATE TABLE screen_time_credit (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id  uuid NOT NULL REFERENCES household (id),
    child_id      uuid NOT NULL,
    source        text NOT NULL CHECK (source IN ('redemption', 'grant', 'time_request')),
    redemption_id uuid UNIQUE,
    granted_by    uuid,
    minutes       integer NOT NULL CHECK (minutes BETWEEN 1 AND 1440),
    seconds       integer NOT NULL CHECK (seconds BETWEEN 0 AND 86400),
    used_seconds  integer NOT NULL DEFAULT 0 CHECK (used_seconds BETWEEN 0 AND seconds),
    created_at    timestamptz NOT NULL,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, redemption_id) REFERENCES redemption (household_id, id),
    FOREIGN KEY (household_id, granted_by) REFERENCES adult (household_id, id),
    CHECK ((source = 'redemption') = (redemption_id IS NOT NULL))
);
CREATE INDEX screen_time_credit_unspent_idx ON screen_time_credit (household_id, child_id, created_at, id)
    WHERE used_seconds < seconds;

-- An unlock window: the device is unlocked from starts_at to ends_at, unless a cut ends it sooner.
CREATE TABLE screen_time_window (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    child_id     uuid NOT NULL,
    credit_id    uuid NOT NULL,
    starts_at    timestamptz NOT NULL,
    ends_at      timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, credit_id) REFERENCES screen_time_credit (household_id, id),
    CHECK (ends_at > starts_at AND ends_at <= starts_at + interval '24 hours')
);
CREATE INDEX screen_time_window_child_idx ON screen_time_window (household_id, child_id, ends_at);

-- A parent ended a window early (or cancelled it before it started): at most one cut per window.
CREATE TABLE screen_time_cut (
    window_id        uuid PRIMARY KEY,
    household_id     uuid NOT NULL,
    cut_at           timestamptz NOT NULL,
    refunded_seconds integer NOT NULL CHECK (refunded_seconds >= 0),
    cut_by           uuid,
    created_at       timestamptz NOT NULL,
    FOREIGN KEY (household_id, window_id) REFERENCES screen_time_window (household_id, id),
    FOREIGN KEY (household_id, cut_by) REFERENCES adult (household_id, id)
);

-- What a child did on a locked device's shield ("One more minute"), sent by the app later. The
-- app makes the ID, so an upload repeated after a lost answer is recorded once.
CREATE TABLE screen_time_event (
    id           uuid NOT NULL,
    household_id uuid NOT NULL,
    child_id     uuid NOT NULL,
    device_id    uuid,
    kind         text NOT NULL CHECK (kind IN ('one_more_minute')),
    occurred_at  timestamptz NOT NULL,
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, device_id) REFERENCES device (household_id, id),
    CHECK (occurred_at <= created_at)
);
CREATE INDEX screen_time_event_child_idx ON screen_time_event (household_id, child_id, occurred_at);

-- "Ask for more time". A parent answers once: 30 minutes, an hour, all day (given at once, after
-- anything queued, outside allowed hours too), or no. Asking again while one waits reminds the
-- parents (asked_at moves on).
CREATE TABLE time_request (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id  uuid NOT NULL,
    child_id      uuid NOT NULL,
    device_id     uuid,
    state         text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'approved', 'declined')),
    choice        text CHECK (choice IN ('30min', '1h', 'all_day', 'no')),
    asked_at      timestamptz NOT NULL,
    decided_at    timestamptz,
    decided_by    uuid,
    credit_id     uuid,
    granted_until timestamptz,
    created_at    timestamptz NOT NULL,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, device_id) REFERENCES device (household_id, id),
    FOREIGN KEY (household_id, decided_by) REFERENCES adult (household_id, id),
    FOREIGN KEY (household_id, credit_id) REFERENCES screen_time_credit (household_id, id),
    CHECK ((state = 'pending') = (decided_at IS NULL) AND (state = 'pending') = (choice IS NULL)),
    CHECK ((state = 'approved') = (credit_id IS NOT NULL AND granted_until IS NOT NULL)),
    CHECK ((state = 'declined') = (choice = 'no') OR state = 'pending'),
    CHECK (asked_at >= created_at AND (decided_at IS NULL OR decided_at >= created_at))
);
CREATE UNIQUE INDEX time_request_one_pending ON time_request (household_id, child_id) WHERE state = 'pending';
CREATE INDEX time_request_child_idx ON time_request (household_id, child_id, created_at DESC);

-- A waiting time request is an inbox item.
ALTER TABLE approval DROP CONSTRAINT approval_kind_check;
ALTER TABLE approval ADD CONSTRAINT approval_kind_check
    CHECK (kind IN ('completion', 'routine', 'redemption', 'time_request'));
ALTER TABLE approval ADD COLUMN time_request_id uuid;
ALTER TABLE approval ADD CONSTRAINT approval_time_request_fk
    FOREIGN KEY (household_id, time_request_id) REFERENCES time_request (household_id, id);
ALTER TABLE approval DROP CONSTRAINT approval_subject_check;
ALTER TABLE approval ADD CONSTRAINT approval_subject_check CHECK (
    num_nonnulls(occurrence_id, redemption_id, time_request_id) = 1
    AND (kind = 'redemption') = (redemption_id IS NOT NULL)
    AND (kind = 'time_request') = (time_request_id IS NOT NULL));
CREATE UNIQUE INDEX approval_one_pending_time_request ON approval (time_request_id) WHERE state = 'pending';

-- Windows, cuts and shield events are written once.
CREATE TRIGGER screen_time_window_append_only BEFORE UPDATE OR DELETE ON screen_time_window
    FOR EACH ROW EXECUTE FUNCTION refuse_change();
CREATE TRIGGER screen_time_window_no_truncate BEFORE TRUNCATE ON screen_time_window
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_change();
CREATE TRIGGER screen_time_cut_append_only BEFORE UPDATE OR DELETE ON screen_time_cut
    FOR EACH ROW EXECUTE FUNCTION refuse_change();
CREATE TRIGGER screen_time_cut_no_truncate BEFORE TRUNCATE ON screen_time_cut
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_change();
CREATE TRIGGER screen_time_event_append_only BEFORE UPDATE OR DELETE ON screen_time_event
    FOR EACH ROW EXECUTE FUNCTION refuse_change();

-- +goose Down
DROP INDEX approval_one_pending_time_request;
ALTER TABLE approval DROP CONSTRAINT approval_subject_check;
DELETE FROM approval WHERE kind = 'time_request';
ALTER TABLE approval DROP CONSTRAINT approval_time_request_fk;
ALTER TABLE approval DROP COLUMN time_request_id;
ALTER TABLE approval ADD CONSTRAINT approval_subject_check
    CHECK ((kind = 'redemption') = (redemption_id IS NOT NULL AND occurrence_id IS NULL)
       AND (kind = 'redemption' OR occurrence_id IS NOT NULL));
ALTER TABLE approval DROP CONSTRAINT approval_kind_check;
ALTER TABLE approval ADD CONSTRAINT approval_kind_check CHECK (kind IN ('completion', 'routine', 'redemption'));
DROP TABLE time_request;
DROP TABLE screen_time_event;
DROP TABLE screen_time_cut;
DROP TABLE screen_time_window;
DROP TABLE screen_time_credit;
ALTER TABLE redemption DROP CONSTRAINT redemption_screen_time_target, DROP COLUMN screen_time_target;
ALTER TABLE reward DROP CONSTRAINT reward_screen_time_target, DROP COLUMN screen_time_target;
ALTER TABLE child DROP CONSTRAINT child_allowed_hours,
    DROP COLUMN school_night_start, DROP COLUMN school_night_end, DROP COLUMN weekend_start, DROP COLUMN weekend_end;
