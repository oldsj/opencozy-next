-- +goose Up
-- Screen time, after review (slice 8a).

-- Allowed hours are all four times or none: a partly set rule (which the old check let through,
-- since a comparison with NULL passes a CHECK) is refused.
ALTER TABLE child ADD CONSTRAINT child_allowed_hours_complete
    CHECK (num_nulls(school_night_start, school_night_end, weekend_start, weekend_end) IN (0, 4));

-- A parent can take back the unused rest of a grant or a "yes" (a reward's is taken back by
-- cancelling the reward, which refunds its points). The credit's seconds drop to what was used;
-- these record when, by whom, and how much went. A forfeited credit never takes time back.
ALTER TABLE screen_time_credit
    ADD COLUMN forfeited_at      timestamptz,
    ADD COLUMN forfeited_by      uuid,
    ADD COLUMN forfeited_seconds integer CHECK (forfeited_seconds BETWEEN 1 AND 86400),
    ADD CONSTRAINT screen_time_credit_forfeited_by FOREIGN KEY (household_id, forfeited_by) REFERENCES adult (household_id, id),
    ADD CONSTRAINT screen_time_credit_forfeit CHECK (
        (forfeited_at IS NULL AND forfeited_by IS NULL AND forfeited_seconds IS NULL)
        OR (forfeited_at IS NOT NULL AND forfeited_seconds IS NOT NULL AND source <> 'redemption'));

-- +goose Down
ALTER TABLE screen_time_credit
    DROP CONSTRAINT screen_time_credit_forfeit,
    DROP CONSTRAINT screen_time_credit_forfeited_by,
    DROP COLUMN forfeited_seconds,
    DROP COLUMN forfeited_by,
    DROP COLUMN forfeited_at;
ALTER TABLE child DROP CONSTRAINT child_allowed_hours_complete;
