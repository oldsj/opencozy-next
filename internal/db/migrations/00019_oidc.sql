-- +goose Up

-- Single sign-on with an OpenID Connect provider (design §6, slice 10b). A link ties one
-- provider account (issuer, subject) to an existing adult. It is made on the adult's first
-- sign-in with the provider, only when the provider reports the adult's own email as verified;
-- later sign-ins match the subject. Nobody is ever created. email is the provider's email when
-- the link was made (the adult's email for a link carried by the importer).
CREATE TABLE oidc_link (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    household_id uuid NOT NULL,
    adult_id     uuid NOT NULL,
    issuer       text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 2048),
    subject      text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    email        text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    UNIQUE (issuer, subject),
    -- One provider account per adult and issuer: a second one is refused, not added.
    UNIQUE (adult_id, issuer),
    FOREIGN KEY (household_id, adult_id) REFERENCES adult (household_id, id) ON DELETE CASCADE
);
CREATE INDEX oidc_link_household_adult_idx ON oidc_link (household_id, adult_id);

-- Sign-ins in progress. Each lasts 10 minutes and works once: the callback deletes it. The state
-- sent to the provider and the browser's binding cookie are stored as SHA-256, so a row can't be
-- finished from the database alone; the PKCE verifier and nonce are needed in the clear.
CREATE TABLE oidc_sign_in (
    state_hash    bytea PRIMARY KEY CHECK (length(state_hash) = 32),
    binding_hash  bytea NOT NULL CHECK (length(binding_hash) = 32),
    code_verifier text NOT NULL CHECK (length(code_verifier) BETWEEN 43 AND 128),
    nonce         text NOT NULL CHECK (length(nonce) BETWEEN 22 AND 128),
    expires_at    timestamptz NOT NULL
);
CREATE INDEX oidc_sign_in_expires_at_idx ON oidc_sign_in (expires_at);

-- +goose Down
DROP TABLE oidc_sign_in;
DROP TABLE oidc_link;
