package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/keyring"
)

// googleTokenAAD binds a refresh token's ciphertext to its connection row.
func googleTokenAAD(householdID, connectionID uuid.UUID) keyring.AAD {
	return keyring.AAD{HouseholdID: householdID.String(), Table: "calendar_google_connection", RowID: connectionID.String(), Purpose: "refresh_token"}
}

// googleVerifierAAD binds a PKCE verifier's ciphertext to its connect-in-progress row.
func googleVerifierAAD(householdID, stateID uuid.UUID) keyring.AAD {
	return keyring.AAD{HouseholdID: householdID.String(), Table: "google_oauth_state", RowID: stateID.String(), Purpose: "verifier"}
}

// SealGoogleToken encrypts a refresh token for its connection row.
func SealGoogleToken(kr *keyring.Keyring, householdID, connectionID uuid.UUID, token string) (string, error) {
	return kr.Encrypt(googleTokenAAD(householdID, connectionID), []byte(token))
}

// OpenGoogleToken decrypts a connection's refresh token.
func OpenGoogleToken(kr *keyring.Keyring, householdID, connectionID uuid.UUID, sealed string) (string, error) {
	b, err := kr.Decrypt(googleTokenAAD(householdID, connectionID), sealed)
	return string(b), err
}

// SealGoogleVerifier encrypts a PKCE verifier for its connect-in-progress row.
func SealGoogleVerifier(kr *keyring.Keyring, householdID, stateID uuid.UUID, verifier string) (string, error) {
	return kr.Encrypt(googleVerifierAAD(householdID, stateID), []byte(verifier))
}

// OpenGoogleVerifier decrypts a PKCE verifier.
func OpenGoogleVerifier(kr *keyring.Keyring, householdID, stateID uuid.UUID, sealed string) (string, error) {
	b, err := kr.Decrypt(googleVerifierAAD(householdID, stateID), sealed)
	return string(b), err
}

func init() {
	keyring.Register("calendar_google_connection.refresh_token", rekeyGoogleTokens)
	keyring.Register("google_oauth_state.verifier", rekeyGoogleVerifiers)
}

func rekeyGoogleTokens(ctx context.Context, pool *pgxpool.Pool, kr *keyring.Keyring) (int, error) {
	q := dbq.New(pool)
	rows, err := q.ListGoogleRefreshTokens(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, r := range rows {
		sealed, did, err := kr.Rekey(googleTokenAAD(r.HouseholdID, r.ID), r.RefreshToken)
		if err != nil {
			return changed, fmt.Errorf("calendar_google_connection %s: %w", r.ID, err)
		}
		if !did {
			continue
		}
		if err := q.SetGoogleRefreshToken(ctx, dbq.SetGoogleRefreshTokenParams{HouseholdID: r.HouseholdID, ID: r.ID, NewToken: &sealed, ReadToken: &r.RefreshToken}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func rekeyGoogleVerifiers(ctx context.Context, pool *pgxpool.Pool, kr *keyring.Keyring) (int, error) {
	q := dbq.New(pool)
	rows, err := q.ListGoogleOAuthStates(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, r := range rows {
		sealed, did, err := kr.Rekey(googleVerifierAAD(r.HouseholdID, r.ID), r.Verifier)
		if err != nil {
			return changed, fmt.Errorf("google_oauth_state %s: %w", r.ID, err)
		}
		if !did {
			continue
		}
		if err := q.SetGoogleOAuthStateVerifier(ctx, dbq.SetGoogleOAuthStateVerifierParams{ID: r.ID, Verifier: sealed}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// syncGoogle refreshes one Google calendar. The sync token only says whether anything changed:
// unchanged keeps the stored events; any change, an expired token (410) or a new window
// fetches the whole window again and replaces them, as for an ICS feed.
func syncGoogle(ctx context.Context, fence Txer, kr *keyring.Keyring, g *Google, log *slog.Logger, now time.Time,
	row dbq.ClaimDueFeedsRow, zone *time.Location, w Window, failed func(string, error) error) error {
	if g == nil {
		return failed(CodeAccessDenied, errors.New("google calendar is not configured"))
	}
	if kr == nil || row.GoogleConnectionID == nil || row.GoogleCalendarID == nil {
		return failed(CodeInvalidCalendar, errors.New("no keyring or Google calendar"))
	}
	var conn dbq.CalendarGoogleConnection
	err := fence.Tx(ctx, func(q *dbq.Queries) (err error) {
		conn, err = q.GetGoogleConnection(ctx, dbq.GetGoogleConnectionParams{HouseholdID: row.HouseholdID, ID: *row.GoogleConnectionID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // disconnected meanwhile; its sources went with it
	} else if err != nil {
		return err
	}
	if conn.NeedsReconnect || conn.RefreshToken == nil {
		return failed(CodeTokenExpired, errors.New("the Google account needs connecting again"))
	}
	refresh, err := OpenGoogleToken(kr, conn.HouseholdID, conn.ID, *conn.RefreshToken)
	if err != nil {
		log.ErrorContext(ctx, "Google refresh token does not decrypt", "connection", conn.ID, "err", err)
		return failed(CodeTokenExpired, err)
	}
	access, err := g.Access(ctx, refresh)
	if errors.Is(err, ErrReconnect) {
		if err := fence.Tx(ctx, func(q *dbq.Queries) error {
			return q.GoogleConnectionNeedsReconnect(ctx, dbq.GoogleConnectionNeedsReconnectParams{HouseholdID: conn.HouseholdID, ID: conn.ID, ReadToken: conn.RefreshToken})
		}); err != nil {
			return err
		}
		return failed(CodeTokenExpired, err)
	} else if err != nil {
		return failed(CodeOf(err), err)
	}
	if access.Rotated != "" {
		sealed, err := SealGoogleToken(kr, conn.HouseholdID, conn.ID, access.Rotated)
		if err != nil {
			return err
		}
		if err := fence.Tx(ctx, func(q *dbq.Queries) error {
			return q.SetGoogleRefreshToken(ctx, dbq.SetGoogleRefreshTokenParams{HouseholdID: conn.HouseholdID, ID: conn.ID, NewToken: &sealed, ReadToken: conn.RefreshToken})
		}); err != nil {
			return err
		}
	}
	calendarID := *row.GoogleCalendarID
	if row.WindowStart.Valid && dateOf(row.WindowStart) == w.From && row.GoogleSyncToken != nil {
		changed, next, err := g.Changed(ctx, access.Token, calendarID, *row.GoogleSyncToken)
		switch {
		case errors.Is(err, errSyncTokenGone): // fetch everything below
		case err != nil:
			return failed(CodeOf(err), err)
		case !changed:
			return fence.Tx(ctx, func(q *dbq.Queries) error {
				if err := q.FeedUnchanged(ctx, dbq.FeedUnchangedParams{ID: row.ID}); err != nil {
					return err
				}
				return q.SetGoogleSyncToken(ctx, dbq.SetGoogleSyncTokenParams{ID: row.ID, SyncToken: &next})
			})
		}
	}
	feed, next, err := g.Events(ctx, access.Token, calendarID, zone, w)
	if err != nil {
		return failed(CodeOf(err), err)
	}
	return fence.Tx(ctx, func(q *dbq.Queries) error {
		if err := store(ctx, q, row, feed, Fetched{}, "", w, now); err != nil {
			return err
		}
		return q.SetGoogleSyncToken(ctx, dbq.SetGoogleSyncTokenParams{ID: row.ID, SyncToken: nonEmpty(next)})
	})
}
