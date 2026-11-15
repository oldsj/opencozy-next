-- +goose Up
-- The initial schema. uuidv7() is built in from PostgreSQL 18: time-ordered ids index better.

CREATE TABLE household (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    time_zone   text NOT NULL CHECK (length(time_zone) BETWEEN 1 AND 64),
    archived_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Adults are never deleted (later tables record who decided what); removing one sets removed_at.
CREATE TABLE adult (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id         uuid NOT NULL REFERENCES household (id),
    email                text NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    name                 text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    -- argon2id for new passwords; scrypt-ba for hashes imported from the old app, replaced by
    -- argon2id at the next successful sign-in. NULL when the adult signs in only by passkey or OIDC.
    password_hash        text,
    password_scheme      text CHECK (password_scheme IN ('argon2id', 'scrypt-ba')),
    -- The WebAuthn user handle: the old text ID's bytes for imported adults, random otherwise.
    webauthn_user_handle bytea NOT NULL UNIQUE CHECK (length(webauthn_user_handle) BETWEEN 1 AND 64),
    removed_at           timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id),
    CHECK ((password_hash IS NULL) = (password_scheme IS NULL))
);

-- Web sessions. Only the SHA-256 of the cookie's token is stored. authenticated_at is the last
-- password (or passkey) check, for operations that need a recent sign-in.
CREATE TABLE adult_session (
    token_hash       bytea PRIMARY KEY CHECK (length(token_hash) = 32),
    adult_id         uuid NOT NULL REFERENCES adult (id) ON DELETE CASCADE,
    authenticated_at timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL
);
CREATE INDEX adult_session_adult_id_idx ON adult_session (adult_id);
CREATE INDEX adult_session_expires_at_idx ON adult_session (expires_at);

CREATE TABLE child (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    archived_at  timestamptz,
    version      integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (household_id, id)
);

-- Service (ocz_) and assistant (oca_) bearer credentials. token_hash is NULL for a credential
-- imported for reissue: it exists (name, scopes, children) but cannot authenticate until a new
-- secret is issued.
CREATE TABLE credential (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    kind         text NOT NULL CHECK (kind IN ('service', 'assistant')),
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    token_hash   bytea UNIQUE CHECK (length(token_hash) = 32),
    scopes       text[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 32),
    all_children boolean NOT NULL DEFAULT false,
    -- The adult an assistant acts for (it stops when they are removed); for a service
    -- credential, who made it.
    adult_id     uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    revoked_at   timestamptz,
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id),
    CHECK (kind <> 'assistant' OR adult_id IS NOT NULL)
);

-- The children a service credential may act for, when not all_children. Composite keys keep
-- both sides in the same household.
CREATE TABLE credential_child (
    household_id  uuid NOT NULL,
    credential_id uuid NOT NULL,
    child_id      uuid NOT NULL,
    PRIMARY KEY (credential_id, child_id),
    FOREIGN KEY (household_id, credential_id) REFERENCES credential (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, child_id) REFERENCES child (household_id, id)
);

-- Parent phone sessions (ocp_ bearer), one per signed-in phone.
CREATE TABLE phone_session (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    adult_id     uuid NOT NULL,
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    token_hash   bytea UNIQUE CHECK (length(token_hash) = 32),
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id)
);

-- Wall displays (__Host-display cookie). A display can call only operations whose display
-- scopes it holds; an imported display keeps exactly its old scopes.
CREATE TABLE display (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL REFERENCES household (id),
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    scopes       text[] NOT NULL CHECK (scopes <@ ARRAY['today', 'tv', 'calendar', 'meals', 'dashboard']),
    token_hash   bytea UNIQUE CHECK (length(token_hash) = 32),
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    UNIQUE (household_id, id)
);

-- Idempotent commands: a retried request with the same Idempotency-Key gets the first answer.
-- request_hash covers the actor, the operation and the body, so a key reused for something else
-- is refused. Rows expire after 30 days.
CREATE TABLE command_log (
    household_id    uuid NOT NULL REFERENCES household (id),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_hash    bytea NOT NULL CHECK (length(request_hash) = 32),
    status          integer NOT NULL,
    content_type    text NOT NULL,
    body            bytea NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (household_id, idempotency_key)
);
CREATE INDEX command_log_created_at_idx ON command_log (created_at);

-- +goose Down
DROP TABLE display;
DROP TABLE phone_session;
DROP TABLE command_log;
DROP TABLE credential_child;
DROP TABLE credential;
DROP TABLE child;
DROP TABLE adult_session;
DROP TABLE adult;
DROP TABLE household;
