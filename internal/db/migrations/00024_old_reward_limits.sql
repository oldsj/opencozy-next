-- +goose Up
-- The old app allowed reward titles up to 160 characters and costs up to 1,000,000 points. The
-- importer carries old rewards and redemptions as they are (a redemption's cost must match its
-- carried spend), so the database allows the old limits; the API keeps its tighter ones for new
-- input.
ALTER TABLE reward DROP CONSTRAINT reward_title_check,
    ADD CONSTRAINT reward_title_check CHECK (length(title) BETWEEN 1 AND 160),
    DROP CONSTRAINT reward_cost_check,
    ADD CONSTRAINT reward_cost_check CHECK (cost BETWEEN 1 AND 1000000);
ALTER TABLE redemption DROP CONSTRAINT redemption_title_check,
    ADD CONSTRAINT redemption_title_check CHECK (length(title) BETWEEN 1 AND 160),
    DROP CONSTRAINT redemption_cost_check,
    ADD CONSTRAINT redemption_cost_check CHECK (cost BETWEEN 1 AND 1000000);

-- +goose Down
-- Fails if an imported row is over the new limits: it can't go back losslessly.
ALTER TABLE reward DROP CONSTRAINT reward_title_check,
    ADD CONSTRAINT reward_title_check CHECK (length(title) BETWEEN 1 AND 80),
    DROP CONSTRAINT reward_cost_check,
    ADD CONSTRAINT reward_cost_check CHECK (cost BETWEEN 1 AND 100000);
ALTER TABLE redemption DROP CONSTRAINT redemption_title_check,
    ADD CONSTRAINT redemption_title_check CHECK (length(title) BETWEEN 1 AND 80),
    DROP CONSTRAINT redemption_cost_check,
    ADD CONSTRAINT redemption_cost_check CHECK (cost BETWEEN 1 AND 100000);
