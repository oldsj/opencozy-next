-- +goose Up
-- Pairing a wall display. The display asks for a pairing and shows its code; a signed-in adult
-- enters the code (with a recent sign-in), which creates the display in their household; the
-- display then claims its session with the secret in its pairing cookie. Only hashes are stored.
-- A pairing lasts 10 minutes and is deleted when claimed.
CREATE TABLE display_pairing (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    claim_hash   bytea NOT NULL UNIQUE CHECK (length(claim_hash) = 32),
    code_hash    bytea NOT NULL UNIQUE CHECK (length(code_hash) = 32),
    household_id uuid,
    display_id   uuid,
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at   timestamptz NOT NULL,
    FOREIGN KEY (household_id, display_id) REFERENCES display (household_id, id) ON DELETE CASCADE,
    CHECK ((household_id IS NULL) = (display_id IS NULL))
);
CREATE INDEX display_pairing_expires_at_idx ON display_pairing (expires_at);

-- +goose Down
DROP TABLE display_pairing;
