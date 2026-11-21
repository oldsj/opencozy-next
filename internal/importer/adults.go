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
// password hash kept as scrypt-ba (rehashed to argon2id at the next sign-in), and the phone push
// mute (push_muted_until, the same meaning: until then, no alerts). Users with no
// household are not carried. Other accounts (OIDC links) are not carried either: OIDC links to
// an adult by verified email. Their token columns are never read.
func importAdults(ctx context.Context, x *run) error {
	x.r.columnsRead("auth_user", "id", "name", "email", "created_at", "email_verified", "image", "updated_at")
	x.r.columnsRead("household_owner", "user_id", "household_id", "created_at", "push_muted_until")
	x.r.columnsRead("auth_account", "user_id", "provider_id", "password",
		"id", "account_id", "access_token", "refresh_token", "id_token", "access_token_expires_at",
		"refresh_token_expires_at", "scope", "created_at", "updated_at")

	rows, err := x.src.Query(ctx, `
		SELECT u.id, u.name, u.email, u.created_at, o.household_id, a.password, o.push_muted_until
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
			muted              *time.Time
		)
		if err := rows.Scan(&oldID, &name, &email, &created, &household, &password, &muted); err != nil {
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
		if _, err := x.dst.Exec(ctx, `INSERT INTO adult (id, household_id, email, name, password_hash, password_scheme, webauthn_user_handle, created_at, push_muted_until)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, household, strings.ToLower(email), name, password, scheme, []byte(oldID), created, muted); err != nil {
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
	x.r.table("auth_account", accounts, "adult (password)", withPassword, passwords)
	if accounts > withPassword {
		x.r.note("auth_account", fmt.Sprintf("%d other accounts, not carried", accounts-withPassword))
	}
	return nil
}
