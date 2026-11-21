package server

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
	"github.com/oldsj/opencozy-next/internal/screentime"
)

func toWebFilter(portals, games []string) api.WebFilter {
	if portals == nil {
		portals = []string{}
	}
	if games == nil {
		games = []string{}
	}
	return api.WebFilter{GamePortals: portals, Games: games}
}

func webFilter(ctx context.Context, q *dbq.Queries, h uuid.UUID) (api.WebFilter, error) {
	f, err := q.GetWebFilter(ctx, h)
	if err != nil {
		return api.WebFilter{}, err
	}
	return toWebFilter(f.GamePortalDomains, f.GameDomains), nil
}

// GetWebFilter is the household's blocked websites.
func (s *Server) GetWebFilter(ctx context.Context, _ api.GetWebFilterRequestObject) (api.GetWebFilterResponseObject, error) {
	f, err := webFilter(ctx, s.q, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	return api.GetWebFilter200JSONResponse(f), nil
}

// SetWebFilter replaces the household's blocked websites.
func (s *Server) SetWebFilter(ctx context.Context, req api.SetWebFilterRequestObject) (api.SetWebFilterResponseObject, error) {
	cmd := mustCmd(ctx)
	f, err := screentime.NewWebFilter(req.Body.GamePortals, req.Body.Games)
	var de screentime.DomainError
	if errors.As(err, &de) {
		return api.SetWebFilter422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError(de.Code, de.Message))}, nil
	} else if err != nil {
		return nil, err
	}
	row, err := cmd.Q.SetWebFilter(ctx, dbq.SetWebFilterParams{ID: cmd.HouseholdID(), GamePortals: f.GamePortals, Games: f.Games})
	if err != nil {
		return nil, err
	}
	// Kid devices read it with their screen time.
	if err := live.Notify(ctx, cmd.Q, cmd.HouseholdID(), live.Event{Topic: live.Today}); err != nil {
		return nil, err
	}
	return api.SetWebFilter200JSONResponse(toWebFilter(row.GamePortalDomains, row.GameDomains)), nil
}

// GetGamesAllowances is the router's games gate: the credential's children with iPad time running.
func (s *Server) GetGamesAllowances(ctx context.Context, _ api.GetGamesAllowancesRequestObject) (api.GetGamesAllowancesResponseObject, error) {
	a := mustActor(ctx)
	now, err := s.readNow(ctx)
	if err != nil {
		return nil, err
	}
	children, err := s.q.ListChildren(ctx, dbq.ListChildrenParams{HouseholdID: a.HouseholdID})
	if err != nil {
		return nil, err
	}
	out := api.GetGamesAllowances200JSONResponse{ServerTime: now.UTC()}
	out.Allowances = make([]struct {
		ChildId          api.Id `json:"childId"`
		SecondsRemaining int    `json:"secondsRemaining"`
	}, 0, len(children))
	for _, c := range children {
		if !a.MayActFor(c.ID) {
			continue
		}
		ws, err := screentime.Windows(ctx, s.q, a.HouseholdID, c.ID, now)
		if err != nil {
			return nil, err
		}
		until := screentime.UnlockedUntil(ws, now)
		if until == nil {
			continue
		}
		left := min(until.Sub(now), screentime.AllowanceCap)
		seconds := int((left + time.Second - 1) / time.Second)
		if seconds > 0 {
			out.Allowances = append(out.Allowances, struct {
				ChildId          api.Id `json:"childId"`
				SecondsRemaining int    `json:"secondsRemaining"`
			}{c.ID, seconds})
		}
	}
	return out, nil
}
