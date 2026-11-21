package server

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/calendar"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
)

const (
	maxGoogleConnections = 10
	googleConnectTTL     = 10 * time.Minute
	googleRevokeTimeout  = 5 * time.Second
)

// googleAPI is what the handlers need from Google (calendar.Google; a stand-in in tests).
type googleAPI interface {
	AuthURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (calendar.GoogleAccount, error)
	Access(ctx context.Context, refreshToken string) (calendar.Access, error)
	Calendars(ctx context.Context, accessToken string) ([]calendar.GoogleCalendar, error)
	Revoke(ctx context.Context, token string) error
}

// UseGoogle turns Google calendar on (main passes it when GOOGLE_CLIENT_ID is set).
func (s *Server) UseGoogle(g *calendar.Google) {
	if g != nil {
		s.google = g
	}
}

var (
	errGoogleOff       = apiError("not_configured", "This server isn't set up for Google calendars: GOOGLE_CLIENT_ID is not set.")
	errReconnectNeeded = apiError("reconnect_needed", "Google no longer accepts this account. Connect it again.")
)

// ConnectGoogle starts a connect: a one-use state for this adult (only its hash is stored) and
// a PKCE verifier (sealed), then Google's consent page.
func (s *Server) ConnectGoogle(ctx context.Context, _ api.ConnectGoogleRequestObject) (api.ConnectGoogleResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	kr := s.cfg.SecretKeys
	if s.google == nil || kr == nil {
		return api.ConnectGoogle422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errGoogleOff)}, nil
	}
	if err := cmd.Q.DeleteExpiredGoogleOAuthStates(ctx); err != nil {
		return nil, err
	}
	state, hash, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()
	sealed, err := calendar.SealGoogleVerifier(kr, a.HouseholdID, id, verifier)
	if err != nil {
		return nil, err
	}
	if err := cmd.Q.CreateGoogleOAuthState(ctx, dbq.CreateGoogleOAuthStateParams{ID: id, StateHash: hex.EncodeToString(hash),
		HouseholdID: a.HouseholdID, AdultID: a.AdultID, Verifier: sealed, ExpiresAt: time.Now().Add(googleConnectTTL)}); err != nil {
		return nil, err
	}
	return api.ConnectGoogle200JSONResponse{AuthorizeUrl: s.google.AuthURL(state, verifier)}, nil
}

// GoogleCallback finishes a connect this adult started and sends the browser back to the
// calendars screen with the outcome.
func (s *Server) GoogleCallback(ctx context.Context, req api.GoogleCallbackRequestObject) (api.GoogleCallbackResponseObject, error) {
	a := mustActor(ctx)
	back := func(outcome string, connection ...uuid.UUID) api.GoogleCallbackResponseObject {
		q := url.Values{"google": {outcome}}
		if len(connection) > 0 {
			q.Set("connection", connection[0].String())
		}
		return api.GoogleCallback303Response{Headers: api.GoogleCallback303ResponseHeaders{Location: "/calendars?" + q.Encode()}}
	}
	kr, p := s.cfg.SecretKeys, req.Params
	if s.google == nil || kr == nil {
		return back("failed"), nil
	}
	if p.State == nil || !auth.WellFormedCookie(*p.State) {
		return back("expired"), nil
	}
	st, err := s.q.TakeGoogleOAuthState(ctx, dbq.TakeGoogleOAuthStateParams{StateHash: hex.EncodeToString(auth.HashToken(*p.State)),
		HouseholdID: a.HouseholdID, AdultID: a.AdultID})
	if errors.Is(err, pgx.ErrNoRows) {
		return back("expired"), nil
	} else if err != nil {
		return nil, err
	}
	switch {
	case p.Error != nil && *p.Error == "access_denied":
		return back("denied"), nil
	case p.Error != nil || p.Code == nil || *p.Code == "":
		return back("failed"), nil
	}
	verifier, err := calendar.OpenGoogleVerifier(kr, a.HouseholdID, st.ID, st.Verifier)
	if err != nil {
		return nil, err
	}
	acct, err := s.google.Exchange(ctx, *p.Code, verifier)
	if errors.Is(err, calendar.ErrScopeMissing) {
		return back("scope_missing"), nil
	} else if err != nil {
		s.log.InfoContext(ctx, "Google connect failed", "err", err)
		return back("failed"), nil
	}
	id, outcome, err := s.saveGoogleAccount(ctx, a.HouseholdID, acct)
	if err != nil {
		return nil, err
	}
	if outcome != "connected" {
		s.revokeGoogle(ctx, acct.RefreshToken)
		return back(outcome), nil
	}
	s.wakeCalendar()
	return back(outcome, id), nil
}

// saveGoogleAccount stores a newly connected account, or replaces the token of one already
// connected (clearing "reconnect needed" and refreshing its calendars soon).
func (s *Server) saveGoogleAccount(ctx context.Context, h uuid.UUID, acct calendar.GoogleAccount) (uuid.UUID, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := dbq.New(tx)
	if _, err := q.LockHousehold(ctx, h); err != nil {
		return uuid.Nil, "", err
	}
	kr := s.cfg.SecretKeys
	email := strings.ToLower(acct.Email)
	c, err := q.GetGoogleConnectionBySubject(ctx, dbq.GetGoogleConnectionBySubjectParams{HouseholdID: h, Subject: acct.Subject})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if n, err := q.CountGoogleConnections(ctx, h); err != nil {
			return uuid.Nil, "", err
		} else if n >= maxGoogleConnections {
			return uuid.Nil, "too_many", nil
		}
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, "", err
		}
		sealed, err := calendar.SealGoogleToken(kr, h, id, acct.RefreshToken)
		if err != nil {
			return uuid.Nil, "", err
		}
		if c, err = q.CreateGoogleConnection(ctx, dbq.CreateGoogleConnectionParams{ID: id, HouseholdID: h, Subject: acct.Subject,
			Email: email, Scope: acct.Scope, RefreshToken: &sealed}); err != nil {
			return uuid.Nil, "", err
		}
	case err != nil:
		return uuid.Nil, "", err
	default:
		sealed, err := calendar.SealGoogleToken(kr, h, c.ID, acct.RefreshToken)
		if err != nil {
			return uuid.Nil, "", err
		}
		if c, err = q.ReconnectGoogleConnection(ctx, dbq.ReconnectGoogleConnectionParams{HouseholdID: h, ID: c.ID, Email: email,
			Scope: acct.Scope, RefreshToken: &sealed}); err != nil {
			return uuid.Nil, "", err
		}
		if err := q.WakeGoogleSources(ctx, dbq.WakeGoogleSourcesParams{HouseholdID: h, GoogleConnectionID: &c.ID}); err != nil {
			return uuid.Nil, "", err
		}
	}
	if err := live.Notify(ctx, q, h, live.Event{Topic: live.Calendar}); err != nil {
		return uuid.Nil, "", err
	}
	return c.ID, "connected", tx.Commit(ctx)
}

// revokeGoogle asks Google to revoke a token, briefly; nothing depends on it succeeding.
func (s *Server) revokeGoogle(ctx context.Context, token string) {
	if s.google == nil || token == "" {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), googleRevokeTimeout)
	defer cancel()
	if err := s.google.Revoke(rctx, token); err != nil {
		s.log.InfoContext(ctx, "Google revoke failed", "err", err)
	}
}

func (s *Server) ListGoogleConnections(ctx context.Context, _ api.ListGoogleConnectionsRequestObject) (api.ListGoogleConnectionsResponseObject, error) {
	rows, err := s.q.ListGoogleConnections(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := make([]api.GoogleConnection, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.GoogleConnection{Id: r.ID, Email: r.Email, NeedsReconnect: r.NeedsReconnect, Calendars: int(r.Calendars), Version: int(r.Version)})
	}
	return api.ListGoogleConnections200JSONResponse{Enabled: s.google != nil && s.cfg.SecretKeys != nil, Connections: out}, nil
}

// googleAccess opens a connection's token and refreshes an access token, marking the
// connection "reconnect needed" when Google refuses it.
func (s *Server) googleAccess(ctx context.Context, q *dbq.Queries, c dbq.CalendarGoogleConnection) (string, error) {
	if c.NeedsReconnect || c.RefreshToken == nil {
		return "", calendar.ErrReconnect
	}
	refresh, err := calendar.OpenGoogleToken(s.cfg.SecretKeys, c.HouseholdID, c.ID, *c.RefreshToken)
	if err != nil {
		return "", err
	}
	acc, err := s.google.Access(ctx, refresh)
	if errors.Is(err, calendar.ErrReconnect) {
		if err := q.GoogleConnectionNeedsReconnect(ctx, c.ID); err != nil {
			return "", err
		}
		return "", calendar.ErrReconnect
	} else if err != nil {
		return "", err
	}
	if acc.Rotated != "" {
		sealed, err := calendar.SealGoogleToken(s.cfg.SecretKeys, c.HouseholdID, c.ID, acc.Rotated)
		if err != nil {
			return "", err
		}
		if err := q.SetGoogleRefreshToken(ctx, dbq.SetGoogleRefreshTokenParams{ID: c.ID, RefreshToken: &sealed}); err != nil {
			return "", err
		}
	}
	return acc.Token, nil
}

func googleUnavailable() api.Error {
	return apiError("google_unavailable", "Google couldn't be reached. Try again in a minute.")
}

func (s *Server) ListGoogleCalendars(ctx context.Context, req api.ListGoogleCalendarsRequestObject) (api.ListGoogleCalendarsResponseObject, error) {
	h := mustActor(ctx).HouseholdID
	c, err := s.q.GetGoogleConnection(ctx, dbq.GetGoogleConnectionParams{HouseholdID: h, ID: req.ConnectionId})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.ListGoogleCalendars404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	if s.google == nil || s.cfg.SecretKeys == nil {
		return api.ListGoogleCalendars422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errGoogleOff)}, nil
	}
	reconnect := api.ListGoogleCalendars422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errReconnectNeeded)}
	unavailable := api.ListGoogleCalendarsdefaultJSONResponse{StatusCode: http.StatusBadGateway, Body: googleUnavailable()}
	token, err := s.googleAccess(ctx, s.q, c)
	if errors.Is(err, calendar.ErrReconnect) {
		return reconnect, nil
	} else if err != nil {
		s.log.InfoContext(ctx, "Google token refresh failed", "connection", c.ID, "err", err)
		return unavailable, nil
	}
	cals, err := s.google.Calendars(ctx, token)
	if err != nil {
		s.log.InfoContext(ctx, "Google calendar list failed", "connection", c.ID, "err", err)
		return unavailable, nil
	}
	followed, err := s.q.ListFollowedGoogleCalendars(ctx, dbq.ListFollowedGoogleCalendarsParams{HouseholdID: h, GoogleConnectionID: &c.ID})
	if err != nil {
		return nil, err
	}
	by := make(map[string]uuid.UUID, len(followed))
	for _, f := range followed {
		if f.GoogleCalendarID != nil {
			by[*f.GoogleCalendarID] = f.ID
		}
	}
	out := make([]api.GoogleCalendar, 0, len(cals))
	for _, g := range cals {
		gc := api.GoogleCalendar{GoogleId: g.ID, Name: g.Name, Primary: g.Primary}
		if id, ok := by[g.ID]; ok {
			gc.CalendarId = &id
		}
		out = append(out, gc)
	}
	return api.ListGoogleCalendars200JSONResponse{Calendars: out}, nil
}

func (s *Server) FollowGoogleCalendar(ctx context.Context, req api.FollowGoogleCalendarRequestObject) (api.FollowGoogleCalendarResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	unprocessable := func(e api.Error) api.FollowGoogleCalendarResponseObject {
		return api.FollowGoogleCalendar422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(e)}
	}
	c, err := cmd.Q.GetGoogleConnection(ctx, dbq.GetGoogleConnectionParams{HouseholdID: h, ID: req.ConnectionId})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.FollowGoogleCalendar404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	if s.google == nil || s.cfg.SecretKeys == nil {
		return unprocessable(errGoogleOff), nil
	}
	if c.NeedsReconnect {
		return unprocessable(errReconnectNeeded), nil
	}
	b := req.Body
	followed, err := cmd.Q.ListFollowedGoogleCalendars(ctx, dbq.ListFollowedGoogleCalendarsParams{HouseholdID: h, GoogleConnectionID: &c.ID})
	if err != nil {
		return nil, err
	}
	for _, f := range followed {
		if f.GoogleCalendarID != nil && *f.GoogleCalendarID == b.GoogleId {
			return api.FollowGoogleCalendar409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("calendar_exists", "This household already follows that calendar."))}, nil
		}
	}
	if n, err := cmd.Q.CountCalendarSources(ctx, h); err != nil {
		return nil, err
	} else if n >= maxCalendars {
		return unprocessable(apiError("too_many_calendars", "This household follows 50 calendars. Remove one to add another.")), nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	googleID := b.GoogleId
	r, err := cmd.Q.CreateGoogleCalendarSource(ctx, dbq.CreateGoogleCalendarSourceParams{ID: id, HouseholdID: h, Name: strings.TrimSpace(b.Name),
		Color: string(b.Color), ShowOnDisplay: b.ShowOnDisplay, GoogleConnectionID: &c.ID, GoogleCalendarID: &googleID})
	if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Calendar}); err != nil {
		return nil, err
	}
	s.wakeCalendar()
	return api.FollowGoogleCalendar201JSONResponse(toSource(r)), nil
}

// DisconnectGoogle deletes the account and, by cascade, its calendars, then asks Google to
// revoke the token.
func (s *Server) DisconnectGoogle(ctx context.Context, req api.DisconnectGoogleRequestObject) (api.DisconnectGoogleResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	sealed, err := cmd.Q.DeleteGoogleConnection(ctx, dbq.DeleteGoogleConnectionParams{HouseholdID: h, ID: req.ConnectionId,
		ExpectedVersion: int32(req.Params.ExpectedVersion)}) //nolint:gosec // the spec caps it at int32
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := cmd.Q.GetGoogleConnection(ctx, dbq.GetGoogleConnectionParams{HouseholdID: h, ID: req.ConnectionId}); errors.Is(err, pgx.ErrNoRows) {
			return api.DisconnectGoogle404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		} else if err != nil {
			return nil, err
		}
		return api.DisconnectGoogle409JSONResponse{ConflictJSONResponse: conflict("Google account")}, nil
	} else if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Calendar}); err != nil {
		return nil, err
	}
	if s.cfg.SecretKeys != nil && sealed != nil {
		if token, err := calendar.OpenGoogleToken(s.cfg.SecretKeys, h, req.ConnectionId, *sealed); err == nil {
			s.revokeGoogle(ctx, token)
		}
	}
	return api.DisconnectGoogle204Response{}, nil
}
