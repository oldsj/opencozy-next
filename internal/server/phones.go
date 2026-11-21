package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Parent phones (slice 11, design §3): the iOS parent app signs in with a password or a passkey
// and gets an ocp_ bearer session for 90 days, one per phone, which acts as its adult. Each
// session holds at most one APNs token. Adults see and sign out phones in Manage.

// phoneSessionTTL is how long a phone stays signed in (the table allows at most 90 days).
const phoneSessionTTL = 90 * 24 * time.Hour

var errPhoneName = apiError("invalid_name", "Give the phone a name.")

// startPhoneSession stores a new phone session for the adult and returns its one-time token.
func (s *Server) startPhoneSession(ctx context.Context, adult dbq.Adult, name string) (api.PhoneSignedIn, error) {
	secret, hash, err := auth.NewToken(auth.PrefixPhone)
	if err != nil {
		return api.PhoneSignedIn{}, err
	}
	now := time.Now()
	row, err := s.q.CreatePhoneSession(ctx, dbq.CreatePhoneSessionParams{
		HouseholdID: adult.HouseholdID, AdultID: adult.ID, Name: name, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(phoneSessionTTL),
	})
	if err != nil {
		return api.PhoneSignedIn{}, err
	}
	me, err := s.me(ctx, adult)
	if err != nil {
		return api.PhoneSignedIn{}, err
	}
	return api.PhoneSignedIn{Token: secret, Me: me, Session: toPhoneSession(row, adult.Name, true)}, nil
}

func toPhoneSession(p dbq.PhoneSession, adultName string, current bool) api.PhoneSession {
	return api.PhoneSession{
		Id: p.ID, AdultId: p.AdultID, AdultName: adultName, Name: p.Name, CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt,
		LastUsedAt: p.LastUsedAt, PushRegistered: p.PushToken != nil, Current: current,
	}
}

// PhoneSignIn signs a phone in with an email and password.
func (s *Server) PhoneSignIn(ctx context.Context, req api.PhoneSignInRequestObject) (api.PhoneSignInResponseObject, error) {
	name := strings.TrimSpace(req.Body.DeviceName)
	if name == "" {
		return api.PhoneSignIn400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(errPhoneName)}, nil
	}
	wrong := api.PhoneSignIn401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errWrong)}
	a, err := s.q.GetAdultByEmail(ctx, normalizeEmail(string(req.Body.Email)))
	if errors.Is(err, pgx.ErrNoRows) {
		auth.BurnTime(req.Body.Password) // same timing whether or not the account exists
		return wrong, nil
	}
	if err != nil {
		return nil, err
	}
	ok, err := s.checkPassword(ctx, a, req.Body.Password)
	if err != nil {
		return nil, err
	}
	if !ok {
		return wrong, nil
	}
	out, err := s.startPhoneSession(ctx, a, name)
	if err != nil {
		return nil, err
	}
	return api.PhoneSignIn201JSONResponse(out), nil
}

// PhoneSignInWithPasskey signs a phone in with a passkey, after POST /auth/passkey/options.
func (s *Server) PhoneSignInWithPasskey(ctx context.Context, req api.PhoneSignInWithPasskeyRequestObject) (api.PhoneSignInWithPasskeyResponseObject, error) {
	name := strings.TrimSpace(req.Body.DeviceName)
	if name == "" {
		return api.PhoneSignInWithPasskey400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(errPhoneName)}, nil
	}
	adult, ok, err := s.passkeySignInAdult(ctx, req.Body.ChallengeId, req.Body.Credential)
	if err != nil || !ok {
		return api.PhoneSignInWithPasskey401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errPasskey)}, err
	}
	out, err := s.startPhoneSession(ctx, adult, name)
	if err != nil {
		return nil, err
	}
	return api.PhoneSignInWithPasskey201JSONResponse(out), nil
}

// GetPhoneSession returns this phone's session, its adult and household.
func (s *Server) GetPhoneSession(ctx context.Context, _ api.GetPhoneSessionRequestObject) (api.GetPhoneSessionResponseObject, error) {
	a := mustActor(ctx)
	p, err := s.q.GetPhoneSession(ctx, dbq.GetPhoneSessionParams{HouseholdID: a.HouseholdID, ID: a.CredentialID})
	if err != nil {
		return nil, err
	}
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	me, err := s.me(ctx, adult)
	if err != nil {
		return nil, err
	}
	var muted *time.Time
	if adult.PushMutedUntil != nil && adult.PushMutedUntil.After(time.Now()) {
		muted = adult.PushMutedUntil
	}
	return api.GetPhoneSession200JSONResponse{Session: toPhoneSession(p, adult.Name, true), Me: me, PushMutedUntil: muted}, nil
}

// PhoneSignOut ends this phone's session and forgets its push token. Like signing out on the
// web, it is not a command: doing it twice is the same as once.
func (s *Server) PhoneSignOut(ctx context.Context, _ api.PhoneSignOutRequestObject) (api.PhoneSignOutResponseObject, error) {
	a := mustActor(ctx)
	if _, err := s.q.RevokePhoneSession(ctx, dbq.RevokePhoneSessionParams{HouseholdID: a.HouseholdID, ID: a.CredentialID}); err != nil {
		return nil, err
	}
	return api.PhoneSignOut204Response{}, nil
}

// registerPush takes the token from wherever it was registered and gives it to id.
func registerPush(ctx context.Context, q *dbq.Queries, id uuid.UUID, token string) error {
	if err := q.ReleasePhonePushToken(ctx, dbq.ReleasePhonePushTokenParams{Token: &token, KeepID: id}); err != nil {
		return err
	}
	return q.ReleaseDevicePushToken(ctx, dbq.ReleaseDevicePushTokenParams{Token: &token, KeepID: id})
}

// RegisterPhonePush stores this phone's APNs token.
func (s *Server) RegisterPhonePush(ctx context.Context, req api.RegisterPhonePushRequestObject) (api.RegisterPhonePushResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	token, env := strings.ToLower(req.Body.Token), string(req.Body.Environment)
	if err := registerPush(ctx, cmd.Q, a.CredentialID, token); err != nil {
		return nil, err
	}
	at, err := cmd.Q.SetPhonePushToken(ctx, dbq.SetPhonePushTokenParams{HouseholdID: a.HouseholdID, ID: a.CredentialID, Token: &token, Environment: &env})
	if err != nil {
		return nil, err
	}
	return api.RegisterPhonePush200JSONResponse{Environment: api.PushRegisteredEnvironment(env), RegisteredAt: at}, nil
}

// UnregisterPhonePush stops pushes to this phone.
func (s *Server) UnregisterPhonePush(ctx context.Context, _ api.UnregisterPhonePushRequestObject) (api.UnregisterPhonePushResponseObject, error) {
	a := mustActor(ctx)
	if err := mustCmd(ctx).Q.ClearPhonePushToken(ctx, dbq.ClearPhonePushTokenParams{HouseholdID: a.HouseholdID, ID: a.CredentialID}); err != nil {
		return nil, err
	}
	return api.UnregisterPhonePush204Response{}, nil
}

// ListPhoneSessions lists the household's signed-in parent phones.
func (s *Server) ListPhoneSessions(ctx context.Context, _ api.ListPhoneSessionsRequestObject) (api.ListPhoneSessionsResponseObject, error) {
	a := mustActor(ctx)
	rows, err := s.q.ListPhoneSessions(ctx, a.HouseholdID)
	if err != nil {
		return nil, err
	}
	items := make([]api.PhoneSession, 0, len(rows))
	for _, r := range rows {
		items = append(items, api.PhoneSession{
			Id: r.ID, AdultId: r.AdultID, AdultName: r.AdultName, Name: r.Name, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
			LastUsedAt: r.LastUsedAt, PushRegistered: r.PushRegistered, Current: a.Kind == actor.Phone && r.ID == a.CredentialID,
		})
	}
	return api.ListPhoneSessions200JSONResponse{Items: items}, nil
}

// RevokePhoneSession signs a phone out; its next request gets 401.
func (s *Server) RevokePhoneSession(ctx context.Context, req api.RevokePhoneSessionRequestObject) (api.RevokePhoneSessionResponseObject, error) {
	a := mustActor(ctx)
	n, err := mustCmd(ctx).Q.RevokePhoneSession(ctx, dbq.RevokePhoneSessionParams{HouseholdID: a.HouseholdID, ID: req.PhoneSessionId})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.RevokePhoneSession404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.RevokePhoneSession204Response{}, nil
}

// RegisterDevicePush stores a kid device's APNs token.
func (s *Server) RegisterDevicePush(ctx context.Context, req api.RegisterDevicePushRequestObject) (api.RegisterDevicePushResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	token, env := strings.ToLower(req.Body.Token), string(req.Body.Environment)
	if err := registerPush(ctx, cmd.Q, a.CredentialID, token); err != nil {
		return nil, err
	}
	at, err := cmd.Q.SetDevicePushToken(ctx, dbq.SetDevicePushTokenParams{HouseholdID: a.HouseholdID, ID: a.CredentialID, Token: &token, Environment: &env})
	if err != nil {
		return nil, err
	}
	return api.RegisterDevicePush200JSONResponse{Environment: api.PushRegisteredEnvironment(env), RegisteredAt: at}, nil
}

// UnregisterDevicePush stops pushes to a kid device.
func (s *Server) UnregisterDevicePush(ctx context.Context, _ api.UnregisterDevicePushRequestObject) (api.UnregisterDevicePushResponseObject, error) {
	a := mustActor(ctx)
	if err := mustCmd(ctx).Q.ClearDevicePushToken(ctx, dbq.ClearDevicePushTokenParams{HouseholdID: a.HouseholdID, ID: a.CredentialID}); err != nil {
		return nil, err
	}
	return api.UnregisterDevicePush204Response{}, nil
}

// touchPhone records, coarsely, that a phone session was used (for Manage). A failure is only
// logged: it never fails the request.
func (s *Server) touchPhone(ctx context.Context, a actor.Actor) {
	if a.Kind != actor.Phone {
		return
	}
	if err := s.q.TouchPhoneSession(ctx, dbq.TouchPhoneSessionParams{HouseholdID: a.HouseholdID, ID: a.CredentialID}); err != nil {
		s.log.WarnContext(ctx, "phone last used", "err", err)
	}
}

// appleAppSiteAssociation lets the iOS app use this site's passkeys (webcredentials). Served only
// when APPLE_TEAM_ID and APPLE_BUNDLE_ID are set.
func (s *Server) appleAppSiteAssociation(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"webcredentials": map[string]any{"apps": []string{s.cfg.AppleTeamID + "." + s.cfg.AppleBundleID}},
	})
}

// maxPushMute is how far ahead an adult may pause their alerts.
const maxPushMute = 90 * 24 * time.Hour

func pushMute(until *time.Time, now time.Time) api.PushMute {
	if until == nil || !until.After(now) {
		return api.PushMute{}
	}
	return api.PushMute{MutedUntil: until}
}

// GetPushMute says whether the adult's phone alerts are paused.
func (s *Server) GetPushMute(ctx context.Context, _ api.GetPushMuteRequestObject) (api.GetPushMuteResponseObject, error) {
	a := mustActor(ctx)
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	return api.GetPushMute200JSONResponse(pushMute(adult.PushMutedUntil, time.Now())), nil
}

// SetPushMute pauses the adult's own phone alerts until a time (old #87), or turns them back on.
func (s *Server) SetPushMute(ctx context.Context, req api.SetPushMuteRequestObject) (api.SetPushMuteResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	until := req.Body.Until
	if until != nil && (!until.After(now) || until.Sub(now) > maxPushMute) {
		return api.SetPushMute400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(
			apiError("invalid_until", "Choose a time in the next 90 days."))}, nil
	}
	got, err := cmd.Q.SetPushMute(ctx, dbq.SetPushMuteParams{HouseholdID: a.HouseholdID, ID: a.AdultID, Until: until})
	if err != nil {
		return nil, err
	}
	return api.SetPushMute200JSONResponse(pushMute(got, now)), nil
}
