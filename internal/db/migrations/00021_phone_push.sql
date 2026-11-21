-- +goose Up
-- Slice 11: parent phone sessions and push (design §3, §8).

-- A phone session holds at most one APNs device token, and a token belongs to at most one live
-- session or device: it names one app install, so registering it moves it there. Revoking a
-- session clears its token. Sessions last at most 90 days.
ALTER TABLE phone_session
    ADD COLUMN last_used_at       timestamptz,
    ADD COLUMN push_token         text,
    ADD COLUMN push_environment   text,
    ADD COLUMN push_registered_at timestamptz,
    ADD CONSTRAINT phone_session_expiry CHECK (expires_at > created_at AND expires_at <= created_at + interval '90 days'),
    ADD CONSTRAINT phone_session_push CHECK (
        (push_token IS NULL AND push_environment IS NULL AND push_registered_at IS NULL)
        OR (push_token ~ '^[a-f0-9]{64,200}$' AND push_environment IN ('development', 'production')
            AND push_registered_at IS NOT NULL AND revoked_at IS NULL));
CREATE UNIQUE INDEX phone_session_push_token_key ON phone_session (push_token) WHERE push_token IS NOT NULL;
CREATE INDEX phone_session_household_idx ON phone_session (household_id, adult_id);

-- The kid app's token: pushes for the device's current child.
ALTER TABLE device
    ADD COLUMN push_token         text,
    ADD COLUMN push_environment   text,
    ADD COLUMN push_registered_at timestamptz,
    ADD CONSTRAINT device_push CHECK (
        (push_token IS NULL AND push_environment IS NULL AND push_registered_at IS NULL)
        OR (push_token ~ '^[a-f0-9]{64,200}$' AND push_environment IN ('development', 'production')
            AND push_registered_at IS NOT NULL AND revoked_at IS NULL));
CREATE UNIQUE INDEX device_push_token_key ON device (push_token) WHERE push_token IS NOT NULL;

-- Old #87: while this is in the future, the adult's phones get no alerts. Silent updates (an
-- answered item leaving, the badge) still arrive. Each adult sets their own.
ALTER TABLE adult ADD COLUMN push_muted_until timestamptz;

-- +goose Down
ALTER TABLE adult DROP COLUMN push_muted_until;
DROP INDEX device_push_token_key;
ALTER TABLE device DROP CONSTRAINT device_push,
    DROP COLUMN push_registered_at, DROP COLUMN push_environment, DROP COLUMN push_token;
DROP INDEX phone_session_household_idx;
DROP INDEX phone_session_push_token_key;
ALTER TABLE phone_session DROP CONSTRAINT phone_session_push, DROP CONSTRAINT phone_session_expiry,
    DROP COLUMN push_registered_at, DROP COLUMN push_environment, DROP COLUMN push_token, DROP COLUMN last_used_at;
