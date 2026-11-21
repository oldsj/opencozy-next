package importer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type adultRef struct {
	id, household uuid.UUID
}

// importAdults turns Better Auth users who belong to a household into adults (design §6): a new
// UUID each, the old text ID's bytes as the WebAuthn user handle, the email lowercased, and the
// password hash kept as scrypt-ba (rehashed to argon2id at the next sign-in). Users with no
// household are not carried. OIDC accounts become single sign-on links (importOIDCLinks); their
// token columns are never read.
func importAdults(ctx context.Context, x *run) error {
	x.r.columnsRead("auth_user", "id", "name", "email", "created_at", "email_verified", "image", "updated_at")
	x.r.columnsRead("household_owner", "user_id", "household_id", "created_at")
	x.r.columnsRead("auth_account", "user_id", "provider_id", "password",
		"id", "account_id", "access_token", "refresh_token", "id_token", "access_token_expires_at",
		"refresh_token_expires_at", "scope", "created_at", "updated_at")

	rows, err := x.src.Query(ctx, `
		SELECT u.id, u.name, u.email, u.created_at, o.household_id, a.password
		FROM opencozy.auth_user u
		JOIN opencozy.household_owner o ON o.user_id = u.id
		LEFT JOIN opencozy.auth_account a ON a.user_id = u.id AND a.provider_id = 'credential' AND a.password IS NOT NULL
		ORDER BY u.created_at, u.id`)
	if err != nil {
		return safe(err)
	}
	var written, withPassword int64
	for rows.Next() {
		var (
			oldID, name, email string
			created            time.Time
			household          uuid.UUID
			password           *string
		)
		if err := rows.Scan(&oldID, &name, &email, &created, &household, &password); err != nil {
			return safe(err)
		}
		if _, dup := x.adults[oldID]; dup {
			return fmt.Errorf("import: an old user belongs to two households or has two password accounts")
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		var scheme *string
		if password != nil {
			s := "scrypt-ba"
			scheme = &s
			withPassword++
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO adult (id, household_id, email, name, password_hash, password_scheme, webauthn_user_handle, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, household, strings.ToLower(email), name, password, scheme, []byte(oldID), created); err != nil {
			return safe(err)
		}
		x.adults[oldID] = adultRef{id: id, household: household}
		written++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}

	users, err := count(ctx, x.src, "auth_user")
	if err != nil {
		return err
	}
	owners, err := count(ctx, x.src, "household_owner")
	if err != nil {
		return err
	}
	accounts, err := count(ctx, x.src, "auth_account")
	if err != nil {
		return err
	}
	adults, err := countNew(ctx, x.dst, "adult")
	if err != nil {
		return err
	}
	var passwords int64
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM adult WHERE password_hash IS NOT NULL`).Scan(&passwords); err != nil {
		return safe(err)
	}
	x.r.table("auth_user", users, "adult", written, adults)
	if users > written {
		x.r.note("auth_user", fmt.Sprintf("%d without a household, not carried", users-written))
	}
	x.r.table("household_owner", owners, "adult", owners, adults)
	links, skipped, err := importOIDCLinks(ctx, x)
	if err != nil {
		return err
	}
	linked, err := countNew(ctx, x.dst, "oidc_link")
	if err != nil {
		return err
	}
	x.r.table("auth_account", accounts, "adult (password) + oidc_link", withPassword+links, passwords+linked)
	if skipped > 0 {
		x.r.note("auth_account", fmt.Sprintf("%d other accounts in all, not carried (the notes above, and users with no household)", skipped))
	}
	return nil
}

// importOIDCLinks carries the old app's OIDC accounts (provider "oidc", whose account ID is the
// provider's subject) as single sign-on links for OIDC_ISSUER, the provider the old app used.
// The link's email is the adult's own, the one it was made with. Without OIDC_ISSUER they are
// not carried, and each adult links again by verified email at their first sign-in. It returns
// the links written and the old non-password accounts not carried.
func importOIDCLinks(ctx context.Context, x *run) (written, skipped int64, err error) {
	rows, err := x.src.Query(ctx, `
		SELECT user_id, provider_id, account_id, created_at FROM opencozy.auth_account
		WHERE provider_id <> 'credential' ORDER BY created_at, id`)
	if err != nil {
		return 0, 0, safe(err)
	}
	defer rows.Close()
	var noIssuer, otherProvider, twice int64
	for rows.Next() {
		var (
			user, provider, subject string
			created                 time.Time
		)
		if err := rows.Scan(&user, &provider, &subject, &created); err != nil {
			return 0, 0, safe(err)
		}
		a, ok := x.adults[user]
		switch {
		case !ok:
			skipped++ // a user with no household: not an adult here
			continue
		case provider != "oidc":
			otherProvider++
			skipped++
			continue
		case x.oidcIssuer == "":
			noIssuer++
			skipped++
			continue
		}
		// An adult with two accounts at the provider keeps the first: the new app allows one.
		tag, err := x.dst.Exec(ctx, `INSERT INTO oidc_link (household_id, adult_id, issuer, subject, email, created_at)
			SELECT household_id, id, $3, $4, email, $5 FROM adult WHERE household_id = $1 AND id = $2
			ON CONFLICT (adult_id, issuer) DO NOTHING`, a.household, a.id, x.oidcIssuer, subject, created)
		if err != nil {
			return 0, 0, safe(err)
		}
		if tag.RowsAffected() == 0 {
			twice++
			skipped++
			continue
		}
		written++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, safe(err)
	}
	if noIssuer > 0 {
		x.r.note("auth_account", fmt.Sprintf("%d OIDC links left behind because OIDC_ISSUER isn't set; those adults link again by verified email at their first single sign-on", noIssuer))
	}
	if otherProvider > 0 {
		x.r.note("auth_account", fmt.Sprintf("%d accounts of an unknown provider, not carried", otherProvider))
	}
	if twice > 0 {
		x.r.note("auth_account", fmt.Sprintf("%d second OIDC accounts of an adult, not carried (the first is)", twice))
	}
	return written, skipped, nil
}
