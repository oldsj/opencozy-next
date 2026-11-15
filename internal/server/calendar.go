package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/calendar"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
)

const (
	maxCalendarDays = 42
	maxCalendars    = 50
	seriesKey       = "*" // an assignment to every occurrence
)

func householdZone(ctx context.Context, q *dbq.Queries, householdID uuid.UUID) (*time.Location, error) {
	h, err := q.GetHousehold(ctx, householdID)
	if err != nil {
		return nil, err
	}
	return day.Zone(h.TimeZone)
}

func dateString(d pgtype.Date) string { return d.Time.Format(time.DateOnly) }

// wakeCalendar starts the refresh job early, after a feed is added. Commands have no
// after-commit hook, so it waits a moment for the command to commit; if the run still misses
// the new row, the next minute's run takes it.
func (s *Server) wakeCalendar() {
	time.AfterFunc(2*time.Second, func() {
		select {
		case s.calendarWake <- struct{}{}:
		default:
		}
	})
}

// CalendarWake is the refresh job's early start (calendar.Job).
func (s *Server) CalendarWake() <-chan struct{} { return s.calendarWake }

// assignments maps series → recurrence key ("*" for the series) → children (empty: nobody).
type assignments map[uuid.UUID]map[string][]uuid.UUID

func loadAssignments(ctx context.Context, q *dbq.Queries, h uuid.UUID, series []uuid.UUID) (assignments, error) {
	rows, err := q.ListAssignments(ctx, dbq.ListAssignmentsParams{HouseholdID: h, SeriesIds: series})
	if err != nil {
		return nil, err
	}
	out := assignments{}
	for _, r := range rows {
		if out[r.SeriesID] == nil {
			out[r.SeriesID] = map[string][]uuid.UUID{}
		}
		list := out[r.SeriesID][r.RecurrenceKey]
		if list == nil {
			list = []uuid.UUID{}
		}
		if r.ChildID != nil {
			list = append(list, *r.ChildID)
		}
		out[r.SeriesID][r.RecurrenceKey] = list
	}
	return out, nil
}

// children is who an occurrence is for: its own assignment, else the series', else nobody.
func (a assignments) children(series uuid.UUID, key string) []uuid.UUID {
	if list, ok := a[series][key]; ok {
		return list
	}
	if list, ok := a[series][seriesKey]; ok {
		return list
	}
	return []uuid.UUID{}
}

type eventFields struct {
	id, series              uuid.UUID
	key                     string
	allDay                  bool
	startsAt, endsAt        *time.Time
	startDate, endDate      pgtype.Date
	title, location, status *string
	calID                   uuid.UUID
	calName, calColor       string
}

func (f eventFields) event(zone *time.Location, children []uuid.UUID) api.CalendarEvent {
	e := api.CalendarEvent{
		Id: f.id, SeriesId: f.series, AllDay: f.allDay, Title: f.title, Location: f.location, Children: children,
		Status:   api.CalendarEventStatus(*f.status),
		Calendar: api.CalendarRef{Id: f.calID, Name: f.calName, Color: api.ProfileColor(f.calColor)},
	}
	if f.allDay {
		e.Start, e.End = dateString(f.startDate), dateString(f.endDate)
	} else {
		e.Start, e.End = f.startsAt.In(zone).Format(time.RFC3339), f.endsAt.In(zone).Format(time.RFC3339)
	}
	return e
}

// sortKey orders events by when they start in household time, all-day first on a day.
func sortKey(e api.CalendarEvent) string {
	if e.AllDay {
		return e.Start + "T00:00:00-0"
	}
	return e.Start[:19] + "-1"
}

func (s *Server) GetCalendar(ctx context.Context, req api.GetCalendarRequestObject) (api.GetCalendarResponseObject, error) {
	a := mustActor(ctx)
	from, to := dayOf(req.Params.From), dayOf(req.Params.To)
	n := 0
	for d := from; d.Compare(to) < 0 && n <= maxCalendarDays; d = d.AddDays(1) {
		n++
	}
	if !plannable(from) || !plannable(to) || n < 1 || n > maxCalendarDays {
		return api.GetCalendar400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError("bad_range", "Ask for 1 to 42 days, from before to."))}, nil
	}
	zone, err := householdZone(ctx, s.q, a.HouseholdID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListCalendarEvents(ctx, dbq.ListCalendarEventsParams{
		HouseholdID: a.HouseholdID, DisplayOnly: a.Kind == actor.Display,
		FromTime: from.Start(zone), ToTime: to.Start(zone), FromDate: pgDate(from), ToDate: pgDate(to),
	})
	if err != nil {
		return nil, err
	}
	var series []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		if !seen[r.SeriesID] {
			seen[r.SeriesID] = true
			series = append(series, r.SeriesID)
		}
	}
	asg, err := loadAssignments(ctx, s.q, a.HouseholdID, series)
	if err != nil {
		return nil, err
	}
	events := make([]api.CalendarEvent, 0, len(rows))
	for _, r := range rows {
		f := eventFields{id: r.ID, series: r.SeriesID, key: r.RecurrenceKey, allDay: r.AllDay, startsAt: r.StartsAt, endsAt: r.EndsAt,
			startDate: r.StartDate, endDate: r.EndDate, title: r.Title, location: r.Location, status: &r.Status,
			calID: r.CalendarID, calName: r.CalendarName, calColor: r.CalendarColor}
		events = append(events, f.event(zone, asg.children(r.SeriesID, r.RecurrenceKey)))
	}
	sort.SliceStable(events, func(i, j int) bool {
		ki, kj := sortKey(events[i]), sortKey(events[j])
		if ki != kj {
			return ki < kj
		}
		return strings.ToLower(deref(events[i].Title)) < strings.ToLower(deref(events[j].Title))
	})
	return api.GetCalendar200JSONResponse{Events: events}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) eventDetail(ctx context.Context, q *dbq.Queries, h, id uuid.UUID) (api.CalendarEventDetail, error) {
	r, err := q.GetCalendarEvent(ctx, dbq.GetCalendarEventParams{HouseholdID: h, ID: id})
	if err != nil {
		return api.CalendarEventDetail{}, err
	}
	zone, err := householdZone(ctx, q, h)
	if err != nil {
		return api.CalendarEventDetail{}, err
	}
	asg, err := loadAssignments(ctx, q, h, []uuid.UUID{r.SeriesID})
	if err != nil {
		return api.CalendarEventDetail{}, err
	}
	f := eventFields{id: r.ID, series: r.SeriesID, key: r.RecurrenceKey, allDay: r.AllDay, startsAt: r.StartsAt, endsAt: r.EndsAt,
		startDate: r.StartDate, endDate: r.EndDate, title: r.Title, location: r.Location, status: &r.Status,
		calID: r.SourceID, calName: r.CalendarName, calColor: r.CalendarColor}
	e := f.event(zone, asg.children(r.SeriesID, r.RecurrenceKey))
	return api.CalendarEventDetail{Id: e.Id, SeriesId: e.SeriesId, Title: e.Title, AllDay: e.AllDay, Start: e.Start, End: e.End,
		Status: api.CalendarEventDetailStatus(e.Status), Calendar: e.Calendar, Location: e.Location, Children: e.Children,
		Description: r.Description, Recurring: r.Recurring}, nil
}

func (s *Server) GetCalendarEvent(ctx context.Context, req api.GetCalendarEventRequestObject) (api.GetCalendarEventResponseObject, error) {
	e, err := s.eventDetail(ctx, s.q, mustActor(ctx).HouseholdID, req.EventId)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetCalendarEvent404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	return api.GetCalendarEvent200JSONResponse(e), nil
}

func (s *Server) AssignEventChildren(ctx context.Context, req api.AssignEventChildrenRequestObject) (api.AssignEventChildrenResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	h := a.HouseholdID
	ev, err := cmd.Q.GetCalendarEvent(ctx, dbq.GetCalendarEventParams{HouseholdID: h, ID: req.EventId})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AssignEventChildren404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	kids := req.Body.Children
	for _, c := range kids {
		if !a.MayActFor(c) {
			return api.AssignEventChildren403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(apiError("forbidden", "Not allowed."))}, nil
		}
	}
	if n, err := cmd.Q.CountLiveChildren(ctx, dbq.CountLiveChildrenParams{HouseholdID: h, Ids: kids}); err != nil {
		return nil, err
	} else if int(n) != len(kids) {
		return api.AssignEventChildren422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("no_such_child", "Choose children of this household."))}, nil
	}
	key := ev.RecurrenceKey
	if req.Body.Scope == api.Series {
		key = seriesKey
		if err := cmd.Q.DeleteOccurrenceAssignments(ctx, dbq.DeleteOccurrenceAssignmentsParams{HouseholdID: h, SeriesID: ev.SeriesID}); err != nil {
			return nil, err
		}
	}
	id, err := cmd.Q.UpsertAssignment(ctx, dbq.UpsertAssignmentParams{HouseholdID: h, SeriesID: ev.SeriesID, RecurrenceKey: key})
	if err != nil {
		return nil, err
	}
	if err := cmd.Q.ClearAssignmentChildren(ctx, id); err != nil {
		return nil, err
	}
	if len(kids) > 0 {
		if err := cmd.Q.AddAssignmentChildren(ctx, dbq.AddAssignmentChildrenParams{HouseholdID: h, AssignmentID: id, ChildIds: kids}); err != nil {
			return nil, err
		}
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Calendar}); err != nil {
		return nil, err
	}
	d, err := s.eventDetail(ctx, cmd.Q, h, req.EventId)
	if err != nil {
		return nil, err
	}
	return api.AssignEventChildren200JSONResponse{Id: d.Id, SeriesId: d.SeriesId, Title: d.Title, AllDay: d.AllDay, Start: d.Start, End: d.End,
		Status: api.CalendarEventStatus(d.Status), Calendar: d.Calendar, Location: d.Location, Children: d.Children}, nil
}

func toSource(r dbq.CalendarSource) api.CalendarSource {
	src := api.CalendarSource{Id: r.ID, Name: r.Name, Provider: api.CalendarSourceProvider(r.Provider), Color: api.ProfileColor(r.Color),
		ShowOnDisplay: r.ShowOnDisplay, Status: api.CalendarSourceStatusOk, LastSyncAt: r.LastSuccessAt, Version: int(r.Version)}
	if r.ErrorCode != nil {
		code := api.CalendarErrorCode(*r.ErrorCode)
		src.Error = &code
		if r.ConsecutiveFailures > 0 { // too_many_events alone still syncs
			src.Status = api.CalendarSourceStatusError
		}
	}
	return src
}

func (s *Server) ListCalendars(ctx context.Context, _ api.ListCalendarsRequestObject) (api.ListCalendarsResponseObject, error) {
	rows, err := s.q.ListCalendarSources(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := make([]api.CalendarSource, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSource(r))
	}
	return api.ListCalendars200JSONResponse{Calendars: out}, nil
}

func (s *Server) AddCalendarLink(ctx context.Context, req api.AddCalendarLinkRequestObject) (api.AddCalendarLinkResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	unprocessable := func(code, msg string) api.AddCalendarLinkResponseObject {
		return api.AddCalendarLink422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError(code, msg))}
	}
	kr := s.cfg.SecretKeys
	if kr == nil {
		return unprocessable("not_configured", "This server can't store calendar links yet: OPENCOZY_SECRET_KEYS is not set."), nil
	}
	feedURL, err := calendar.NormalizeURL(req.Body.Url)
	if err != nil {
		return unprocessable("invalid_link", "Paste a calendar link that starts with https://, http:// or webcal://."), nil
	}
	if n, err := cmd.Q.CountCalendarSources(ctx, h); err != nil {
		return nil, err
	} else if n >= maxCalendars {
		return unprocessable("too_many_calendars", "This household follows 50 calendars. Remove one to add another."), nil
	}
	macs, err := kr.FeedMACs(h.String(), []byte(feedURL))
	if err != nil {
		return nil, err
	}
	if taken, err := cmd.Q.CalendarFeedTaken(ctx, dbq.CalendarFeedTakenParams{HouseholdID: h, Macs: macs}); err != nil {
		return nil, err
	} else if taken {
		return api.AddCalendarLink409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("calendar_exists", "This household already follows that calendar."))}, nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	sealed, mac, err := calendar.SealFeedURL(kr, h, id, feedURL)
	if err != nil {
		return nil, err
	}
	r, err := cmd.Q.CreateCalendarSource(ctx, dbq.CreateCalendarSourceParams{ID: id, HouseholdID: h, Name: strings.TrimSpace(req.Body.Name),
		Color: string(req.Body.Color), ShowOnDisplay: req.Body.ShowOnDisplay, FeedUrl: &sealed, FeedUrlMac: &mac})
	if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Calendar}); err != nil {
		return nil, err
	}
	s.wakeCalendar()
	return api.AddCalendarLink201JSONResponse(toSource(r)), nil
}

func (s *Server) UpdateCalendar(ctx context.Context, req api.UpdateCalendarRequestObject) (api.UpdateCalendarResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	b := req.Body
	r, err := cmd.Q.UpdateCalendarSource(ctx, dbq.UpdateCalendarSourceParams{HouseholdID: h, ID: req.CalendarId, Name: strings.TrimSpace(b.Name),
		Color: string(b.Color), ShowOnDisplay: b.ShowOnDisplay, ExpectedVersion: int32(b.ExpectedVersion)}) //nolint:gosec // the spec caps it at int32
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := cmd.Q.GetCalendarSource(ctx, dbq.GetCalendarSourceParams{HouseholdID: h, ID: req.CalendarId}); errors.Is(err, pgx.ErrNoRows) {
			return api.UpdateCalendar404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		} else if err != nil {
			return nil, err
		}
		return api.UpdateCalendar409JSONResponse{ConflictJSONResponse: conflict("calendar")}, nil
	} else if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Calendar}); err != nil {
		return nil, err
	}
	return api.UpdateCalendar200JSONResponse(toSource(r)), nil
}

func (s *Server) RemoveCalendar(ctx context.Context, req api.RemoveCalendarRequestObject) (api.RemoveCalendarResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	n, err := cmd.Q.DeleteCalendarSource(ctx, dbq.DeleteCalendarSourceParams{HouseholdID: h, ID: req.CalendarId, ExpectedVersion: int32(req.Params.ExpectedVersion)}) //nolint:gosec // the spec caps it at int32
	if err != nil {
		return nil, err
	}
	if n == 0 {
		if _, err := cmd.Q.GetCalendarSource(ctx, dbq.GetCalendarSourceParams{HouseholdID: h, ID: req.CalendarId}); errors.Is(err, pgx.ErrNoRows) {
			return api.RemoveCalendar404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		} else if err != nil {
			return nil, err
		}
		return api.RemoveCalendar409JSONResponse{ConflictJSONResponse: conflict("calendar")}, nil
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Calendar}); err != nil {
		return nil, err
	}
	return api.RemoveCalendar204Response{}, nil
}
