-- +goose Up
-- The TV gate (slice 9). TVs are household devices with no child attached. The router
-- (service credential, tv:gate) lets a TV's MACs online only while it has a session or is open;
-- Home Assistant (tv:report) reports power, and a TV turning on with nobody watching asks the
-- parents "Who's watching?". A child's session spends that child's TV time (tv_credit).
--
-- Every write holds the household lock (internal/command), which serializes the TV domain, so
-- rows here take no locks of their own.

CREATE TABLE tv (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id     uuid NOT NULL REFERENCES household (id),
    name             text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    room             text CHECK (room IS NULL OR length(btrim(room)) BETWEEN 1 AND 80),
    ha_entity_id     text CHECK (ha_entity_id IS NULL OR ha_entity_id ~ '^[a-z0-9_]{1,64}[.][a-z0-9_]{1,190}$'),
    -- The last thing Home Assistant said about the TV; 'unknown' when nothing current has.
    power            text NOT NULL DEFAULT 'unknown' CHECK (power IN ('unknown', 'on', 'off')),
    power_changed_at timestamptz,
    -- The router's reports of the TV trying to go online while blocked: the start of the
    -- current unbroken run, and the latest report.
    attempts_since   timestamptz,
    last_attempt_at  timestamptz,
    -- Open hours: one daily window (minutes after household midnight) when the TV is simply
    -- online. An end before the start crosses midnight. open_days is a weekday bitmask (bit 0
    -- Sunday … bit 6 Saturday) of the days a window starts on; NULL is every day.
    open_start       smallint CHECK (open_start BETWEEN 0 AND 1439),
    open_end         smallint CHECK (open_end BETWEEN 0 AND 1439),
    open_days        smallint CHECK (open_days BETWEEN 1 AND 126),
    -- A parent's "Open TVs": open, as in open hours, until then. Past values are simply closed.
    open_until       timestamptz CHECK (open_until IS NULL OR isfinite(open_until)),
    version          integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    archived_at      timestamptz,
    UNIQUE (household_id, id),
    CHECK ((open_start IS NULL) = (open_end IS NULL) AND (open_start IS NULL OR open_start <> open_end)),
    CHECK (open_days IS NULL OR open_start IS NOT NULL),
    CHECK ((attempts_since IS NULL) = (last_attempt_at IS NULL) AND (attempts_since IS NULL OR attempts_since <= last_attempt_at))
);
CREATE INDEX tv_live_idx ON tv (household_id) WHERE archived_at IS NULL;
CREATE UNIQUE INDEX tv_entity_live ON tv (household_id, ha_entity_id) WHERE archived_at IS NULL AND ha_entity_id IS NOT NULL;

-- Every MAC of a live TV (1 to 8 each); removing a TV deletes its MACs, so a MAC is listed once
-- per household.
CREATE TABLE tv_mac (
    household_id uuid NOT NULL,
    tv_id        uuid NOT NULL,
    mac          text NOT NULL CHECK (mac ~ '^[0-9a-f]{2}(:[0-9a-f]{2}){5}$'),
    PRIMARY KEY (household_id, mac),
    FOREIGN KEY (household_id, tv_id) REFERENCES tv (household_id, id) ON DELETE CASCADE
);
CREATE INDEX tv_mac_tv_idx ON tv_mac (household_id, tv_id);

-- A child's TV time: one row per TV-target redemption or grant (slice 8 calls tv.AddCredit).
-- Sessions spend credits oldest first. granted_seconds is what was given; seconds drops to
-- used_seconds when a credit is cancelled (the unused rest is forfeited and refunded).
CREATE TABLE tv_credit (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id    uuid NOT NULL,
    child_id        uuid NOT NULL,
    redemption_id   uuid,
    granted_seconds integer NOT NULL CHECK (granted_seconds BETWEEN 1 AND 86400),
    seconds         integer NOT NULL,
    used_seconds    integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL CHECK (isfinite(created_at)),
    UNIQUE (household_id, id),
    UNIQUE (household_id, redemption_id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    FOREIGN KEY (household_id, redemption_id) REFERENCES redemption (household_id, id),
    CHECK (used_seconds >= 0 AND used_seconds <= seconds AND seconds <= granted_seconds)
);
CREATE INDEX tv_credit_unspent_idx ON tv_credit (household_id, child_id, created_at, id) WHERE seconds > used_seconds;

-- A TV session: the TV is allowed online while it is open. Parents are never charged; a child's
-- session spends that child's TV time and ends when it runs out, or at ends_by (the end of the
-- child's allowed hours when it started). charged_seconds is the share already taken from
-- credits. started_by and ended_by name an adult or a device; neither has a foreign key, so
-- history outlives accounts.
CREATE TABLE tv_session (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id      uuid NOT NULL,
    tv_id             uuid NOT NULL,
    watcher           text NOT NULL CHECK (watcher IN ('parents', 'child')),
    child_id          uuid,
    charged_seconds   integer NOT NULL DEFAULT 0,
    ends_by           timestamptz,
    started_at        timestamptz NOT NULL CHECK (isfinite(started_at)),
    ended_at          timestamptz CHECK (ended_at IS NULL OR isfinite(ended_at)),
    end_reason        text CHECK (end_reason IN ('tv_off', 'time_up', 'parent_ended', 'done', 'open_hours', 'parent_override')),
    started_by_adult  uuid,
    started_by_device uuid,
    ended_by_adult    uuid,
    -- When the child's devices were told the session is almost over (once).
    warned_at         timestamptz,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, tv_id) REFERENCES tv (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id),
    CHECK ((watcher = 'parents' AND child_id IS NULL) OR (watcher = 'child' AND child_id IS NOT NULL)),
    CHECK (charged_seconds >= 0 AND (watcher = 'child' OR charged_seconds = 0)),
    CHECK ((ended_at IS NULL) = (end_reason IS NULL) AND (ended_at IS NULL OR ended_at >= started_at)),
    CHECK (ends_by IS NULL OR (watcher = 'child' AND isfinite(ends_by) AND ends_by > started_at)),
    CHECK (started_by_adult IS NULL OR started_by_device IS NULL)
);
CREATE UNIQUE INDEX tv_session_open_tv ON tv_session (household_id, tv_id) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX tv_session_open_child ON tv_session (household_id, child_id) WHERE ended_at IS NULL AND watcher = 'child';
CREATE INDEX tv_session_started_idx ON tv_session (household_id, started_at DESC, id DESC);

-- "Who's watching?": raised when a TV turns on (Home Assistant, or the router seeing it try)
-- with nobody watching, or when a display or an adult asks; answered once (a session starts, or
-- "Not now"), or withdrawn when the TV turns off or opens. Not carried by the import.
CREATE TABLE tv_prompt (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id       uuid NOT NULL,
    tv_id              uuid NOT NULL,
    source             text NOT NULL CHECK (source IN ('home_assistant', 'router', 'display', 'adult')),
    state              text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'answered', 'dismissed', 'withdrawn')),
    version            integer NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL,
    decided_at         timestamptz,
    decided_by_adult   uuid,
    decided_by_device  uuid,
    session_id         uuid,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, tv_id) REFERENCES tv (household_id, id),
    FOREIGN KEY (household_id, session_id) REFERENCES tv_session (household_id, id),
    CHECK ((state = 'pending' AND version = 1 AND decided_at IS NULL AND decided_by_adult IS NULL AND decided_by_device IS NULL)
        OR (state <> 'pending' AND version = 2 AND decided_at IS NOT NULL AND decided_at >= created_at)),
    CHECK ((state = 'answered') = (session_id IS NOT NULL))
);
CREATE UNIQUE INDEX tv_prompt_pending_tv ON tv_prompt (household_id, tv_id) WHERE state = 'pending';
CREATE INDEX tv_prompt_decided_idx ON tv_prompt (household_id, tv_id, decided_at DESC) WHERE state = 'dismissed';

-- Parent history of "Open TVs": who opened, extended or closed which TV, and until when.
-- Expiry is not an event. An open that ended a session links it. via_credential is the
-- assistant credential that acted for the adult (no foreign key: history outlives credentials).
CREATE TABLE tv_open_event (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id     uuid NOT NULL,
    tv_id            uuid NOT NULL,
    action           text NOT NULL CHECK (action IN ('open', 'extend', 'close')),
    until            timestamptz,
    at               timestamptz NOT NULL CHECK (isfinite(at)),
    by_adult         uuid,
    via_credential   uuid,
    ended_session_id uuid,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, tv_id) REFERENCES tv (household_id, id),
    FOREIGN KEY (household_id, ended_session_id) REFERENCES tv_session (household_id, id),
    CHECK ((action = 'close') = (until IS NULL) AND (until IS NULL OR (isfinite(until) AND until > at))),
    CHECK (ended_session_id IS NULL OR action = 'open')
);
CREATE INDEX tv_open_event_at_idx ON tv_open_event (household_id, at DESC, id DESC);

-- +goose Down
DROP TABLE tv_open_event;
DROP TABLE tv_prompt;
DROP TABLE tv_session;
DROP TABLE tv_credit;
DROP TABLE tv_mac;
DROP TABLE tv;
