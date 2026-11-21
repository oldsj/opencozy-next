-- +goose Up
-- Slice 12: what an assistant does is recorded as its adult, "via" the assistant credential.
-- point_ledger.credential_id already records it for points; decisions and grants record it here.
ALTER TABLE approval
    ADD COLUMN decided_via uuid,
    ADD CONSTRAINT approval_decided_via FOREIGN KEY (household_id, decided_via) REFERENCES credential (household_id, id),
    ADD CONSTRAINT approval_decided_via_by CHECK (decided_via IS NULL OR decided_by IS NOT NULL);
ALTER TABLE screen_time_credit
    ADD COLUMN granted_via uuid,
    ADD CONSTRAINT screen_time_credit_granted_via FOREIGN KEY (household_id, granted_via) REFERENCES credential (household_id, id),
    ADD CONSTRAINT screen_time_credit_granted_via_by CHECK (granted_via IS NULL OR granted_by IS NOT NULL);
-- An assistant acts for every child, as its adult does.
ALTER TABLE credential
    ADD CONSTRAINT credential_assistant_all_children CHECK (kind <> 'assistant' OR all_children);

-- +goose Down
ALTER TABLE credential DROP CONSTRAINT credential_assistant_all_children;
ALTER TABLE screen_time_credit
    DROP CONSTRAINT screen_time_credit_granted_via_by,
    DROP CONSTRAINT screen_time_credit_granted_via,
    DROP COLUMN granted_via;
ALTER TABLE approval
    DROP CONSTRAINT approval_decided_via_by,
    DROP CONSTRAINT approval_decided_via,
    DROP COLUMN decided_via;
