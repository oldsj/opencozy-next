-- +goose Up
-- The old app allowed longer titles, more points and more routine steps than the new API
-- accepts for new input (titles 1-160, points up to 1,000,000, 32 steps). The importer carries
-- old rows as they are, so the database allows the old limits; the API keeps its tighter ones
-- for anything new or edited.
ALTER TABLE chore DROP CONSTRAINT chore_title_check,
    ADD CONSTRAINT chore_title_check CHECK (length(title) BETWEEN 1 AND 160),
    DROP CONSTRAINT chore_points_check,
    ADD CONSTRAINT chore_points_check CHECK (points BETWEEN 0 AND 1000000);
ALTER TABLE routine DROP CONSTRAINT routine_title_check,
    ADD CONSTRAINT routine_title_check CHECK (length(title) BETWEEN 1 AND 160),
    DROP CONSTRAINT routine_points_check,
    ADD CONSTRAINT routine_points_check CHECK (points BETWEEN 0 AND 1000000);
ALTER TABLE routine_step DROP CONSTRAINT routine_step_title_check,
    ADD CONSTRAINT routine_step_title_check CHECK (length(title) BETWEEN 1 AND 160),
    DROP CONSTRAINT routine_step_position_check,
    ADD CONSTRAINT routine_step_position_check CHECK (position BETWEEN 0 AND 31);
ALTER TABLE occurrence DROP CONSTRAINT occurrence_points_check,
    ADD CONSTRAINT occurrence_points_check CHECK (points BETWEEN 0 AND 1000000);
ALTER TABLE approval DROP CONSTRAINT approval_title_check,
    ADD CONSTRAINT approval_title_check CHECK (length(title) BETWEEN 1 AND 160),
    DROP CONSTRAINT approval_points_check,
    ADD CONSTRAINT approval_points_check CHECK (points BETWEEN 0 AND 1000000);

-- +goose Down
-- Fails if an imported row is over the new limits, which is the point: it can't go back losslessly.
ALTER TABLE chore DROP CONSTRAINT chore_title_check,
    ADD CONSTRAINT chore_title_check CHECK (length(title) BETWEEN 1 AND 80),
    DROP CONSTRAINT chore_points_check,
    ADD CONSTRAINT chore_points_check CHECK (points BETWEEN 0 AND 1000);
ALTER TABLE routine DROP CONSTRAINT routine_title_check,
    ADD CONSTRAINT routine_title_check CHECK (length(title) BETWEEN 1 AND 80),
    DROP CONSTRAINT routine_points_check,
    ADD CONSTRAINT routine_points_check CHECK (points BETWEEN 0 AND 1000);
ALTER TABLE routine_step DROP CONSTRAINT routine_step_title_check,
    ADD CONSTRAINT routine_step_title_check CHECK (length(title) BETWEEN 1 AND 80),
    DROP CONSTRAINT routine_step_position_check,
    ADD CONSTRAINT routine_step_position_check CHECK (position BETWEEN 0 AND 19);
ALTER TABLE occurrence DROP CONSTRAINT occurrence_points_check,
    ADD CONSTRAINT occurrence_points_check CHECK (points BETWEEN 0 AND 1000);
ALTER TABLE approval DROP CONSTRAINT approval_title_check,
    ADD CONSTRAINT approval_title_check CHECK (length(title) BETWEEN 1 AND 80),
    DROP CONSTRAINT approval_points_check,
    ADD CONSTRAINT approval_points_check CHECK (points BETWEEN 0 AND 1000);
