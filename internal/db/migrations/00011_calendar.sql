-- +goose Up
-- Calendars (slice 13a: ICS feeds). A source is one calendar a household follows; refreshing it
-- replaces its series and events. Event ids stay stable across refreshes (series id plus
-- recurrence key), and assignments hang off the series, so they survive a refresh.

CREATE TABLE calendar_source (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id         uuid NOT NULL REFERENCES household (id),
    provider             text NOT NULL CHECK (provider IN ('ics', 'google', 'icloud')),
    name                 text NOT NULL CHECK (name = btrim(name) AND length(name) BETWEEN 1 AND 80),
    color                text NOT NULL
        CHECK (color IN ('rose', 'sage', 'sky', 'butter', 'lilac', 'peach', 'teal', 'sand')),
    show_on_display      boolean NOT NULL DEFAULT true,
    -- An ICS feed's URL is a secret (it often carries a token): internal/keyring ciphertext, and
    -- an HMAC of it for the one-feed-once check. Neither is ever returned.
    feed_url             text CHECK (length(feed_url) BETWEEN 1 AND 8192),
    feed_url_mac         text CHECK (length(feed_url_mac) BETWEEN 1 AND 200),
    version              integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- Sync state. next_sync_at is also the claim: the refresh job pushes it forward before it
    -- fetches, so two runs never fetch one feed at once.
    next_sync_at         timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_attempt_at      timestamptz,
    last_success_at      timestamptz,
    error_code           text CHECK (error_code IN ('timeout', 'not_found', 'access_denied', 'too_large',
        'invalid_calendar', 'too_many_events', 'rate_limited', 'token_expired')),
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    http_etag            text CHECK (length(http_etag) BETWEEN 1 AND 1024),
    http_last_modified   text CHECK (length(http_last_modified) BETWEEN 1 AND 200),
    -- What the stored events were built from: the feed's hash and the first day of the window.
    feed_hash            text CHECK (feed_hash ~ '^[a-f0-9]{64}$'),
    window_start         date,
    CHECK ((provider = 'ics') = (feed_url IS NOT NULL AND feed_url_mac IS NOT NULL)),
    UNIQUE (household_id, id),
    UNIQUE (household_id, feed_url_mac)
);
CREATE INDEX calendar_source_due_idx ON calendar_source (next_sync_at);

CREATE TABLE calendar_series (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    source_id    uuid NOT NULL,
    uid          text NOT NULL CHECK (length(uid) BETWEEN 1 AND 1024),
    recurring    boolean NOT NULL,
    -- The last successful fetch whose feed had this UID. A series missing from a fetch keeps its
    -- row and assignments (a briefly empty or damaged feed must not wipe them); one missing for
    -- 30 days is pruned.
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, uid),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, source_id) REFERENCES calendar_source (household_id, id) ON DELETE CASCADE
);

CREATE TABLE calendar_event (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id   uuid NOT NULL,
    source_id      uuid NOT NULL,
    series_id      uuid NOT NULL,
    -- 'single', or the original start of a recurrence: a date for all-day, UTC otherwise.
    recurrence_key text NOT NULL CHECK (recurrence_key ~ '^(single|[0-9]{8}|[0-9]{8}T[0-9]{6}Z)$'),
    all_day        boolean NOT NULL,
    starts_at      timestamptz,
    ends_at        timestamptz,
    start_date     date,
    end_date       date, -- exclusive
    title          text CHECK (length(title) BETWEEN 1 AND 500),
    location       text CHECK (length(location) BETWEEN 1 AND 500),
    description    text CHECK (length(description) BETWEEN 1 AND 4000),
    status         text NOT NULL CHECK (status IN ('confirmed', 'tentative')),
    UNIQUE (series_id, recurrence_key),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, series_id) REFERENCES calendar_series (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, source_id) REFERENCES calendar_source (household_id, id) ON DELETE CASCADE,
    CHECK (CASE WHEN all_day
        THEN start_date IS NOT NULL AND end_date IS NOT NULL AND isfinite(start_date) AND isfinite(end_date)
            AND end_date > start_date AND starts_at IS NULL AND ends_at IS NULL
        ELSE starts_at IS NOT NULL AND ends_at IS NOT NULL AND isfinite(starts_at) AND isfinite(ends_at)
            AND ends_at >= starts_at AND start_date IS NULL AND end_date IS NULL END)
);
CREATE INDEX calendar_event_time_idx ON calendar_event (household_id, starts_at) WHERE NOT all_day;
CREATE INDEX calendar_event_date_idx ON calendar_event (household_id, start_date) WHERE all_day;
CREATE INDEX calendar_event_source_idx ON calendar_event (source_id);

-- Which children an event is for: a whole series ('*'), or one occurrence (its recurrence key),
-- which wins over the series. An assignment with no children says "nobody".
CREATE TABLE calendar_assignment (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id   uuid NOT NULL,
    series_id      uuid NOT NULL,
    recurrence_key text NOT NULL CHECK (recurrence_key ~ '^(\*|single|[0-9]{8}|[0-9]{8}T[0-9]{6}Z)$'),
    UNIQUE (series_id, recurrence_key),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, series_id) REFERENCES calendar_series (household_id, id) ON DELETE CASCADE
);

CREATE TABLE calendar_assignment_child (
    household_id  uuid NOT NULL,
    assignment_id uuid NOT NULL,
    child_id      uuid NOT NULL,
    PRIMARY KEY (assignment_id, child_id),
    FOREIGN KEY (household_id, assignment_id) REFERENCES calendar_assignment (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);
CREATE INDEX calendar_assignment_child_idx ON calendar_assignment_child (household_id, child_id);

-- +goose Down
DROP TABLE calendar_assignment_child;
DROP TABLE calendar_assignment;
DROP TABLE calendar_event;
DROP TABLE calendar_series;
DROP TABLE calendar_source;
