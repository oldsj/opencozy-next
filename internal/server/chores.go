package server

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

func toChore(c dbq.Chore) api.Chore {
	return api.Chore{Id: c.ID, ChildId: c.ChildID, Title: c.Title, Points: int(c.Points), Days: daysList(c.Days), Version: int(c.Version)}
}

func toRoutine(r dbq.Routine, steps []dbq.RoutineStep) api.Routine {
	out := api.Routine{Id: r.ID, ChildId: r.ChildID, Title: r.Title, Points: int(r.Points), Days: daysList(r.Days), Version: int(r.Version)}
	out.Steps = make([]struct {
		Id    api.Id `json:"id"`
		Title string `json:"title"`
	}, len(steps))
	for i, s := range steps {
		out.Steps[i].Id, out.Steps[i].Title = s.ID, s.Title
	}
	return out
}

// ListChores lists the household's chores.
func (s *Server) ListChores(ctx context.Context, _ api.ListChoresRequestObject) (api.ListChoresResponseObject, error) {
	rows, err := s.q.ListChores(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := make(api.ListChores200JSONResponse, len(rows))
	for i, c := range rows {
		out[i] = toChore(c)
	}
	return out, nil
}

// CreateChore adds a chore for a live child of the household.
func (s *Server) CreateChore(ctx context.Context, req api.CreateChoreRequestObject) (api.CreateChoreResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	if !a.MayActFor(req.Body.ChildId) {
		return api.CreateChore404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	if err := cmd.LockChildren(ctx, req.Body.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.CreateChore404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	c, err := cmd.Q.CreateChore(ctx, dbq.CreateChoreParams{
		HouseholdID: a.HouseholdID, ChildID: req.Body.ChildId, Title: strings.TrimSpace(req.Body.Title),
		Points: int32(req.Body.Points), Days: daysMask(req.Body.Days), //nolint:gosec // 0-1000 by the spec
	})
	if err != nil {
		return nil, err
	}
	return api.CreateChore201JSONResponse(toChore(c)), nil
}

// ArchiveChore removes a chore from now on.
func (s *Server) ArchiveChore(ctx context.Context, req api.ArchiveChoreRequestObject) (api.ArchiveChoreResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.ArchiveChore(ctx, dbq.ArchiveChoreParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.ChoreId, Now: &now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.ArchiveChore404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.ArchiveChore204Response{}, nil
}

// ListRoutines lists the household's routines with their steps.
func (s *Server) ListRoutines(ctx context.Context, _ api.ListRoutinesRequestObject) (api.ListRoutinesResponseObject, error) {
	h := mustActor(ctx).HouseholdID
	rows, err := s.q.ListRoutines(ctx, h)
	if err != nil {
		return nil, err
	}
	all, err := s.q.ListRoutineSteps(ctx, h)
	if err != nil {
		return nil, err
	}
	steps := map[uuid.UUID][]dbq.RoutineStep{}
	for _, st := range all {
		steps[st.RoutineID] = append(steps[st.RoutineID], st)
	}
	out := make(api.ListRoutines200JSONResponse, len(rows))
	for i, r := range rows {
		out[i] = toRoutine(r, steps[r.ID])
	}
	return out, nil
}

// CreateRoutine adds a routine and its steps for a live child of the household.
func (s *Server) CreateRoutine(ctx context.Context, req api.CreateRoutineRequestObject) (api.CreateRoutineResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	if !a.MayActFor(req.Body.ChildId) {
		return api.CreateRoutine404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	if err := cmd.LockChildren(ctx, req.Body.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.CreateRoutine404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	r, err := cmd.Q.CreateRoutine(ctx, dbq.CreateRoutineParams{
		HouseholdID: a.HouseholdID, ChildID: req.Body.ChildId, Title: strings.TrimSpace(req.Body.Title),
		Points: int32(req.Body.Points), Days: daysMask(req.Body.Days), //nolint:gosec // 0-1000 by the spec
	})
	if err != nil {
		return nil, err
	}
	steps := make([]dbq.RoutineStep, 0, len(req.Body.Steps))
	for i, t := range req.Body.Steps {
		st, err := cmd.Q.CreateRoutineStep(ctx, dbq.CreateRoutineStepParams{
			HouseholdID: a.HouseholdID, RoutineID: r.ID, Title: strings.TrimSpace(t), Position: int16(i), //nolint:gosec // at most 20 by the spec
		})
		if err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	return api.CreateRoutine201JSONResponse(toRoutine(r, steps)), nil
}

// ArchiveRoutine removes a routine from now on.
func (s *Server) ArchiveRoutine(ctx context.Context, req api.ArchiveRoutineRequestObject) (api.ArchiveRoutineResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.ArchiveRoutine(ctx, dbq.ArchiveRoutineParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.RoutineId, Now: &now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.ArchiveRoutine404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.ArchiveRoutine204Response{}, nil
}
