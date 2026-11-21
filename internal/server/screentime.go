package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
	"github.com/oldsj/opencozy-next/internal/push"
	"github.com/oldsj/opencozy-next/internal/screentime"
)

var (
	errNothingBanked = apiError("nothing_banked", "No iPad time left to use.")
	errNothingToEnd  = apiError("nothing_to_end", "No iPad time is running or queued.")
	errNothingQueued = apiError("nothing_to_end", "No iPad time is queued.")
	errTVStartNow    = apiError("start_now_tv", "TV time starts when the child watches a TV, not now.")
)

// readNow is the time for a read: the command clock when tests set one, otherwise the database's.
func (s *Server) readNow(ctx context.Context) (time.Time, error) {
	if s.runner.Clock != nil {
		return s.runner.Clock.Now(ctx, s.q)
	}
	return s.q.Now(ctx)
}

// clockTime is "8:00 AM" in the household's zone, for messages to a child.
func clockTime(t time.Time, loc *time.Location) string { return t.In(loc).Format("3:04 PM") }

// opensMessage is "Screen time starts at 8:00 AM." naming the day when it isn't today.
func opensMessage(now, opens time.Time, loc *time.Location) string {
	when := ""
	switch day.Of(opens, loc).Compare(day.Of(now, loc).AddDays(1)) {
	case 0:
		when = " tomorrow"
	case 1:
		when = " on " + opens.In(loc).Weekday().String()
	}
	return fmt.Sprintf("Screen time starts at %s%s.", clockTime(opens, loc), when)
}

// utcPtr is t in UTC: every time on the wire is UTC, whatever zone it was read or computed in.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func toAllowedHours(h *day.AllowedHours) *api.AllowedHours {
	if h == nil {
		return nil
	}
	return &api.AllowedHours{
		SchoolNight: api.HoursWindow{Start: h.SchoolNight.From.String(), End: h.SchoolNight.To.String()},
		Weekend:     api.HoursWindow{Start: h.Weekend.From.String(), End: h.Weekend.To.String()},
	}
}

func fromAllowedHours(h *api.AllowedHours) (*day.AllowedHours, error) {
	if h == nil {
		return nil, nil
	}
	parse := func(w api.HoursWindow) (day.Window, error) {
		from, err := day.ParseTimeOfDay(w.Start)
		if err != nil {
			return day.Window{}, err
		}
		to, err := day.ParseTimeOfDay(w.End)
		return day.Window{From: from, To: to}, err
	}
	sn, err := parse(h.SchoolNight)
	if err != nil {
		return nil, err
	}
	we, err := parse(h.Weekend)
	if err != nil {
		return nil, err
	}
	out := &day.AllowedHours{SchoolNight: sn, Weekend: we}
	return out, out.Valid()
}

func toTimeRequest(r dbq.TimeRequest) api.TimeRequest {
	out := api.TimeRequest{Id: r.ID, State: api.TimeRequestState(r.State), AskedAt: r.AskedAt.UTC(), DecidedAt: utcPtr(r.DecidedAt), GrantedUntil: utcPtr(r.GrantedUntil)}
	if r.Choice != nil {
		out.Choice = ptr(api.TimeChoice(*r.Choice))
	}
	return out
}

// screenTime is the child's iPad time at now, read through q (the command's, after a change).
func screenTime(ctx context.Context, q *dbq.Queries, h, child uuid.UUID, now time.Time) (api.ScreenTime, error) {
	rule, err := screentime.ChildRule(ctx, q, h, child)
	if err != nil {
		return api.ScreenTime{}, err
	}
	ws, err := screentime.Windows(ctx, q, h, child, now)
	if err != nil {
		return api.ScreenTime{}, err
	}
	banked, err := screentime.Banked(ctx, q, h, child)
	if err != nil {
		return api.ScreenTime{}, err
	}
	req, err := screentime.Shown(ctx, q, h, child, now)
	if err != nil {
		return api.ScreenTime{}, err
	}
	credits, err := q.OpenCredits(ctx, dbq.OpenCreditsParams{HouseholdID: h, ChildID: child})
	if err != nil {
		return api.ScreenTime{}, err
	}
	filter, err := webFilter(ctx, q, h)
	if err != nil {
		return api.ScreenTime{}, err
	}
	out := api.ScreenTime{
		ServerTime: now.UTC(), ChildId: child, BankedSeconds: int(banked), Windows: make([]api.Window, len(ws)),
		AllowedHours: toAllowedHours(rule.Hours), WebFilter: filter,
	}
	out.Credits = make([]api.Credit, len(credits))
	for i, c := range credits {
		out.Credits[i] = api.Credit{
			Id: c.ID, Source: api.CreditSource(c.Source), RedemptionId: c.RedemptionID, RewardTitle: c.RewardTitle, GrantedBy: c.GrantedByName,
			Minutes: int(c.Minutes), Seconds: int(c.Seconds), UsedSeconds: int(c.UsedSeconds), CreatedAt: c.CreatedAt.UTC(),
		}
	}
	for i, w := range ws {
		out.Windows[i] = api.Window{Id: w.ID, StartsAt: w.StartsAt.UTC(), EndsAt: w.EndsAt.UTC()}
	}
	if until := screentime.UnlockedUntil(ws, now); until != nil {
		out.UnlockedUntil = utcPtr(until)
		out.AllowanceSeconds = int(min(until.Sub(now), screentime.AllowanceCap).Seconds())
	}
	if st := rule.Status(now); st.Rule {
		hn := &api.HoursNow{Open: st.Open}
		if st.Open {
			hn.ClosesAt = utcPtr(&st.ClosesAt)
		} else {
			hn.OpensAt = utcPtr(&st.OpensAt)
		}
		out.HoursNow = hn
	}
	if req != nil {
		out.TimeRequest = ptr(toTimeRequest(*req))
	}
	return out, nil
}

func toSpend(s screentime.Spent) *api.Spend {
	return &api.Spend{Seconds: int(s.Seconds), StartsAt: s.StartsAt.UTC(), EndsAt: s.EndsAt.UTC(), Clipped: s.Clipped}
}

// childInitiated reports whether spending is the child's own, so limited by allowed hours.
// Anyone but an adult (or an assistant acting for one) counts as the child, so a caller added
// later is limited too.
func childInitiated(a actor.Actor) bool { return !a.ActsAsAdult() }

// screenTimeChanged tells the household's screens (live) and the child's devices that the
// child's iPad time changed: a silent push wakes the app to sync, so the shields lift or come
// back at once. The device's own request needs no push: its response has the answer.
func (s *Server) screenTimeChanged(ctx context.Context, cmd *command.Cmd, child uuid.UUID) error {
	if err := live.Notify(ctx, cmd.Q, cmd.HouseholdID(), live.Event{Topic: live.Today, ChildID: &child}); err != nil {
		return err
	}
	if a, ok := actor.From(ctx); ok && (a.Kind == actor.Device || a.Kind == actor.DeviceWeb) {
		return nil
	}
	key, n := push.ScreenTimeChanged(child)
	return s.push.ToChild(ctx, cmd.Q, cmd.HouseholdID(), child, key, n)
}

// GetScreenTime is a child's iPad time now.
func (s *Server) GetScreenTime(ctx context.Context, req api.GetScreenTimeRequestObject) (api.GetScreenTimeResponseObject, error) {
	a := mustActor(ctx)
	c, err := s.q.GetChild(ctx, dbq.GetChildParams{HouseholdID: a.HouseholdID, ID: req.ChildId})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.ArchivedAt != nil) {
		return api.GetScreenTime404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	now, err := s.readNow(ctx)
	if err != nil {
		return nil, err
	}
	out, err := screenTime(ctx, s.q, a.HouseholdID, req.ChildId, now)
	if err != nil {
		return nil, err
	}
	return api.GetScreenTime200JSONResponse(out), nil
}

// SpendScreenTime starts some of the child's banked time.
func (s *Server) SpendScreenTime(ctx context.Context, req api.SpendScreenTimeRequestObject) (api.SpendScreenTimeResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	conflict := func(e api.Error) api.SpendScreenTimeResponseObject {
		return api.SpendScreenTime409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(e)}
	}
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.SpendScreenTime404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	rule, err := screentime.ChildRule(ctx, cmd.Q, a.HouseholdID, req.ChildId)
	if err != nil {
		return nil, err
	}
	var limit *time.Time
	if childInitiated(a) {
		var closed screentime.ErrClosed
		if limit, err = rule.SpendLimit(now); errors.As(err, &closed) {
			return conflict(apiError("outside_hours", opensMessage(now, closed.OpensAt, rule.Zone))), nil
		} else if err != nil {
			return nil, err
		}
	}
	spent, err := screentime.Spend(ctx, cmd, req.ChildId, int64(req.Body.Minutes)*60, nil, limit)
	var full screentime.ErrQueueFull
	switch {
	case errors.Is(err, screentime.ErrNothingBanked):
		return conflict(errNothingBanked), nil
	case errors.As(err, &full):
		return conflict(apiError("hours_full", fmt.Sprintf("You already have screen time until %s, when screen time ends today.", clockTime(full.Until, rule.Zone)))), nil
	case err != nil:
		return nil, err
	}
	if err := s.screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
		return nil, err
	}
	st, err := screenTime(ctx, cmd.Q, a.HouseholdID, req.ChildId, now)
	if err != nil {
		return nil, err
	}
	return api.SpendScreenTime200JSONResponse{Spend: toSpend(spent), ScreenTime: st}, nil
}

// GrantScreenTime gives a child screen time, and with startNow starts it.
func (s *Server) GrantScreenTime(ctx context.Context, req api.GrantScreenTimeRequestObject) (api.GrantScreenTimeResponseObject, error) {
	a, cmd, b := mustActor(ctx), mustCmd(ctx), req.Body
	toTV := b.Target != nil && *b.Target == api.TargetTv
	if toTV && b.StartNow != nil && *b.StartNow {
		return api.GrantScreenTime422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errTVStartNow)}, nil
	}
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.GrantScreenTime404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	if toTV {
		if err := s.tv.AddCredit(ctx, cmd.Q, a.HouseholdID, req.ChildId, nil, &a.AdultID, int32(b.Minutes)*60, now); err != nil { //nolint:gosec // 1-240 by the spec
			return nil, err
		}
		st, err := screenTime(ctx, cmd.Q, a.HouseholdID, req.ChildId, now)
		if err != nil {
			return nil, err
		}
		view, err := s.childTvNow(ctx, cmd.Q, a.HouseholdID, req.ChildId, now)
		if err != nil {
			return nil, err
		}
		return api.GrantScreenTime200JSONResponse{ScreenTime: st, Tv: &view}, nil
	}
	cr, err := screentime.AddCredit(ctx, cmd, screentime.Credit{
		Child: req.ChildId, Source: screentime.SourceGrant, GrantedBy: &a.AdultID, GrantedVia: a.Via(), Minutes: int32(b.Minutes), //nolint:gosec // 1-240 by the spec
	})
	if err != nil {
		return nil, err
	}
	out := api.GrantScreenTime200JSONResponse{}
	if b.StartNow != nil && *b.StartNow {
		spent, err := screentime.Spend(ctx, cmd, req.ChildId, int64(cr.Seconds), &cr.ID, nil)
		if err != nil {
			return nil, err
		}
		out.Spend = toSpend(spent)
	}
	if err := s.screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
		return nil, err
	}
	if out.ScreenTime, err = screenTime(ctx, cmd.Q, a.HouseholdID, req.ChildId, now); err != nil {
		return nil, err
	}
	return out, nil
}

// EndScreenTime ends a child's iPad time early, or cancels what is queued.
func (s *Server) EndScreenTime(ctx context.Context, req api.EndScreenTimeRequestObject) (api.EndScreenTimeResponseObject, error) {
	a, cmd, b := mustActor(ctx), mustCmd(ctx), req.Body
	scope, refund := screentime.Now, b.Refund != nil && *b.Refund
	if b.Scope != nil && *b.Scope == api.EndScreenTimeJSONBodyScopeQueued {
		scope = screentime.Queued
	}
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.EndScreenTime404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	ended, err := screentime.End(ctx, cmd, req.ChildId, scope, refund, &a.AdultID)
	if errors.Is(err, screentime.ErrNothingToEnd) {
		e := errNothingToEnd
		if scope == screentime.Queued {
			e = errNothingQueued
		}
		return api.EndScreenTime409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(e)}, nil
	} else if err != nil {
		return nil, err
	}
	if err := s.screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
		return nil, err
	}
	st, err := screenTime(ctx, cmd.Q, a.HouseholdID, req.ChildId, now)
	if err != nil {
		return nil, err
	}
	return api.EndScreenTime200JSONResponse{
		Scope: api.EndResultScope(scope), Windows: ended.Windows, CutSeconds: int(ended.CutSeconds),
		RefundedSeconds: int(ended.Refunded), ForfeitedSeconds: int(ended.CutSeconds - ended.Refunded),
		EndedAt: utcPtr(ended.EndedAt), KeptUntil: utcPtr(ended.KeptUntil), ScreenTime: st,
	}, nil
}

// SetAllowedHours sets or clears a child's allowed hours.
func (s *Server) SetAllowedHours(ctx context.Context, req api.SetAllowedHoursRequestObject) (api.SetAllowedHoursResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	hours, err := fromAllowedHours(req.Body.AllowedHours)
	if err != nil {
		return api.SetAllowedHours422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(
			apiError("invalid_hours", "Allowed hours must end after they start, on the same day."))}, nil
	}
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.SetAllowedHours404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	if err := screentime.SetRule(ctx, cmd.Q, a.HouseholdID, req.ChildId, hours); err != nil {
		return nil, err
	}
	if err := s.screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
		return nil, err
	}
	st, err := screenTime(ctx, cmd.Q, a.HouseholdID, req.ChildId, now)
	if err != nil {
		return nil, err
	}
	return api.SetAllowedHours200JSONResponse(st), nil
}

// AskForTime asks the parents for more time, from the child's own device.
func (s *Server) AskForTime(ctx context.Context, req api.AskForTimeRequestObject) (api.AskForTimeResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.AskForTime404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	device := a.CredentialID
	asked, err := screentime.Ask(ctx, cmd, req.ChildId, &device)
	var tooSoon screentime.ErrAskedTooSoon
	if errors.As(err, &tooSoon) {
		return api.AskForTime409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(
			apiError("asked_too_soon", "A parent just said no. You can ask again in a few minutes."))}, nil
	} else if err != nil {
		return nil, err
	}
	if asked.New || asked.Reminded {
		if err := s.screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
			return nil, err
		}
		// A new request, or asking again after RemindAfter, pushes to the parents (old #74).
		ap, err := cmd.Q.PendingApprovalForTimeRequest(ctx, dbq.PendingApprovalForTimeRequestParams{HouseholdID: a.HouseholdID, TimeRequestID: &asked.Request.ID})
		if err != nil {
			return nil, err
		}
		if err := s.pushPending(ctx, cmd, ap, asked.Request.AskedAt); err != nil {
			return nil, err
		}
	}
	if asked.New {
		return api.AskForTime201JSONResponse(toTimeRequest(asked.Request)), nil
	}
	return api.AskForTime200JSONResponse(toTimeRequest(asked.Request)), nil
}

// RecordScreenTimeEvents records what the child did on the locked shield.
func (s *Server) RecordScreenTimeEvents(ctx context.Context, req api.RecordScreenTimeEventsRequestObject) (api.RecordScreenTimeEventsResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	if len(a.Children) != 1 {
		return nil, errors.New("screen-time events: a device acts for exactly one child")
	}
	child, device := a.Children[0], a.CredentialID
	if err := cmd.LockChildren(ctx, child); err != nil {
		return nil, err // a device's child is live: authentication checked it
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	out := api.RecordScreenTimeEvents200JSONResponse{Recorded: make([]api.Id, 0, len(req.Body.Events))}
	seen := map[uuid.UUID]bool{}
	for _, e := range req.Body.Events {
		if seen[e.Id] {
			continue
		}
		seen[e.Id] = true
		if _, err := cmd.Q.InsertScreenTimeEvent(ctx, dbq.InsertScreenTimeEventParams{
			HouseholdID: a.HouseholdID, ID: e.Id, ChildID: child, DeviceID: &device, Kind: string(e.Kind),
			OccurredAt: now.Add(-time.Duration(e.SecondsAgo) * time.Second), Now: now,
		}); err != nil {
			return nil, err
		}
		out.Recorded = append(out.Recorded, e.Id)
	}
	return out, nil
}

// ForfeitScreenTimeCredit takes back the unused rest of a parent's grant or "yes".
func (s *Server) ForfeitScreenTimeCredit(ctx context.Context, req api.ForfeitScreenTimeCreditRequestObject) (api.ForfeitScreenTimeCreditResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	notFound := api.ForfeitScreenTimeCredit404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}
	conflict := func(code, msg string) api.ForfeitScreenTimeCreditResponseObject {
		return api.ForfeitScreenTimeCredit409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError(code, msg))}
	}
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	seconds, err := screentime.Forfeit(ctx, cmd, req.ChildId, req.CreditId, a.AdultID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return notFound, nil
	case errors.Is(err, screentime.ErrRewardCredit):
		return conflict("cancel_the_reward", "This time was bought with points: cancel the reward to take it back and return the points."), nil
	case errors.Is(err, screentime.ErrNothingLeft):
		return conflict("nothing_left", "This time has all been used."), nil
	case err != nil:
		return nil, err
	}
	if err := s.screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
		return nil, err
	}
	st, err := screenTime(ctx, cmd.Q, a.HouseholdID, req.ChildId, now)
	if err != nil {
		return nil, err
	}
	return api.ForfeitScreenTimeCredit200JSONResponse{ForfeitedSeconds: int(seconds), ScreenTime: st}, nil
}
