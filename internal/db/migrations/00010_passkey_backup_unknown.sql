-- +goose Up

-- The old app never stored backup eligibility, so an imported passkey's is unknown (NULL) until
-- its first sign-in, which records the flag from that signature-checked assertion.
ALTER TABLE passkey ALTER COLUMN backup_eligible DROP NOT NULL;

-- +goose Down
UPDATE passkey SET backup_eligible = backed_up WHERE backup_eligible IS NULL;
ALTER TABLE passkey ALTER COLUMN backup_eligible SET NOT NULL;
