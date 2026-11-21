package server

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
	"github.com/oldsj/opencozy-next/internal/screentime"
)

// Editing what adults manage. Every save names the version it was made from; a stale one gets
// 409 version_conflict instead of overwriting someone else's change.

var errNoSuchChildToAssign = apiError("no_such_child", "That child isn’t in the household any more. Choose someone else.")

// lockForEdit locks the children an edit touches (the current one and a new one), after
// checking the new one is live. Locks come before the command's clock.
func lockForEdit(ctx context.Context, cmd *command.Cmd, from, to uuid.UUID) (ok bool, err error) {
	ids := []uuid.UUID{from}
	if to != from {
		ids = append(ids, to)
	}
	if err := cmd.LockChildren(ctx, ids...); errors.Is(err, command.ErrNoChild) {
		// The current child may be archived (it keeps its chores); only the new one must be live.
		if to == from {
			return false, nil
		}
		if err := cmd.LockChildren(ctx, to); errors.Is(err, command.ErrNoChild) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// moveToday carries today's still-open occurrence of a definition along with an edit: to the
// new child (ticks cleared), with the new points, and keeping only ticks of steps that remain.
// Done or waiting ones stay as they are: what was earned stays earned.
func moveToday(ctx context.Context, cmd *command.Cmd, definition, child uuid.UUID, points int32, keep func(uuid.UUID) bool) error {
	now, err := cmd.Now(ctx)
	if err != nil {
		return err
	}
	_, d, err := householdDay(ctx, cmd.Q, cmd.HouseholdID(), now)
	if err != nil {
		return err
	}
	occ, err := cmd.Q.LockOpenOccurrence(ctx, dbq.LockOpenOccurrenceParams{HouseholdID: cmd.HouseholdID(), DefinitionID: definition, Day: pgDate(d)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	checked := []uuid.UUID{}
	reason := occ.RejectedReason
	if occ.ChildID == child {
		for _, s := range occ.CheckedSteps {
			if keep(s) {
				checked = append(checked, s)
			}
		}
	} else {
		reason = nil
	}
	return cmd.Q.MoveOccurrence(ctx, dbq.MoveOccurrenceParams{
		HouseholdID: cmd.HouseholdID(), ID: occ.ID, ChildID: child, Points: points, CheckedSteps: checked, RejectedReason: reason,
	})
}

// UpdateChore edits a chore. A new child takes it from today; points already earned stay.
func (s *Server) UpdateChore(ctx context.Context, req api.UpdateChoreRequestObject) (api.UpdateChoreResponseObject, error) {
	a, cmd, b := mustActor(ctx), mustCmd(ctx), req.Body
	notFound := api.UpdateChore404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}
	cur, err := cmd.Q.GetLiveChore(ctx, dbq.GetLiveChoreParams{HouseholdID: a.HouseholdID, ID: req.ChoreId})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	if ok, err := lockForEdit(ctx, cmd, cur.ChildID, b.ChildId); err != nil {
		return nil, err
	} else if !ok {
		return api.UpdateChore422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errNoSuchChildToAssign)}, nil
	}
	c, err := cmd.Q.UpdateChore(ctx, dbq.UpdateChoreParams{
		HouseholdID: a.HouseholdID, ID: req.ChoreId, ChildID: b.ChildId, Title: strings.TrimSpace(b.Title),
		Points: int32(b.Points), Days: daysMask(&b.Days), ExpectedVersion: int32(b.ExpectedVersion), //nolint:gosec // bounded by the spec
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateChore409JSONResponse(conflict("chore")), nil
	} else if err != nil {
		return nil, err
	}
	if err := moveToday(ctx, cmd, c.ID, c.ChildID, c.Points, func(uuid.UUID) bool { return true }); err != nil {
		return nil, err
	}
	return api.UpdateChore200JSONResponse(toChore(c)), nil
}

// UpdateRoutine edits a routine and its steps. Steps sent with an id of this routine keep it
// (and today's tick on it); the rest are new; steps left out go.
func (s *Server) UpdateRoutine(ctx context.Context, req api.UpdateRoutineRequestObject) (api.UpdateRoutineResponseObject, error) {
	a, cmd, b := mustActor(ctx), mustCmd(ctx), req.Body
	notFound := api.UpdateRoutine404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}
	unprocessable := func(code, msg string) api.UpdateRoutineResponseObject {
		return api.UpdateRoutine422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError(code, msg))}
	}
	cur, err := cmd.Q.GetLiveRoutine(ctx, dbq.GetLiveRoutineParams{HouseholdID: a.HouseholdID, ID: req.RoutineId})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	if ok, err := lockForEdit(ctx, cmd, cur.ChildID, b.ChildId); err != nil {
		return nil, err
	} else if !ok {
		return api.UpdateRoutine422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errNoSuchChildToAssign)}, nil
	}
	r, err := cmd.Q.UpdateRoutine(ctx, dbq.UpdateRoutineParams{
		HouseholdID: a.HouseholdID, ID: req.RoutineId, ChildID: b.ChildId, Title: strings.TrimSpace(b.Title),
		Points: int32(b.Points), Days: daysMask(&b.Days), ExpectedVersion: int32(b.ExpectedVersion), //nolint:gosec // bounded by the spec
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateRoutine409JSONResponse(conflict("routine")), nil
	} else if err != nil {
		return nil, err
	}
	old, err := cmd.Q.StepsOfRoutine(ctx, dbq.StepsOfRoutineParams{HouseholdID: a.HouseholdID, RoutineID: r.ID})
	if err != nil {
		return nil, err
	}
	mine := map[uuid.UUID]bool{}
	for _, st := range old {
		mine[st.ID] = true
	}
	seen := map[uuid.UUID]bool{}
	for _, st := range b.Steps {
		if st.Id == nil {
			continue
		}
		if !mine[*st.Id] || seen[*st.Id] {
			return unprocessable("no_such_step", "A step was removed while you were editing. Check the routine and save again."), nil
		}
		seen[*st.Id] = true
	}
	// Positions are unique, so the old steps go first; the kept ones come back with their ids.
	if err := cmd.Q.DeleteRoutineSteps(ctx, dbq.DeleteRoutineStepsParams{HouseholdID: a.HouseholdID, RoutineID: r.ID}); err != nil {
		return nil, err
	}
	steps := make([]dbq.RoutineStep, 0, len(b.Steps))
	for i, st := range b.Steps {
		id := uuid.Must(uuid.NewV7())
		if st.Id != nil {
			id = *st.Id
		}
		row, err := cmd.Q.CreateRoutineStepWithID(ctx, dbq.CreateRoutineStepWithIDParams{
			ID: id, HouseholdID: a.HouseholdID, RoutineID: r.ID, Title: strings.TrimSpace(st.Title), Position: int16(i), //nolint:gosec // at most 20 by the spec
		})
		if err != nil {
			return nil, err
		}
		steps = append(steps, row)
	}
	if err := moveToday(ctx, cmd, r.ID, r.ChildID, r.Points, func(id uuid.UUID) bool { return seen[id] }); err != nil {
		return nil, err
	}
	return api.UpdateRoutine200JSONResponse(toRoutine(r, steps)), nil
}

// UpdateReward edits a reward, or hides it from children. Redemptions keep what they bought.
func (s *Server) UpdateReward(ctx context.Context, req api.UpdateRewardRequestObject) (api.UpdateRewardResponseObject, error) {
	a, cmd, b := mustActor(ctx), mustCmd(ctx), req.Body
	screen := b.Kind == api.RewardEditKindScreenTime
	if screen != (b.Minutes != nil) {
		return api.UpdateReward422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("minutes_mismatch", "Give minutes for iPad time, and none for other rewards."))}, nil
	}
	target, ok := rewardTarget(screen, b.Target)
	if !ok {
		return api.UpdateReward422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errTargetMismatch)}, nil
	}
	var minutes pgtype.Int4
	if b.Minutes != nil {
		minutes = pgtype.Int4{Int32: int32(*b.Minutes), Valid: true} //nolint:gosec // 1-1440 by the spec
	}
	r, err := cmd.Q.UpdateReward(ctx, dbq.UpdateRewardParams{
		HouseholdID: a.HouseholdID, ID: req.RewardId, Title: strings.TrimSpace(b.Title), Cost: int32(b.Cost), //nolint:gosec // bounded by the spec
		Kind: string(b.Kind), Minutes: minutes, Available: b.Available, ExpectedVersion: int32(b.ExpectedVersion), //nolint:gosec // bounded by the spec
		ScreenTimeTarget: target,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := cmd.Q.GetLiveReward(ctx, dbq.GetLiveRewardParams{HouseholdID: a.HouseholdID, ID: req.RewardId}); errors.Is(err, pgx.ErrNoRows) {
			return api.UpdateReward404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		} else if err != nil {
			return nil, err
		}
		return api.UpdateReward409JSONResponse(conflict("reward")), nil
	} else if err != nil {
		return nil, err
	}
	return api.UpdateReward200JSONResponse(toReward(r)), nil
}

func toRedemptionRow(r dbq.GetRedemptionRowRow) api.RedemptionRow {
	out := api.RedemptionRow{
		Id: r.ID, Title: r.Title, Cost: int(r.Cost), Status: api.RedemptionStatus(r.State), RequestedAt: r.CreatedAt,
		RefundedPoints: int(r.RefundedPoints), Version: int(r.Version),
		Child:       api.RedemptionChild{Id: r.ChildID, Name: r.ChildName, Color: api.ProfileColor(r.ChildColor)},
		FulfilledAt: r.FulfilledAt, CancelledAt: r.CancelledAt,
	}
	if r.Minutes.Valid {
		out.Minutes = ptr(int(r.Minutes.Int32))
		out.UsedMinutes = ptr(int((r.UsedSeconds + 59) / 60))
	}
	if r.ScreenTimeTarget != nil {
		out.Target = ptr(api.Target(*r.ScreenTimeTarget))
	}
	return out
}

// ListRedemptions is what children have asked for, for adults to give or cancel.
func (s *Server) ListRedemptions(ctx context.Context, req api.ListRedemptionsRequestObject) (api.ListRedemptionsResponseObject, error) {
	var state *string
	if req.Params.Status != nil {
		state = ptr(string(*req.Params.Status))
	}
	rows, err := s.q.ListRedemptions(ctx, dbq.ListRedemptionsParams{HouseholdID: mustActor(ctx).HouseholdID, State: state})
	if err != nil {
		return nil, err
	}
	out := api.ListRedemptions200JSONResponse{Redemptions: make([]api.RedemptionRow, len(rows))}
	for i, r := range rows {
		out.Redemptions[i] = toRedemptionRow(dbq.GetRedemptionRowRow(r))
	}
	return out, nil
}

// lockRedemption locks a redemption and its child for a decision on it. status is 0, 404 or
// 409 (version_conflict).
func lockRedemption(ctx context.Context, cmd *command.Cmd, id uuid.UUID, expected int) (dbq.Redemption, int, error) {
	h := cmd.HouseholdID()
	peek, err := cmd.Q.GetRedemptionRow(ctx, dbq.GetRedemptionRowParams{HouseholdID: h, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Redemption{}, 404, nil
	} else if err != nil {
		return dbq.Redemption{}, 0, err
	}
	// An archived child's redemptions can still be given or cancelled (with its refund).
	if err := cmd.LockChildrenAny(ctx, peek.ChildID); err != nil {
		return dbq.Redemption{}, 0, err
	}
	red, err := cmd.Q.LockRedemption(ctx, dbq.LockRedemptionParams{HouseholdID: h, ID: id})
	if err != nil {
		return dbq.Redemption{}, 0, err
	}
	if int(red.Version) != expected {
		return red, 409, nil
	}
	return red, 0, nil
}

func (s *Server) redemptionRow(ctx context.Context, q *dbq.Queries, id uuid.UUID) (api.RedemptionRow, error) {
	r, err := q.GetRedemptionRow(ctx, dbq.GetRedemptionRowParams{HouseholdID: mustActor(ctx).HouseholdID, ID: id})
	if err != nil {
		return api.RedemptionRow{}, err
	}
	return toRedemptionRow(r), nil
}

// FulfilRedemption marks an approved reward as given.
func (s *Server) FulfilRedemption(ctx context.Context, req api.FulfilRedemptionRequestObject) (api.FulfilRedemptionResponseObject, error) {
	cmd := mustCmd(ctx)
	red, status, err := lockRedemption(ctx, cmd, req.RedemptionId, req.Body.ExpectedVersion)
	switch {
	case err != nil:
		return nil, err
	case status == 404:
		return api.FulfilRedemption404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	case status == 409:
		return api.FulfilRedemption409JSONResponse(apiError("version_conflict", "Someone else changed this while you were editing. It now shows their version; check it and save again.")), nil
	case red.State != "approved":
		return api.FulfilRedemption409JSONResponse(apiError("not_approved", "Only an approved reward can be marked as given.")), nil
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	if err := cmd.Q.FulfilRedemption(ctx, dbq.FulfilRedemptionParams{HouseholdID: cmd.HouseholdID(), ID: red.ID, Now: &now}); err != nil {
		return nil, err
	}
	row, err := s.redemptionRow(ctx, cmd.Q, red.ID)
	if err != nil {
		return nil, err
	}
	return api.FulfilRedemption200JSONResponse(row), nil
}

// CancelRedemption cancels an approved reward with all its points back, or given screen time
// with the unused share back (rounded down). A given item can't be cancelled.
func (s *Server) CancelRedemption(ctx context.Context, req api.CancelRedemptionRequestObject) (api.CancelRedemptionResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	refuse := func(code, msg string) api.CancelRedemptionResponseObject {
		return api.CancelRedemption409JSONResponse(apiError(code, msg))
	}
	red, status, err := lockRedemption(ctx, cmd, req.RedemptionId, req.Body.ExpectedVersion)
	switch {
	case err != nil:
		return nil, err
	case status == 404:
		return api.CancelRedemption404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	case status == 409:
		return refuse("version_conflict", "Someone else changed this while you were editing. It now shows their version; check it and save again."), nil
	}
	refund := red.Cost
	switch {
	case red.State == "approved":
	case red.State == "fulfilled" && red.Minutes.Valid:
		// The unspent rest is forfeited; its share of the cost comes back, rounded down.
		refund, err = screentime.CancelRedemption(ctx, cmd, red.ID, red.Cost)
		if errors.Is(err, screentime.ErrAllUsed) {
			return refuse("already_used", "That iPad time has already been used, so there’s nothing to refund."), nil
		} else if err != nil {
			return nil, err
		}
	default:
		return refuse("not_cancellable", "This reward has already been given or cancelled."), nil
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	if red.LedgerID == nil {
		return nil, errors.New("redemption without its ledger row")
	}
	if refund > 0 {
		if _, err := ledger.Refund(ctx, cmd, *red.LedgerID, ledger.Entry{Points: refund, AdultID: &a.AdultID, Note: req.Body.Reason}); err != nil {
			return nil, err
		}
	}
	if err := cmd.Q.CancelRedemption(ctx, dbq.CancelRedemptionParams{HouseholdID: cmd.HouseholdID(), ID: red.ID, Now: &now}); err != nil {
		return nil, err
	}
	row, err := s.redemptionRow(ctx, cmd.Q, red.ID)
	if err != nil {
		return nil, err
	}
	return api.CancelRedemption200JSONResponse(row), nil
}

// UpdateHousehold renames the household or moves it to another time zone; the household day
// follows the zone from the next read.
func (s *Server) UpdateHousehold(ctx context.Context, req api.UpdateHouseholdRequestObject) (api.UpdateHouseholdResponseObject, error) {
	a, cmd, b := mustActor(ctx), mustCmd(ctx), req.Body
	cur, err := cmd.Q.GetHousehold(ctx, a.HouseholdID)
	if err != nil {
		return nil, err
	}
	name, zone := cur.Name, cur.TimeZone
	if b.Name != nil {
		name = strings.TrimSpace(*b.Name)
	}
	if b.TimeZone != nil {
		// A named zone (Area/City, or UTC); not a legacy abbreviation such as EST.
		if _, err := day.Zone(*b.TimeZone); err != nil || (!strings.Contains(*b.TimeZone, "/") && *b.TimeZone != "UTC") {
			return api.UpdateHousehold422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("time_zone_unknown", "Choose a time zone from the list."))}, nil
		}
		zone = *b.TimeZone
	}
	h, err := cmd.Q.UpdateHousehold(ctx, dbq.UpdateHouseholdParams{ID: a.HouseholdID, Name: name, TimeZone: zone, ExpectedVersion: int32(b.ExpectedVersion)}) //nolint:gosec // bounded by the spec
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateHousehold409JSONResponse(apiError("version_conflict", "Someone else changed this while you were editing. It now shows their version; check it and save again.")), nil
	} else if err != nil {
		return nil, err
	}
	return api.UpdateHousehold200JSONResponse(toHousehold(h)), nil
}
