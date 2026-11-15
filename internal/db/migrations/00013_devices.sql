-- +goose Up
-- Kid devices (slice 7). An adult offers a pairing for one child; the device redeems it with a
-- P-256 key it made and then signs every request with that key (no bearer secret exists). A
-- device whose child is archived stays listed, but can't act until an adult reassigns it.

-- The message a locked device shows; NULL means the app's default.
ALTER TABLE household ADD COLUMN lock_message text
    CHECK (lock_message IS NULL OR length(btrim(lock_message)) BETWEEN 1 AND 200);

CREATE TABLE device (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    child_id     uuid,
    name         text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    platform     text NOT NULL CHECK (platform IN ('ios', 'android')),
    -- The uncompressed P-256 point (X9.63): 0x04 || X || Y.
    public_key   bytea NOT NULL UNIQUE CHECK (length(public_key) = 65 AND get_byte(public_key, 0) = 4),
    paired_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz,
    revoked_at   timestamptz,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);
CREATE INDEX device_household_idx ON device (household_id) WHERE revoked_at IS NULL;

-- An adult's offer: 5 minutes, once. Only hashes of the token (QR) and code (typed) are stored.
CREATE TABLE device_pairing (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    child_id     uuid NOT NULL,
    token_hash   bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    code_hash    bytea NOT NULL UNIQUE CHECK (length(code_hash) = 32),
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at   timestamptz NOT NULL,
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);
CREATE INDEX device_pairing_expires_at_idx ON device_pairing (expires_at);

-- Nonces of signed requests, kept 120 seconds against replay.
CREATE TABLE device_nonce (
    device_id uuid NOT NULL REFERENCES device (id) ON DELETE CASCADE,
    nonce     text NOT NULL CHECK (nonce ~ '^[A-Za-z0-9_-]{16,64}$'),
    seen_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (device_id, nonce)
);
CREATE INDEX device_nonce_seen_at_idx ON device_nonce (seen_at);

-- The kid view: a one-time link (60 seconds) the app opens in its web view, exchanged for a
-- 24-hour web session in the __Host-device cookie.
CREATE TABLE device_web_link (
    token_hash bytea PRIMARY KEY CHECK (length(token_hash) = 32),
    device_id  uuid NOT NULL REFERENCES device (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE TABLE device_web_session (
    token_hash bytea PRIMARY KEY CHECK (length(token_hash) = 32),
    device_id  uuid NOT NULL REFERENCES device (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX device_web_session_device_idx ON device_web_session (device_id);

-- +goose Down
DROP TABLE device_web_session;
DROP TABLE device_web_link;
DROP TABLE device_nonce;
DROP TABLE device_pairing;
DROP TABLE device;
ALTER TABLE household DROP COLUMN lock_message;
