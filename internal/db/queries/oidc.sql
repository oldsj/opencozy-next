-- name: CreateOIDCSignIn :exec
INSERT INTO oidc_sign_in (state_hash, binding_hash, code_verifier, nonce, expires_at)
VALUES ($1, $2, $3, $4, now() + interval '10 minutes');

-- name: TakeOIDCSignIn :one
-- Works once: the row is gone after this, whatever the callback's outcome.
DELETE FROM oidc_sign_in
WHERE state_hash = $1 AND expires_at > now()
RETURNING binding_hash, code_verifier, nonce;

-- name: DeleteExpiredOIDCSignIns :execrows
DELETE FROM oidc_sign_in WHERE expires_at <= now();

-- name: GetAdultByOIDCSubject :one
-- Like GetAdultByEmail: only a current adult in a live household can sign in.
SELECT a.* FROM oidc_link l
JOIN adult a ON a.household_id = l.household_id AND a.id = l.adult_id
JOIN household h ON h.id = a.household_id
WHERE l.issuer = $1 AND l.subject = $2 AND a.removed_at IS NULL AND h.archived_at IS NULL;

-- name: GetOIDCLinkForAdult :one
SELECT * FROM oidc_link WHERE household_id = $1 AND adult_id = $2 AND issuer = $3;

-- name: CreateOIDCLink :execrows
-- Nothing when the adult already has a link for this issuer, or the subject is linked to
-- someone else (the caller then refuses the sign-in).
INSERT INTO oidc_link (household_id, adult_id, issuer, subject, email, last_used_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT DO NOTHING;

-- name: UseOIDCLink :exec
UPDATE oidc_link SET last_used_at = now() WHERE household_id = $1 AND adult_id = $2 AND issuer = $3;

-- name: DeleteOIDCLink :execrows
DELETE FROM oidc_link WHERE household_id = $1 AND adult_id = $2 AND issuer = $3;
