package server

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// checkPassword verifies an adult's password and, when it is stored in an old scheme, replaces
// it with argon2id.
func (s *Server) checkPassword(ctx context.Context, a dbq.Adult, password string) (bool, error) {
	if a.PasswordHash == nil || a.PasswordScheme == nil {
		auth.BurnTime(password)
		return false, nil
	}
	ok, rehash, err := auth.Verify(*a.PasswordScheme, *a.PasswordHash, password)
	if err != nil || !ok {
		return false, err
	}
	if rehash {
		h, err := auth.HashPassword(password)
		if err != nil {
			return false, err
		}
		scheme := auth.SchemeArgon2id
		if err := s.q.SetAdultPassword(ctx, dbq.SetAdultPasswordParams{HouseholdID: a.HouseholdID, ID: a.ID, PasswordHash: &h, PasswordScheme: &scheme}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Login checks the password and starts a new session.
func (s *Server) Login(ctx context.Context, req api.LoginRequestObject) (api.LoginResponseObject, error) {
	wrong := api.Login401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errWrong)}
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
	me, err := s.me(ctx, a)
	if err != nil {
		return nil, err
	}
	cookie, err := s.startSession(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	return api.Login200JSONResponse{Body: me, Headers: api.Login200ResponseHeaders{SetCookie: &cookie}}, nil
}

// Logout ends the session, if there is one, and clears the cookie.
func (s *Server) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	if a, ok := actor.From(ctx); ok && a.Kind == actor.Adult {
		if err := s.q.DeleteAdultSession(ctx, a.SessionHash); err != nil {
			return nil, err
		}
	}
	cleared := sessionCookie("", -1)
	return api.Logout204Response{Headers: api.Logout204ResponseHeaders{SetCookie: &cleared}}, nil
}

// GetMe returns the signed-in adult and their household.
func (s *Server) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	a := mustActor(ctx)
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	me, err := s.me(ctx, adult)
	if err != nil {
		return nil, err
	}
	return api.GetMe200JSONResponse(me), nil
}

// ConfirmSignIn re-checks the password and marks the session as freshly signed in.
func (s *Server) ConfirmSignIn(ctx context.Context, req api.ConfirmSignInRequestObject) (api.ConfirmSignInResponseObject, error) {
	a := mustActor(ctx)
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	ok, err := s.checkPassword(ctx, adult, req.Body.Password)
	if err != nil {
		return nil, err
	}
	if !ok {
		return api.ConfirmSignIn403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(apiError("wrong_password", "That password is not right."))}, nil
	}
	if err := s.q.ConfirmAdultSession(ctx, a.SessionHash); err != nil {
		return nil, err
	}
	return api.ConfirmSignIn204Response{}, nil
}

func (s *Server) me(ctx context.Context, a dbq.Adult) (api.Me, error) {
	h, err := s.q.GetHousehold(ctx, a.HouseholdID)
	if err != nil {
		return api.Me{}, err
	}
	var me api.Me
	me.Adult.Id, me.Adult.Email, me.Adult.Name = a.ID, a.Email, a.Name
	me.Household = toHousehold(h)
	return me, nil
}

func toHousehold(h dbq.Household) api.Household {
	return api.Household{Id: h.ID, Name: h.Name, TimeZone: h.TimeZone}
}

// GetHousehold returns the caller's household.
func (s *Server) GetHousehold(ctx context.Context, _ api.GetHouseholdRequestObject) (api.GetHouseholdResponseObject, error) {
	h, err := s.q.GetHousehold(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	return api.GetHousehold200JSONResponse(toHousehold(h)), nil
}
