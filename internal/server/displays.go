package server

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// PairingCookie holds the secret a display claims its session with.
const PairingCookie = "__Host-display-pairing"

// pairingRatePerMinute is per client address, for starting and claiming pairings: a display
// polls every 3 seconds (20 a minute), with room for a few displays behind one address.
const pairingRatePerMinute = 90

// maxOpenPairings caps open pairings: starting one needs no sign-in.
const maxOpenPairings = 500

// codeAlphabet has no 0/O or 1/I/L: 31 symbols, so 6 of them are about 30 bits. A code can be
// entered only by an adult who signed in within 10 minutes, and lasts 10 minutes.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newPairingCode() (string, error) {
	out := make([]byte, 6)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = codeAlphabet[n.Int64()]
	}
	return string(out), nil
}

// normalizeCode accepts the code as typed: any case, with or without the dash or spaces.
func normalizeCode(s string) string {
	s = strings.ToUpper(s)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, s)
}

func codeHash(code string) []byte { return auth.HashToken("pairing:" + normalizeCode(code)) }

func displayCookie(name, value string, maxAge int) string {
	return (&http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}).String()
}

func toDisplay(d dbq.Display) api.Display {
	scopes := make([]api.DisplayScope, len(d.Scopes))
	for i, s := range d.Scopes {
		scopes[i] = api.DisplayScope(s)
	}
	return api.Display{Id: d.ID, Name: d.Name, Scopes: scopes, CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt}
}

// StartDisplayPairing gives a display a code to show and a secret to claim its session with.
func (s *Server) StartDisplayPairing(ctx context.Context, _ api.StartDisplayPairingRequestObject) (api.StartDisplayPairingResponseObject, error) {
	if err := s.q.DeleteExpiredPairings(ctx); err != nil {
		return nil, err
	}
	n, err := s.q.CountPairings(ctx)
	if err != nil {
		return nil, err
	}
	if n >= maxOpenPairings {
		return api.StartDisplayPairing503JSONResponse(apiError("pairing_busy", "Too many displays are pairing. Try again in a few minutes.")), nil
	}
	claim, claimHash, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	code, err := newPairingCode()
	if err != nil {
		return nil, err
	}
	expires, err := s.q.CreatePairing(ctx, dbq.CreatePairingParams{ClaimHash: claimHash, CodeHash: codeHash(code)})
	if err != nil {
		return nil, err // a code collision among open pairings: the display retries
	}
	cookie := displayCookie(PairingCookie, claim, int(10*time.Minute/time.Second))
	return api.StartDisplayPairing201JSONResponse{
		Body:    api.Pairing{Code: code, ExpiresAt: expires},
		Headers: api.StartDisplayPairing201ResponseHeaders{SetCookie: &cookie},
	}, nil
}

var errPairingGone = apiError("pairing_expired", "This pairing has expired. Start again for a new code.")

// ClaimDisplayPairing issues the display's session once an adult has entered its code.
func (s *Server) ClaimDisplayPairing(ctx context.Context, req api.ClaimDisplayPairingRequestObject) (api.ClaimDisplayPairingResponseObject, error) {
	gone := api.ClaimDisplayPairing404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errPairingGone)}
	if req.Params.UnderscoreUnderscoreHostDisplayPairing == nil || !auth.WellFormedCookie(*req.Params.UnderscoreUnderscoreHostDisplayPairing) {
		return gone, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := dbq.New(tx)
	p, err := q.LockPairingByClaim(ctx, auth.HashToken(*req.Params.UnderscoreUnderscoreHostDisplayPairing))
	if errors.Is(err, pgx.ErrNoRows) {
		return gone, nil
	} else if err != nil {
		return nil, err
	}
	if p.DisplayID == nil {
		return api.ClaimDisplayPairing202JSONResponse{Status: api.Waiting}, nil
	}
	token, hash, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	d, err := q.IssueDisplayToken(ctx, dbq.IssueDisplayTokenParams{ID: *p.DisplayID, TokenHash: hash})
	if err := q.DeletePairing(ctx, p.ID); err != nil {
		return nil, err
	}
	if errors.Is(err, pgx.ErrNoRows) { // unpaired (or the household archived) before the claim
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return gone, nil
	} else if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cookie := displayCookie(actor.DisplayCookie, token, int(time.Until(d.ExpiresAt)/time.Second))
	return api.ClaimDisplayPairing200JSONResponse{Body: toDisplay(d), Headers: api.ClaimDisplayPairing200ResponseHeaders{SetCookie: &cookie}}, nil
}

// ListDisplays lists the household's paired displays.
func (s *Server) ListDisplays(ctx context.Context, _ api.ListDisplaysRequestObject) (api.ListDisplaysResponseObject, error) {
	rows, err := s.q.ListDisplays(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := make(api.ListDisplays200JSONResponse, len(rows))
	for i, d := range rows {
		out[i] = toDisplay(d)
	}
	return out, nil
}

// CreateDisplay pairs the display showing the code into the adult's household.
func (s *Server) CreateDisplay(ctx context.Context, req api.CreateDisplayRequestObject) (api.CreateDisplayResponseObject, error) {
	cmd := mustCmd(ctx)
	h := mustActor(ctx).HouseholdID
	p, err := cmd.Q.LockOpenPairingByCode(ctx, codeHash(req.Body.Code))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.CreateDisplay404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(apiError("no_such_code", "No display is showing that code. Check it, or start pairing again on the display."))}, nil
	} else if err != nil {
		return nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	scopes := make([]string, len(req.Body.Scopes))
	for i, sc := range req.Body.Scopes {
		scopes[i] = string(sc)
	}
	d, err := cmd.Q.CreateDisplay(ctx, dbq.CreateDisplayParams{HouseholdID: h, Name: strings.TrimSpace(req.Body.Name), Scopes: scopes, Now: now})
	if err != nil {
		return nil, err
	}
	if err := cmd.Q.ApprovePairing(ctx, dbq.ApprovePairingParams{ID: p, HouseholdID: &h, DisplayID: &d.ID}); err != nil {
		return nil, err
	}
	return api.CreateDisplay201JSONResponse(toDisplay(d)), nil
}

// RevokeDisplay unpairs a display.
func (s *Server) RevokeDisplay(ctx context.Context, req api.RevokeDisplayRequestObject) (api.RevokeDisplayResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.RevokeDisplay(ctx, dbq.RevokeDisplayParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.DisplayId, Now: &now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.RevokeDisplay404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.RevokeDisplay204Response{}, nil
}

// GetDisplaySession is the calling display's scopes.
func (s *Server) GetDisplaySession(ctx context.Context, _ api.GetDisplaySessionRequestObject) (api.GetDisplaySessionResponseObject, error) {
	a := mustActor(ctx)
	out := api.GetDisplaySession200JSONResponse{Scopes: make([]api.DisplayScope, 0, len(a.Scopes))}
	for _, sc := range a.Scopes {
		out.Scopes = append(out.Scopes, api.DisplayScope(sc))
	}
	return out, nil
}
