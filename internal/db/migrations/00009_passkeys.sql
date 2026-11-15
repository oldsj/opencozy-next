-- +goose Up

-- An adult's passkeys (design §6). The user handle is adult.webauthn_user_handle; imported
-- credentials keep their ID, public key, sign counter, transports and backup flags.
CREATE TABLE passkey (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id    uuid NOT NULL,
    adult_id        uuid NOT NULL,
    credential_id   bytea NOT NULL UNIQUE CHECK (length(credential_id) BETWEEN 16 AND 1023),
    public_key      bytea NOT NULL CHECK (length(public_key) BETWEEN 1 AND 2048),
    attestation_type text NOT NULL DEFAULT 'none' CHECK (length(attestation_type) <= 40),
    aaguid          bytea CHECK (aaguid IS NULL OR length(aaguid) = 16),
    sign_count      bigint NOT NULL DEFAULT 0 CHECK (sign_count BETWEEN 0 AND 4294967295),
    transports      text[] NOT NULL DEFAULT '{}' CHECK (cardinality(transports) <= 8),
    backup_eligible boolean NOT NULL,
    backed_up       boolean NOT NULL,
    name            text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id) ON DELETE CASCADE
);
CREATE INDEX passkey_adult_idx ON passkey (household_id, adult_id);

-- WebAuthn ceremonies in progress. Each lasts 5 minutes and works once: finishing deletes it.
-- adult_id is the signed-in adult for adding a passkey or confirming a sign-in, NULL to sign in.
CREATE TABLE webauthn_challenge (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    purpose    text NOT NULL CHECK (purpose IN ('register', 'sign_in', 'confirm')),
    adult_id   uuid REFERENCES adult (id) ON DELETE CASCADE,
    session    jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK ((purpose = 'sign_in') = (adult_id IS NULL))
);
CREATE INDEX webauthn_challenge_expires_at_idx ON webauthn_challenge (expires_at);

-- +goose Down
DROP TABLE webauthn_challenge;
DROP TABLE passkey;
