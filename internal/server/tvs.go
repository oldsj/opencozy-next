package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/jobs"
	"github.com/oldsj/opencozy-next/internal/tv"
)

// The TV gate (slice 9). The rules live in internal/tv; these handlers load a snapshot, run one
// command on it and map *tv.Error to the operation's typed responses.

// tvFail maps a domain error to the response the operation declares for its status; anything
// else is returned as an unexpected error.
func tvFail[R any](err error, cases map[int]func(api.Error) R) (R, error) {
	var e *tv.Error
	if errors.As(err, &e) {
		if f, ok := cases[e.Status]; ok {
			return f(apiError(e.Code, e.Message)), nil
		}
	}
	var zero R
	return zero, err
}

var errOpenHours = apiError("invalid_open_hours", "Open hours start and end at different times.")

// tvWho is who a command acts as, for history.
func tvWho(a actor.Actor) tv.Who {
	var w tv.Who
	switch a.Kind {
	case actor.Adult, actor.Phone:
		w.Adult = &a.AdultID
	case actor.Assistant:
		w.Adult, w.Credential = &a.AdultID, &a.CredentialID
	case actor.Device, actor.DeviceWeb:
		w.Device = &a.CredentialID
	}
	return w
}

// readNow is a read's time: the commands' clock when one is set (tests), else the database's.
func (s *Server) readNow(ctx context.Context) (time.Time, error) {
	if s.runner.Clock != nil {
		return s.runner.Clock.Now(ctx, s.q)
	}
	return s.q.Now(ctx)
}

// tvRead loads the household's TVs for a read.
func (s *Server) tvRead(ctx context.Context) (*tv.Snapshot, error) {
	now, err := s.readNow(ctx)
	if err != nil {
		return nil, err
	}
	return s.tv.Load(ctx, s.q, mustActor(ctx).HouseholdID, now)
}

// tvBegin loads and settles the household's TVs for a command, at the command's time.
func (s *Server) tvBegin(ctx context.Context) (*tv.Snapshot, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	return s.tv.Begin(ctx, cmd.Q, mustActor(ctx).HouseholdID, now)
}

// TVJob is the TV settlement job (slice 9).
func (s *Server) TVJob() jobs.Job { return s.tv.Job(s.runner.Clock) }

// ListTvs is every TV with its state, and each child's TV time.
func (s *Server) ListTvs(ctx context.Context, _ api.ListTvsRequestObject) (api.ListTvsResponseObject, error) {
	snap, err := s.tvRead(ctx)
	if err != nil {
		return nil, err
	}
	out, err := snap.List(ctx)
	if err != nil {
		return nil, err
	}
	return api.ListTvs200JSONResponse(out), nil
}

// DisplayTvs is the TVs as a wall display shows them.
func (s *Server) DisplayTvs(ctx context.Context, _ api.DisplayTvsRequestObject) (api.DisplayTvsResponseObject, error) {
	snap, err := s.tvRead(ctx)
	if err != nil {
		return nil, err
	}
	return api.DisplayTvs200JSONResponse(snap.Display()), nil
}

// TvHistory is recent sessions and openings.
func (s *Server) TvHistory(ctx context.Context, _ api.TvHistoryRequestObject) (api.TvHistoryResponseObject, error) {
	snap, err := s.tvRead(ctx)
	if err != nil {
		return nil, err
	}
	out, err := snap.History(ctx)
	if err != nil {
		return nil, err
	}
	return api.TvHistory200JSONResponse(out), nil
}

// ChildTv is a child's TV time, as the kid view shows it.
func (s *Server) ChildTv(ctx context.Context, req api.ChildTvRequestObject) (api.ChildTvResponseObject, error) {
	snap, err := s.tvRead(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.IsChild(req.ChildId) {
		return api.ChildTv404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	out, err := snap.Child(ctx, req.ChildId)
	if err != nil {
		return nil, err
	}
	return api.ChildTv200JSONResponse(out), nil
}

// TvAllowances is the router's list of MACs that may be online.
func (s *Server) TvAllowances(ctx context.Context, _ api.TvAllowancesRequestObject) (api.TvAllowancesResponseObject, error) {
	snap, err := s.tvRead(ctx)
	if err != nil {
		return nil, err
	}
	return api.TvAllowances200JSONResponse(snap.Allowances()), nil
}

func tvInput(name string, room *string, macs []string, entity *string, hours *api.OpenHours) (tv.TvInput, error) {
	h, err := tv.ParseOpenHours(hours)
	if err != nil {
		return tv.TvInput{}, err
	}
	in := tv.TvInput{Name: strings.TrimSpace(name), MACs: append([]string(nil), macs...), Entity: entity, Hours: h}
	if room != nil {
		r := strings.TrimSpace(*room)
		in.Room = &r
	}
	return in, nil
}

// CreateTv adds a TV.
func (s *Server) CreateTv(ctx context.Context, req api.CreateTvRequestObject) (api.CreateTvResponseObject, error) {
	b := req.Body
	in, err := tvInput(b.Name, b.Room, b.Macs, b.HaEntityId, b.OpenHours)
	if err != nil {
		return api.CreateTv422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errOpenHours)}, nil
	}
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	t, err := snap.Create(ctx, in)
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.CreateTvResponseObject{
			409: func(e api.Error) api.CreateTvResponseObject { return api.CreateTv409JSONResponse(e) },
		})
	}
	return api.CreateTv201JSONResponse(snap.TvView(t)), snap.Notify(ctx)
}

// UpdateTv edits a TV.
func (s *Server) UpdateTv(ctx context.Context, req api.UpdateTvRequestObject) (api.UpdateTvResponseObject, error) {
	b := req.Body
	in, err := tvInput(b.Name, b.Room, b.Macs, b.HaEntityId, b.OpenHours)
	if err != nil {
		return api.UpdateTv422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errOpenHours)}, nil
	}
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	t, err := snap.Update(ctx, req.TvId, int32(b.ExpectedVersion), in) //nolint:gosec // a version is small
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.UpdateTvResponseObject{
			404: func(e api.Error) api.UpdateTvResponseObject {
				return api.UpdateTv404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
			409: func(e api.Error) api.UpdateTvResponseObject { return api.UpdateTv409JSONResponse(e) },
		})
	}
	return api.UpdateTv200JSONResponse(snap.TvView(t)), snap.Notify(ctx)
}

// RemoveTv removes a TV.
func (s *Server) RemoveTv(ctx context.Context, req api.RemoveTvRequestObject) (api.RemoveTvResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	if err := snap.Remove(ctx, req.TvId, tvWho(mustActor(ctx))); err != nil {
		return tvFail(err, map[int]func(api.Error) api.RemoveTvResponseObject{
			404: func(e api.Error) api.RemoveTvResponseObject {
				return api.RemoveTv404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
		})
	}
	return api.RemoveTv204Response{}, snap.Notify(ctx)
}

// StartTvSession turns a TV on for the parents or a child.
func (s *Server) StartTvSession(ctx context.Context, req api.StartTvSessionRequestObject) (api.StartTvSessionResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := snap.Start(ctx, req.TvId, string(req.Body.Watcher), req.Body.ChildId, tvWho(mustActor(ctx)))
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.StartTvSessionResponseObject{
			404: func(e api.Error) api.StartTvSessionResponseObject {
				return api.StartTvSession404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
			409: func(e api.Error) api.StartTvSessionResponseObject {
				return api.StartTvSession409JSONResponse{StartConflictJSONResponse: api.StartConflictJSONResponse(e)}
			},
			422: func(e api.Error) api.StartTvSessionResponseObject {
				return api.StartTvSession422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(e)}
			},
		})
	}
	return api.StartTvSession200JSONResponse(snap.SessionView(sess)), snap.Notify(ctx)
}

func endedView(snap *tv.Snapshot, sess *dbq.TvSession) api.EndedTvSession {
	if sess == nil {
		return api.EndedTvSession{}
	}
	v := snap.SessionView(*sess)
	return api.EndedTvSession{Session: &v}
}

// EndTvSession turns a TV off now.
func (s *Server) EndTvSession(ctx context.Context, req api.EndTvSessionRequestObject) (api.EndTvSessionResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := snap.End(ctx, req.TvId, tv.EndParentEnded, tvWho(mustActor(ctx)))
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.EndTvSessionResponseObject{
			404: func(e api.Error) api.EndTvSessionResponseObject {
				return api.EndTvSession404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
		})
	}
	return api.EndTvSession200JSONResponse(endedView(snap, sess)), snap.Notify(ctx)
}

// AnswerTvPrompt answers "Who's watching?".
func (s *Server) AnswerTvPrompt(ctx context.Context, req api.AnswerTvPromptRequestObject) (api.AnswerTvPromptResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	p, sess, err := snap.Answer(ctx, req.PromptId, int32(b.ExpectedVersion), string(b.Answer), b.ChildId, tvWho(mustActor(ctx))) //nolint:gosec // a version is small
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.AnswerTvPromptResponseObject{
			404: func(e api.Error) api.AnswerTvPromptResponseObject {
				return api.AnswerTvPrompt404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
			409: func(e api.Error) api.AnswerTvPromptResponseObject { return api.AnswerTvPrompt409JSONResponse(e) },
			422: func(e api.Error) api.AnswerTvPromptResponseObject {
				return api.AnswerTvPrompt422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(e)}
			},
		})
	}
	out := api.TvPromptAnswered{Session: endedView(snap, sess).Session}
	out.Prompt.Id, out.Prompt.Version, out.Prompt.State = p.ID, int(p.Version), api.TvPromptAnsweredPromptState(p.State)
	return api.AnswerTvPrompt200JSONResponse(out), snap.Notify(ctx)
}

// chosenTvs is nil for every TV, or the ones listed. Both or neither is a 422.
func chosenTvs(all *bool, ids *[]uuid.UUID) ([]uuid.UUID, bool) {
	everyTv := all != nil && *all
	if everyTv == (ids != nil) {
		return nil, false
	}
	if everyTv {
		return nil, true
	}
	return *ids, true
}

var errChosenTvs = apiError("choose_tvs", "Choose every TV or list some, not both.")

// OpenTvs opens TVs for 1 hour, 2 hours or all day.
func (s *Server) OpenTvs(ctx context.Context, req api.OpenTvsRequestObject) (api.OpenTvsResponseObject, error) {
	ids, ok := chosenTvs(req.Body.All, req.Body.TvIds)
	if !ok {
		return api.OpenTvs422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errChosenTvs)}, nil
	}
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	done, ended, err := snap.OpenTVs(ctx, ids, string(req.Body.Duration), tvWho(mustActor(ctx)))
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.OpenTvsResponseObject{
			404: func(e api.Error) api.OpenTvsResponseObject {
				return api.OpenTvs404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
			409: func(e api.Error) api.OpenTvsResponseObject { return api.OpenTvs409JSONResponse(e) },
		})
	}
	return api.OpenTvs200JSONResponse{OpenedJSONResponse: api.OpenedJSONResponse(snap.OpenedView(done, ended))}, snap.Notify(ctx)
}

// ExtendTvs keeps open TVs open an hour longer.
func (s *Server) ExtendTvs(ctx context.Context, req api.ExtendTvsRequestObject) (api.ExtendTvsResponseObject, error) {
	ids, ok := chosenTvs(req.Body.All, req.Body.TvIds)
	if !ok {
		return api.ExtendTvs422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errChosenTvs)}, nil
	}
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	done, err := snap.Extend(ctx, ids, tvWho(mustActor(ctx)))
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.ExtendTvsResponseObject{
			404: func(e api.Error) api.ExtendTvsResponseObject {
				return api.ExtendTvs404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
			409: func(e api.Error) api.ExtendTvsResponseObject { return api.ExtendTvs409JSONResponse(e) },
		})
	}
	return api.ExtendTvs200JSONResponse{OpenedJSONResponse: api.OpenedJSONResponse(snap.OpenedView(done, nil))}, snap.Notify(ctx)
}

// CloseTvs closes TVs a parent opened.
func (s *Server) CloseTvs(ctx context.Context, req api.CloseTvsRequestObject) (api.CloseTvsResponseObject, error) {
	ids, ok := chosenTvs(req.Body.All, req.Body.TvIds)
	if !ok {
		return api.CloseTvs422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errChosenTvs)}, nil
	}
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	done, err := snap.Close(ctx, ids, tvWho(mustActor(ctx)))
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.CloseTvsResponseObject{
			404: func(e api.Error) api.CloseTvsResponseObject {
				return api.CloseTvs404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
		})
	}
	return api.CloseTvs200JSONResponse{OpenedJSONResponse: api.OpenedJSONResponse(snap.OpenedView(done, nil))}, snap.Notify(ctx)
}

// AskForTv asks the parents to turn a TV on.
func (s *Server) AskForTv(ctx context.Context, req api.AskForTvRequestObject) (api.AskForTvResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	source := tv.SourceDisplay
	if mustActor(ctx).Kind != actor.Display {
		source = tv.SourceAdult
	}
	out, err := snap.Ask(ctx, req.TvId, source)
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.AskForTvResponseObject{
			404: func(e api.Error) api.AskForTvResponseObject {
				return api.AskForTv404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
		})
	}
	return api.AskForTv200JSONResponse(out), snap.Notify(ctx)
}

// DoneWatching ends a TV's running session; a child's device ends only that child's own.
func (s *Server) DoneWatching(ctx context.Context, req api.DoneWatchingRequestObject) (api.DoneWatchingResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	a := mustActor(ctx)
	var only *uuid.UUID
	if a.Kind == actor.Device || a.Kind == actor.DeviceWeb {
		only = &a.Children[0]
	}
	sess, err := snap.Done(ctx, req.TvId, only, tvWho(a))
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.DoneWatchingResponseObject{
			403: func(e api.Error) api.DoneWatchingResponseObject { return api.DoneWatching403JSONResponse(e) },
			404: func(e api.Error) api.DoneWatchingResponseObject {
				return api.DoneWatching404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
		})
	}
	return api.DoneWatching200JSONResponse(endedView(snap, sess)), snap.Notify(ctx)
}

// WatchTv starts a child's own session from their own device.
func (s *Server) WatchTv(ctx context.Context, req api.WatchTvRequestObject) (api.WatchTvResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := snap.Watch(ctx, req.Body.TvId, req.ChildId, mustActor(ctx).CredentialID)
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.WatchTvResponseObject{
			404: func(e api.Error) api.WatchTvResponseObject {
				return api.WatchTv404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(e)}
			},
			409: func(e api.Error) api.WatchTvResponseObject { return api.WatchTv409JSONResponse(e) },
		})
	}
	return api.WatchTv200JSONResponse(snap.SessionView(sess)), snap.Notify(ctx)
}

// ReportTvAttempts is the router reporting blocked TVs.
func (s *Server) ReportTvAttempts(ctx context.Context, req api.ReportTvAttemptsRequestObject) (api.ReportTvAttemptsResponseObject, error) {
	snap, err := s.tvBegin(ctx)
	if err != nil {
		return nil, err
	}
	attempts := make([]tv.Attempt, len(req.Body.Attempts))
	for i, a := range req.Body.Attempts {
		attempts[i] = tv.Attempt{MAC: a.Mac, LastSeen: a.LastSeenAt}
	}
	out, err := snap.ReportAttempts(ctx, attempts)
	if err != nil {
		return nil, err
	}
	return api.ReportTvAttempts200JSONResponse(out), snap.Notify(ctx)
}

// ReportTvPower is Home Assistant reporting a TV's power.
func (s *Server) ReportTvPower(ctx context.Context, req api.ReportTvPowerRequestObject) (api.ReportTvPowerResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	var changed time.Time
	if req.Body.LastChanged != nil {
		changed = *req.Body.LastChanged
	}
	out, err := s.tv.ReportPower(ctx, cmd.Q, mustActor(ctx).HouseholdID, now, req.Body.EntityId, req.Body.State, changed)
	if err != nil {
		return tvFail(err, map[int]func(api.Error) api.ReportTvPowerResponseObject{
			404: func(e api.Error) api.ReportTvPowerResponseObject { return api.ReportTvPower404JSONResponse(e) },
		})
	}
	return api.ReportTvPower200JSONResponse(out), nil
}
