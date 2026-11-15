package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// occurrenceNS namespaces occurrence ids: UUIDv5 of the definition and the household day, so a
// read's plan and a command's row agree.
var occurrenceNS = uuid.MustParse("6f1c2a52-6b0e-4c1e-9d63-3d1f0f7b2a10")

func occurrenceID(definition uuid.UUID, d day.Date) uuid.UUID {
	return uuid.NewSHA1(occurrenceNS, append(definition[:], d.String()...))
}

func pgDate(d day.Date) pgtype.Date {
	return pgtype.Date{Time: time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC), Valid: true}
}

// on reports whether a weekday mask (bit 0 Sunday) includes the date.
func on(days int16, d day.Date) bool { return days&(1<<d.Weekday()) != 0 }

func daysMask(days *[]int) int16 {
	if days == nil || len(*days) == 0 {
		return 127
	}
	var m int16
	for _, d := range *days {
		m |= 1 << d
	}
	return m
}

func daysList(mask int16) []int {
	out := []int{}
	for d := range 7 {
		if mask&(1<<d) != 0 {
			out = append(out, d)
		}
	}
	return out
}

// householdDay is the household's zone and the day at now.
func householdDay(ctx context.Context, q *dbq.Queries, household uuid.UUID, now time.Time) (*time.Location, day.Date, error) {
	h, err := q.GetHousehold(ctx, household)
	if err != nil {
		return nil, day.Date{}, err
	}
	loc, err := day.Zone(h.TimeZone)
	if err != nil {
		return nil, day.Date{}, fmt.Errorf("household %s: %w", household, err)
	}
	return loc, day.Of(now, loc), nil
}

func ptr[T any](v T) *T { return &v }

func choreItem(c dbq.Chore, occ *dbq.Occurrence) api.TodayItem {
	it := api.TodayItem{Id: c.ID, Title: c.Title, Points: ptr(int(c.Points)), State: "open"}
	if occ != nil {
		it.State = api.TodayItemState(occ.State)
		it.RejectedReason = occ.RejectedReason
	}
	return it
}

func routineGroup(r dbq.Routine, steps []dbq.RoutineStep, occ *dbq.Occurrence) api.TodayGroup {
	g := api.TodayGroup{Id: r.ID, Kind: api.TodayGroupKindRoutine, Title: r.Title, Items: []api.TodayItem{}}
	if r.Points > 0 {
		g.Status = ptr(fmt.Sprintf("%d points", r.Points))
	}
	for _, s := range steps {
		it := api.TodayItem{Id: s.ID, Title: s.Title, State: "open"}
		if occ != nil && (occ.State != "open" || slices.Contains(occ.CheckedSteps, s.ID)) {
			it.State = "complete"
			if occ.State == "pending" {
				it.State = "pending"
			}
		}
		if occ != nil && occ.State == "open" {
			it.RejectedReason = occ.RejectedReason
		}
		g.Items = append(g.Items, it)
	}
	if occ != nil {
		g.Done = occ.State == "complete"
		g.Pending = occ.State == "pending"
	}
	return g
}

// today builds what the wall display shows, for the children the actor may act for.
func (s *Server) today(ctx context.Context, q *dbq.Queries, a actor.Actor, now time.Time) (api.Today, error) {
	h := a.HouseholdID
	loc, d, err := householdDay(ctx, q, h, now)
	if err != nil {
		return api.Today{}, err
	}
	children, err := q.ListChildren(ctx, dbq.ListChildrenParams{HouseholdID: h, IncludeArchived: false})
	if err != nil {
		return api.Today{}, err
	}
	chores, err := q.ListChores(ctx, h)
	if err != nil {
		return api.Today{}, err
	}
	routines, err := q.ListRoutines(ctx, h)
	if err != nil {
		return api.Today{}, err
	}
	allSteps, err := q.ListRoutineSteps(ctx, h)
	if err != nil {
		return api.Today{}, err
	}
	occs, err := q.ListOccurrences(ctx, dbq.ListOccurrencesParams{HouseholdID: h, Day: pgDate(d)})
	if err != nil {
		return api.Today{}, err
	}
	balances, err := q.Balances(ctx, h)
	if err != nil {
		return api.Today{}, err
	}
	earned, err := q.EarnedSince(ctx, dbq.EarnedSinceParams{HouseholdID: h, CreatedAt: d.Start(loc)})
	if err != nil {
		return api.Today{}, err
	}
	byDef := map[uuid.UUID]*dbq.Occurrence{}
	for i := range occs {
		byDef[occs[i].DefinitionID] = &occs[i]
	}
	steps := map[uuid.UUID][]dbq.RoutineStep{}
	for _, st := range allSteps {
		steps[st.RoutineID] = append(steps[st.RoutineID], st)
	}
	bal, earn := map[uuid.UUID]int64{}, map[uuid.UUID]int64{}
	for _, b := range balances {
		bal[b.ChildID] = b.Balance
	}
	for _, e := range earned {
		earn[e.ChildID] = e.Earned
	}

	out := api.Today{HouseholdDay: openapi_types.Date{Time: pgDate(d).Time}, ServerTime: now.In(loc), Children: []api.TodayChild{}}
	for _, c := range children {
		if !a.MayActFor(c.ID) {
			continue
		}
		tc := api.TodayChild{Groups: []api.TodayGroup{}}
		tc.Child.Id, tc.Child.Name, tc.Child.Color = c.ID, c.Name, api.ProfileColor(c.Color)
		tc.Child.Points, tc.Child.EarnedToday = bal[c.ID], earn[c.ID]
		for _, r := range routines {
			if r.ChildID == c.ID && on(r.Days, d) {
				tc.Groups = append(tc.Groups, routineGroup(r, steps[r.ID], byDef[r.ID]))
			}
		}
		choreGroup := api.TodayGroup{Id: c.ID, Kind: api.TodayGroupKindChores, Title: "Chores", Items: []api.TodayItem{}}
		for _, ch := range chores {
			if ch.ChildID == c.ID && on(ch.Days, d) {
				choreGroup.Items = append(choreGroup.Items, choreItem(ch, byDef[ch.ID]))
			}
		}
		if len(choreGroup.Items) > 0 {
			choreGroup.Done = !slices.ContainsFunc(choreGroup.Items, func(it api.TodayItem) bool { return it.State != "complete" })
			tc.Groups = append(tc.Groups, choreGroup)
		}
		for _, g := range tc.Groups {
			for _, it := range g.Items {
				tc.Child.Total++
				if it.State != "open" {
					tc.Child.Completed++
				}
			}
		}
		out.Children = append(out.Children, tc)
	}
	return out, nil
}

// GetToday is the wall display's day.
func (s *Server) GetToday(ctx context.Context, _ api.GetTodayRequestObject) (api.GetTodayResponseObject, error) {
	now, err := s.q.Now(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.today(ctx, s.q, mustActor(ctx), now)
	if err != nil {
		return nil, err
	}
	return api.GetToday200JSONResponse(t), nil
}

var (
	errNotToday    = apiError("not_today", "That isn't on today's list.")
	errAlreadyDone = apiError("already_done", "That's already done today.")
)

// openOccurrence locks the child, then finds or creates today's occurrence of the definition and
// locks it. It returns nil with a refusal when the definition is not on today's list.
func openOccurrence(ctx context.Context, cmd *command.Cmd, child, definition uuid.UUID, kind string, days int16, points int32) (*dbq.Occurrence, *api.Error, error) {
	if err := cmd.LockChildren(ctx, child); errors.Is(err, command.ErrNoChild) {
		return nil, &errNotFound, nil
	} else if err != nil {
		return nil, nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, nil, err
	}
	_, d, err := householdDay(ctx, cmd.Q, cmd.HouseholdID(), now)
	if err != nil {
		return nil, nil, err
	}
	if !on(days, d) {
		return nil, &errNotToday, nil
	}
	id := occurrenceID(definition, d)
	if err := cmd.Q.EnsureOccurrence(ctx, dbq.EnsureOccurrenceParams{
		ID: id, HouseholdID: cmd.HouseholdID(), ChildID: child, Kind: kind, DefinitionID: definition, Day: pgDate(d), Points: points,
	}); err != nil {
		return nil, nil, err
	}
	occ, err := cmd.Q.LockOccurrence(ctx, dbq.LockOccurrenceParams{HouseholdID: cmd.HouseholdID(), ID: id})
	if err != nil {
		return nil, nil, err
	}
	return &occ, nil, nil
}

// finish ends today's occurrence. Done by a child (on a display), it waits for an adult's
// approval; done by an adult, it is approved at once and the points land.
func finish(ctx context.Context, cmd *command.Cmd, a actor.Actor, occ dbq.Occurrence, kind, title string, checked []uuid.UUID) (dbq.Occurrence, error) {
	adult := a.Kind == actor.Adult || a.Kind == actor.Phone
	state := "pending"
	if adult {
		state = "complete"
	}
	updated, err := cmd.Q.UpdateOccurrence(ctx, dbq.UpdateOccurrenceParams{
		HouseholdID: cmd.HouseholdID(), ID: occ.ID, State: state, CheckedSteps: checked, Points: occ.Points,
	})
	if err != nil {
		return dbq.Occurrence{}, err
	}
	if adult {
		return updated, award(ctx, cmd, a.AdultID, updated)
	}
	_, err = cmd.Q.CreateApproval(ctx, dbq.CreateApprovalParams{
		HouseholdID: cmd.HouseholdID(), ChildID: occ.ChildID, Kind: kind, OccurrenceID: &occ.ID, Title: title, Points: occ.Points,
	})
	return updated, err
}

// CompleteChore marks today's chore done and asks an adult to approve it.
func (s *Server) CompleteChore(ctx context.Context, req api.CompleteChoreRequestObject) (api.CompleteChoreResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	notFound := api.CompleteChore404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}
	c, err := cmd.Q.GetLiveChore(ctx, dbq.GetLiveChoreParams{HouseholdID: a.HouseholdID, ID: req.ChoreId})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !a.MayActFor(c.ChildID)) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	occ, refusal, err := openOccurrence(ctx, cmd, c.ChildID, c.ID, "chore", c.Days, c.Points)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		if refusal == &errNotFound {
			return notFound, nil
		}
		return api.CompleteChore409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refusal)}, nil
	}
	if occ.State != "open" {
		return api.CompleteChore409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(errAlreadyDone)}, nil
	}
	updated, err := finish(ctx, cmd, a, *occ, "completion", c.Title, []uuid.UUID{})
	if err != nil {
		return nil, err
	}
	return api.CompleteChore200JSONResponse(choreItem(c, &updated)), nil
}

// CheckRoutineStep ticks a step; ticking the last one asks an adult to approve the routine.
func (s *Server) CheckRoutineStep(ctx context.Context, req api.CheckRoutineStepRequestObject) (api.CheckRoutineStepResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	notFound := api.CheckRoutineStep404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}
	r, err := cmd.Q.GetLiveRoutine(ctx, dbq.GetLiveRoutineParams{HouseholdID: a.HouseholdID, ID: req.RoutineId})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !a.MayActFor(r.ChildID)) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	steps, err := cmd.Q.ListStepsOfRoutine(ctx, dbq.ListStepsOfRoutineParams{HouseholdID: a.HouseholdID, RoutineID: r.ID})
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(steps, func(s dbq.RoutineStep) bool { return s.ID == req.StepId }) {
		return notFound, nil
	}
	occ, refusal, err := openOccurrence(ctx, cmd, r.ChildID, r.ID, "routine", r.Days, r.Points)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		if refusal == &errNotFound {
			return notFound, nil
		}
		return api.CheckRoutineStep409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refusal)}, nil
	}
	if occ.State != "open" {
		return api.CheckRoutineStep409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(errAlreadyDone)}, nil
	}
	checked := occ.CheckedSteps
	if !slices.Contains(checked, req.StepId) {
		checked = append(checked, req.StepId)
	}
	var updated dbq.Occurrence
	if len(checked) == len(steps) {
		updated, err = finish(ctx, cmd, a, *occ, "routine", r.Title, checked)
	} else {
		updated, err = cmd.Q.UpdateOccurrence(ctx, dbq.UpdateOccurrenceParams{
			HouseholdID: a.HouseholdID, ID: occ.ID, State: "open", CheckedSteps: checked, Points: occ.Points, RejectedReason: occ.RejectedReason,
		})
	}
	if err != nil {
		return nil, err
	}
	return api.CheckRoutineStep200JSONResponse(routineGroup(r, steps, &updated)), nil
}
