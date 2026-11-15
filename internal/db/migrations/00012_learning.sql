-- +goose Up
-- Learning awards: a learning app (a service credential with learning:award) reports an activity
-- a child did; an award rule turns it into points, up to a daily limit.

-- An award rule: points per activity and the most points it may give a child in a household day.
-- Rules are archived, never deleted, so awards keep their rule.
CREATE TABLE award_rule (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    activity_key text NOT NULL CHECK (length(activity_key) BETWEEN 1 AND 160 AND activity_key = btrim(activity_key)),
    points       integer NOT NULL CHECK (points BETWEEN 1 AND 1000000),
    daily_limit  integer NOT NULL CHECK (daily_limit BETWEEN 1 AND 1000000),
    version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id)
);
-- One live rule per activity, so an award never has to choose between two.
CREATE UNIQUE INDEX award_rule_live_activity ON award_rule (household_id, activity_key) WHERE archived_at IS NULL;

-- The rules a learning credential may award with.
CREATE TABLE credential_award_rule (
    household_id  uuid NOT NULL,
    credential_id uuid NOT NULL,
    rule_id       uuid NOT NULL,
    PRIMARY KEY (credential_id, rule_id),
    FOREIGN KEY (household_id, credential_id) REFERENCES credential (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, rule_id) REFERENCES award_rule (household_id, id)
);

-- A child's account in a learning app (a Minecraft player ID, say). Links are kept after
-- unlinking, for the history; only unlinking changes a row.
CREATE TABLE learning_link (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    child_id     uuid NOT NULL,
    provider     text NOT NULL CHECK (provider ~ '^[a-z][a-z0-9_-]{0,79}$'),
    external_id  text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 255 AND external_id = btrim(external_id)),
    linked_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    unlinked_at  timestamptz,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    CHECK (unlinked_at IS NULL OR unlinked_at >= linked_at)
);
-- An account is linked to at most one child at a time.
CREATE UNIQUE INDEX learning_link_live ON learning_link (household_id, provider, external_id) WHERE unlinked_at IS NULL;
CREATE INDEX learning_link_child_idx ON learning_link (household_id, child_id);

-- +goose StatementBegin
CREATE FUNCTION learning_link_unlink_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.unlinked_at IS NULL AND NEW.unlinked_at IS NOT NULL
        AND (NEW.id, NEW.household_id, NEW.child_id, NEW.provider, NEW.external_id, NEW.linked_at)
            = (OLD.id, OLD.household_id, OLD.child_id, OLD.provider, OLD.external_id, OLD.linked_at) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'learning_link: only unlinking is allowed (% refused)', TG_OP
        USING ERRCODE = 'restrict_violation';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER learning_link_unlink_only BEFORE UPDATE OR DELETE ON learning_link
    FOR EACH ROW EXECUTE FUNCTION learning_link_unlink_only();
CREATE TRIGGER learning_link_no_truncate BEFORE TRUNCATE ON learning_link
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_change();

-- An award, once per source event, forever: a retry, however late, never pays twice. The
-- ledger row is its points (source_kind 'learning', source_id = this id); an adult undoes it with
-- a reversal there, and the event still can't be awarded again. rule is the rule as it was.
CREATE TABLE learning_award (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id    uuid NOT NULL,
    source_event_id uuid NOT NULL,
    child_id        uuid NOT NULL,
    rule_id         uuid NOT NULL,
    rule            jsonb NOT NULL CHECK (jsonb_typeof(rule) = 'object'),
    points          integer NOT NULL CHECK (points BETWEEN 1 AND 1000000),
    -- The household day it counts against the rule's daily limit.
    day             date NOT NULL,
    -- What was sent, for telling a retry from a reused event ID.
    provider        text NOT NULL,
    external_id     text NOT NULL,
    activity_key    text NOT NULL,
    credential_id   uuid,
    ledger_id       uuid NOT NULL UNIQUE REFERENCES point_ledger (id),
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (household_id, source_event_id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, rule_id) REFERENCES award_rule (household_id, id),
    FOREIGN KEY (household_id, credential_id) REFERENCES credential (household_id, id)
);
CREATE INDEX learning_award_day_idx ON learning_award (household_id, child_id, rule_id, day);
CREATE TRIGGER learning_award_append_only BEFORE UPDATE OR DELETE ON learning_award
    FOR EACH ROW EXECUTE FUNCTION refuse_change();
CREATE TRIGGER learning_award_no_truncate BEFORE TRUNCATE ON learning_award
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_change();

-- +goose Down
DROP TABLE learning_award;
DROP TABLE learning_link;
DROP FUNCTION learning_link_unlink_only();
DROP TABLE credential_award_rule;
DROP TABLE award_rule;
