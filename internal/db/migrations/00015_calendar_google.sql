-- +goose Up
-- Google calendars (slice 13b). An adult connects a Google account once (OAuth with PKCE); the
-- household then follows any of that account's calendars as calendar sources. The refresh
-- token is internal/keyring ciphertext bound to its row and never returned. Disconnecting
-- deletes the connection and, by cascade, every calendar followed through it.

CREATE TABLE calendar_google_connection (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id    uuid NOT NULL REFERENCES household (id),
    -- Google's stable account id (the OpenID "sub") and the address shown to adults.
    subject         text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    email           text NOT NULL CHECK (length(email) BETWEEN 1 AND 320),
    scope           text NOT NULL CHECK (length(scope) BETWEEN 1 AND 2000),
    -- NULL only when an adult has to connect again: an import without the old app's secret.
    refresh_token   text CHECK (length(refresh_token) BETWEEN 1 AND 8192),
    -- Google refused the refresh token (revoked, expired or the password changed): an adult
    -- has to connect the account again. Its calendars keep their events meanwhile.
    needs_reconnect boolean NOT NULL DEFAULT false,
    CHECK (refresh_token IS NOT NULL OR needs_reconnect),
    version         integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (household_id, id),
    UNIQUE (household_id, subject)
);

-- A connect in progress: the state sent to Google is the capability, so only its hash is
-- stored. One use, ten minutes, and only for the adult who started it.
CREATE TABLE google_oauth_state (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    state_hash   text NOT NULL UNIQUE CHECK (state_hash ~ '^[a-f0-9]{64}$'),
    household_id uuid NOT NULL REFERENCES household (id),
    adult_id     uuid NOT NULL,
    -- The PKCE verifier, internal/keyring ciphertext.
    verifier     text NOT NULL CHECK (length(verifier) BETWEEN 1 AND 1024),
    expires_at   timestamptz NOT NULL,
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id) ON DELETE CASCADE
);
CREATE INDEX google_oauth_state_expires_idx ON google_oauth_state (expires_at);

ALTER TABLE calendar_source
    ADD COLUMN google_connection_id uuid,
    -- Google's calendar id, opaque here.
    ADD COLUMN google_calendar_id   text CHECK (length(google_calendar_id) BETWEEN 1 AND 1024),
    -- The events.list sync token: only a change detector (see internal/calendar/google.go).
    ADD COLUMN google_sync_token    text CHECK (length(google_sync_token) BETWEEN 1 AND 4096),
    ADD FOREIGN KEY (household_id, google_connection_id)
        REFERENCES calendar_google_connection (household_id, id) ON DELETE CASCADE,
    ADD CHECK ((provider = 'google') = (google_connection_id IS NOT NULL AND google_calendar_id IS NOT NULL)),
    ADD UNIQUE (household_id, google_connection_id, google_calendar_id);
CREATE INDEX calendar_source_google_connection_idx ON calendar_source (google_connection_id)
    WHERE google_connection_id IS NOT NULL;

-- +goose Down
DELETE FROM calendar_source WHERE provider = 'google';
ALTER TABLE calendar_source
    DROP COLUMN google_sync_token,
    DROP COLUMN google_calendar_id,
    DROP COLUMN google_connection_id;
DROP TABLE google_oauth_state;
DROP TABLE calendar_google_connection;
