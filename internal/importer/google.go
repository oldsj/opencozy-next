package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/calendar"
	"github.com/oldsj/opencozy-next/internal/importer/oldsecret"
)

// importGoogleConnections carries Google accounts with their IDs, subject, address, scope and
// creation time. With --old-auth-secret-file the refresh token is opened with the old app's key
// and sealed with the keyring; without it, or when the old app had marked the account for
// reconnecting or revoked it, the account imports as "reconnect needed" with no token, and its
// calendars keep their choices until an adult connects it again. Access tokens, refresh leases
// and error times are not carried: the first refresh gets a new access token.
func importGoogleConnections(ctx context.Context, x *run) (map[uuid.UUID]bool, error) {
	x.r.columnsRead("calendar_google_connection", "household_id", "id", "google_subject", "account_email", "status", "scope",
		"refresh_token_encrypted", "credential_version", "created_at",
		// Short-lived, rebuilt by the first refresh:
		"access_token_encrypted", "access_token_expires_at", "last_error_at", "token_refresh_lease_id", "token_refresh_lease_until", "updated_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, google_subject, account_email, status, scope, refresh_token_encrypted, created_at
		FROM opencozy.calendar_google_connection ORDER BY household_id, created_at, id`)
	if err != nil {
		return nil, safe(err)
	}
	type conn struct {
		household, id                 uuid.UUID
		subject, email, status, scope string
		refresh                       string
		created                       time.Time
	}
	var all []conn
	for rows.Next() {
		var c conn
		if err := rows.Scan(&c.household, &c.id, &c.subject, &c.email, &c.status, &c.scope, &c.refresh, &c.created); err != nil {
			return nil, safe(err)
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return nil, safe(err)
	}
	carried := map[uuid.UUID]bool{}
	var n, reconnect int64
	for _, c := range all {
		var sealed *string
		if c.status == "connected" && x.old != nil {
			if x.keys == nil {
				return nil, ErrNoKeys
			}
			token, err := x.old.Decrypt(oldsecret.GoogleToken, c.refresh)
			if errors.Is(err, oldsecret.ErrUnreadable) {
				return nil, fmt.Errorf("import: Google account %s: its token does not open with --old-auth-secret-file; is it the old app's BETTER_AUTH_SECRET?", c.id)
			} else if err != nil {
				return nil, err
			}
			s, err := calendar.SealGoogleToken(x.keys, c.household, c.id, string(token))
			clear(token)
			if err != nil {
				return nil, err
			}
			sealed = &s
		}
		if sealed == nil {
			reconnect++
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO calendar_google_connection (household_id, id, subject, email, scope, refresh_token, needs_reconnect, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, c.household, c.id, strings.TrimSpace(c.subject), strings.ToLower(strings.TrimSpace(c.email)),
			strings.TrimSpace(c.scope), sealed, sealed == nil, c.created); err != nil {
			return nil, fmt.Errorf("import: Google account %s: %w", c.id, safe(err))
		}
		carried[c.id] = true
		n++
	}
	if err := reconcileKind(ctx, x, "calendar_google_connection", n, "calendar_google_connection", 0, ""); err != nil {
		return nil, err
	}
	if reconnect > 0 {
		x.r.note("calendar_google_connection", fmt.Sprintf("%d need connecting again (no old secret, or the old app already needed it)", reconnect))
	}
	return carried, nil
}
