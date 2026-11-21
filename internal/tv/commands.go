package tv

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Commands run on a Snapshot from Begin, under the household lock, with the command's time.
// They return *Error for anything the caller should answer with (404, 409, 403, 422); the
// command runner rolls those back, so a refused command writes nothing.

// Begin loads the household's TVs at now and settles what is over, for a write.
func (d Domain) Begin(ctx context.Context, q *dbq.Queries, householdID uuid.UUID, now time.Time) (*Snapshot, error) {
	s, err := d.Load(ctx, q, householdID, now)
	if err != nil {
		return nil, err
	}
	return s, s.Settle(ctx, nil)
}

// Errors the commands answer with.
var (
	ErrTvRemoved      = conflict("tv_removed", "That TV was removed.")
	ErrNoTVs          = conflict("no_tvs", "There are no TVs to open.")
	ErrUnknownEntity  = &Error{Status: 404, Code: "unknown_entity", Message: "No TV has that Home Assistant entity."}
	ErrNotYourSession = &Error{Status: 403, Code: "not_your_session", Message: "Only the child watching can say they are done."}
	ErrAnswer         = &Error{Status: 422, Code: "invalid_answer", Message: "Choose a child for a child's session, and none otherwise."}
	errVersion        = conflict("version_conflict", "Someone else changed this TV. Reload and try again.")
	errTooManyTVs     = conflict("too_many_tvs", "A household can have at most %d TVs.", MaxTVs)
)

func (s *Snapshot) mustTv(id uuid.UUID) (*dbq.Tv, error) {
	if t := s.tv(id); t != nil {
		return t, nil
	}
	return nil, ErrNotFound
}

// start begins a session on t, replacing a running one, and answers its prompt.
func (s *Snapshot) start(ctx context.Context, t *dbq.Tv, watcher string, child *uuid.UUID, who Who) (dbq.TvSession, *dbq.TvPrompt, error) {
	if st := s.State(*t); st.Open {
		return dbq.TvSession{}, nil, conflict("tv_open", "%s is open until %s, so nobody needs to turn it on.", t.Name, st.ClosesAt.In(s.Loc).Format("15:04"))
	}
	if (watcher == WatcherChild) != (child != nil) {
		return dbq.TvSession{}, nil, ErrAnswer
	}
	current := s.openSession(t.ID)
	var endsBy *time.Time
	if watcher == WatcherChild {
		c := s.child(*child)
		if c == nil {
			return dbq.TvSession{}, nil, conflict("child_unavailable", "That child is unavailable.")
		}
		if current != nil && current.Watcher == WatcherChild && *current.ChildID == *child {
			return *current, nil, nil
		}
		if other := s.childSession(*child); other != nil {
			return dbq.TvSession{}, nil, conflict("already_watching", "%s is already watching %s.", c.Name, s.tvName(other.TvID))
		}
		if s.unspent(*child) < time.Second {
			return dbq.TvSession{}, nil, conflict("no_tv_time", "%s has no TV time left.", c.Name)
		}
		h, err := s.HoursFor(ctx, *child)
		if err != nil {
			return dbq.TvSession{}, nil, err
		}
		if h.Rule {
			if !h.Open {
				return dbq.TvSession{}, nil, conflict("outside_hours", "%s can watch from %s.", c.Name, h.OpensAt.In(s.Loc).Format("15:04"))
			}
			endsBy = &h.ClosesAt
		}
	}
	if current != nil {
		if _, err := s.endSession(ctx, *current, s.Now, EndParentEnded, who.Adult); err != nil {
			return dbq.TvSession{}, nil, err
		}
	}
	sess, err := s.q.StartTvSession(ctx, dbq.StartTvSessionParams{HouseholdID: s.H, TvID: t.ID, Watcher: watcher, ChildID: child, EndsBy: endsBy,
		StartedByAdult: who.Adult, StartedByDevice: who.Device, Now: s.Now})
	if err != nil {
		return dbq.TvSession{}, nil, err
	}
	s.Open = append(s.Open, sess)
	s.changed = true
	p, err := s.resolve(ctx, t.ID, "answered", who, &sess.ID)
	return sess, p, err
}

// Start turns a TV on for the parents or a child (startTvSession).
func (s *Snapshot) Start(ctx context.Context, tvID uuid.UUID, watcher string, child *uuid.UUID, who Who) (dbq.TvSession, error) {
	t, err := s.mustTv(tvID)
	if err != nil {
		return dbq.TvSession{}, err
	}
	sess, _, err := s.start(ctx, t, watcher, child, who)
	return sess, err
}

// End ends a TV's running session now with reason; nil when there was none.
func (s *Snapshot) End(ctx context.Context, tvID uuid.UUID, reason string, who Who) (*dbq.TvSession, error) {
	if _, err := s.mustTv(tvID); err != nil {
		return nil, err
	}
	sess := s.openSession(tvID)
	if sess == nil {
		return nil, nil
	}
	ended, err := s.endSession(ctx, *sess, s.Now, reason, who.Adult)
	return &ended, err
}

// Done is "Done watching": it ends a TV's running session. A child's device (onlyChild) may end
// only that child's own.
func (s *Snapshot) Done(ctx context.Context, tvID uuid.UUID, onlyChild *uuid.UUID, who Who) (*dbq.TvSession, error) {
	if _, err := s.mustTv(tvID); err != nil {
		return nil, err
	}
	sess := s.openSession(tvID)
	if sess == nil {
		return nil, nil
	}
	if onlyChild != nil && (sess.Watcher != WatcherChild || *sess.ChildID != *onlyChild) {
		return nil, ErrNotYourSession
	}
	ended, err := s.endSession(ctx, *sess, s.Now, EndDone, who.Adult)
	return &ended, err
}

// Answers to "Who's watching?".
const (
	AnswerParents = "parents"
	AnswerChild   = "child"
	AnswerNotNow  = "not_now"
)

// Answer decides a prompt: parents or a child (a session starts), or not now. The first answer
// wins.
func (s *Snapshot) Answer(ctx context.Context, promptID uuid.UUID, version int32, answer string, child *uuid.UUID, who Who) (dbq.TvPrompt, *dbq.TvSession, error) {
	if (answer == AnswerChild) != (child != nil) {
		return dbq.TvPrompt{}, nil, ErrAnswer
	}
	p, err := s.q.GetTvPrompt(ctx, dbq.GetTvPromptParams{HouseholdID: s.H, ID: promptID})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.TvPrompt{}, nil, ErrNotFound
	} else if err != nil {
		return dbq.TvPrompt{}, nil, err
	}
	t := s.tv(p.TvID)
	switch {
	case t == nil:
		return dbq.TvPrompt{}, nil, ErrTvRemoved
	case p.State == "withdrawn":
		return dbq.TvPrompt{}, nil, conflict("prompt_withdrawn", "%s was turned off or opened.", t.Name)
	case p.State != "pending":
		return dbq.TvPrompt{}, nil, conflict("already_answered", "Someone already answered.")
	case p.Version != version:
		return dbq.TvPrompt{}, nil, conflict("version_conflict", "That question changed. Reload and try again.")
	}
	if answer == AnswerNotNow {
		done, err := s.resolve(ctx, t.ID, "dismissed", who, nil)
		if err != nil {
			return dbq.TvPrompt{}, nil, err
		}
		return *done, nil, nil
	}
	sess, done, err := s.start(ctx, t, answer, child, who)
	if err != nil {
		return dbq.TvPrompt{}, nil, err
	}
	if done == nil { // the child was already watching this TV: answer it anyway
		if done, err = s.resolve(ctx, t.ID, "answered", who, &sess.ID); err != nil {
			return dbq.TvPrompt{}, nil, err
		}
	}
	return *done, &sess, nil
}

// Watch starts a child's own session from their own device, on a TV asking "Who's watching?"
// with nobody on it.
func (s *Snapshot) Watch(ctx context.Context, tvID, child uuid.UUID, device uuid.UUID) (dbq.TvSession, error) {
	t, err := s.mustTv(tvID)
	if err != nil {
		return dbq.TvSession{}, err
	}
	if s.liveSession(t.ID) != nil {
		return dbq.TvSession{}, conflict("tv_on", "Someone already answered: %s is on.", t.Name)
	}
	if s.asking(*t) == nil {
		return dbq.TvSession{}, conflict("not_asking", "%s isn't asking who's watching. Turn it on first.", t.Name)
	}
	sess, _, err := s.start(ctx, t, WatcherChild, &child, Who{Device: &device})
	return sess, err
}

// Prompt sources.
const (
	SourceHomeAssistant = "home_assistant"
	SourceRouter        = "router"
	SourceDisplay       = "display"
	SourceAdult         = "adult"
)

func (s *Snapshot) raise(ctx context.Context, t dbq.Tv, source string) error {
	p, err := s.q.RaiseTvPrompt(ctx, dbq.RaiseTvPromptParams{HouseholdID: s.H, TvID: t.ID, Source: source, Now: s.Now})
	if err != nil {
		return err
	}
	s.Prompts = append(s.Prompts, p)
	s.changed = true
	return nil
}

// Ask raises "Who's watching?" for a TV (a display or an adult asking). A TV on or open answers
// on; asking again while it asks keeps the one prompt and, at most every remind, refreshes it.
func (s *Snapshot) Ask(ctx context.Context, tvID uuid.UUID, source string) (api.TvAsked, error) {
	t, err := s.mustTv(tvID)
	if err != nil {
		return api.TvAsked{}, err
	}
	if s.liveSession(t.ID) != nil || s.State(*t).Open {
		return api.TvAsked{On: true}, nil
	}
	if p := s.pending(t.ID); p != nil {
		if s.Now.Sub(p.CreatedAt) >= remind {
			if err := s.q.RemindTvPrompt(ctx, dbq.RemindTvPromptParams{HouseholdID: s.H, ID: p.ID, Now: s.Now}); err != nil {
				return api.TvAsked{}, err
			}
			p.CreatedAt = s.Now
			s.changed = true
		}
		return api.TvAsked{Asking: true}, nil
	}
	return api.TvAsked{Asking: true}, s.raise(ctx, *t, source)
}

// targets is the TVs a command names, by name: every live one (nil ids), or the ones listed,
// each of which must exist.
func (s *Snapshot) targets(ids []uuid.UUID) ([]*dbq.Tv, error) {
	var out []*dbq.Tv
	for i := range s.TVs {
		if ids == nil || slices.Contains(ids, s.TVs[i].ID) {
			out = append(out, &s.TVs[i])
		}
	}
	if ids != nil && len(out) != len(ids) {
		return nil, ErrNotFound
	}
	return out, nil
}

func (s *Snapshot) recordOpen(ctx context.Context, t *dbq.Tv, action string, until *time.Time, who Who, ended *uuid.UUID) error {
	if err := s.q.SetTvOpenUntil(ctx, dbq.SetTvOpenUntilParams{HouseholdID: s.H, ID: t.ID, OpenUntil: until}); err != nil {
		return err
	}
	t.OpenUntil = until
	s.changed = true
	return s.q.RecordTvOpenEvent(ctx, dbq.RecordTvOpenEventParams{HouseholdID: s.H, TvID: t.ID, Action: action, Until: until, At: s.Now,
		ByAdult: who.Adult, ViaCredential: who.Credential, EndedSessionID: ended})
}

// OpenTVs opens TVs (nil ids: every TV) for duration (Open1h, Open2h, OpenAllDay): each one's
// session ends now (parent_override) and its prompt is withdrawn. It returns the TVs and the
// ended sessions.
func (s *Snapshot) OpenTVs(ctx context.Context, ids []uuid.UUID, duration string, who Who) ([]uuid.UUID, []dbq.TvSession, error) {
	tvs, err := s.targets(ids)
	if err != nil {
		return nil, nil, err
	}
	if len(tvs) == 0 {
		return nil, nil, ErrNoTVs
	}
	until := OverrideEnd(duration, s.Now, s.Loc)
	var done []uuid.UUID
	var ended []dbq.TvSession
	for _, t := range tvs {
		var endedID *uuid.UUID
		if sess := s.openSession(t.ID); sess != nil {
			e, err := s.endSession(ctx, *sess, s.Now, EndParentOverride, who.Adult)
			if err != nil {
				return nil, nil, err
			}
			ended, endedID = append(ended, e), &e.ID
		}
		if _, err := s.resolve(ctx, t.ID, "withdrawn", Who{}, nil); err != nil {
			return nil, nil, err
		}
		if err := s.recordOpen(ctx, t, "open", &until, who, endedID); err != nil {
			return nil, nil, err
		}
		done = append(done, t.ID)
	}
	return done, ended, nil
}

// Extend keeps TVs a parent opened (nil ids: every one) open ExtendBy longer, at most
// MaxOverride ahead.
func (s *Snapshot) Extend(ctx context.Context, ids []uuid.UUID, who Who) ([]uuid.UUID, error) {
	tvs, err := s.targets(ids)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		tvs = slices.DeleteFunc(tvs, func(t *dbq.Tv) bool { return t.OpenUntil == nil || !t.OpenUntil.After(s.Now) })
		if len(tvs) == 0 {
			return nil, conflict("not_open", "No TVs are open.")
		}
	}
	var done []uuid.UUID
	for _, t := range tvs {
		if t.OpenUntil == nil || !t.OpenUntil.After(s.Now) {
			return nil, conflict("not_open", "%s isn't open. Open it first.", t.Name)
		}
		until := t.OpenUntil.Add(ExtendBy)
		if until.Sub(s.Now) > MaxOverride {
			return nil, conflict("open_limit", "%s can be open at most 24 hours ahead.", t.Name)
		}
		if err := s.recordOpen(ctx, t, "extend", &until, who, nil); err != nil {
			return nil, err
		}
		done = append(done, t.ID)
	}
	return done, nil
}

// Close closes TVs a parent opened (nil ids: every one) now. A TV not opened is left as it is.
func (s *Snapshot) Close(ctx context.Context, ids []uuid.UUID, who Who) ([]uuid.UUID, error) {
	tvs, err := s.targets(ids)
	if err != nil {
		return nil, err
	}
	var done []uuid.UUID
	for _, t := range tvs {
		if t.OpenUntil != nil && t.OpenUntil.After(s.Now) {
			if err := s.recordOpen(ctx, t, "close", nil, who, nil); err != nil {
				return nil, err
			}
		}
		done = append(done, t.ID)
	}
	return done, nil
}

// OpenedView is the answer to open, extend and close.
func (s *Snapshot) OpenedView(ids []uuid.UUID, ended []dbq.TvSession) api.OpenedTvs {
	out := api.OpenedTvs{ServerTime: s.Now, Tvs: []api.OpenedTv{}, EndedSessions: make([]api.TvSession, 0, len(ended))}
	for _, t := range s.TVs {
		if slices.Contains(ids, t.ID) {
			st := s.State(t)
			out.Tvs = append(out.Tvs, api.OpenedTv{Id: t.ID, Name: t.Name, OpenUntil: tptr(st.OverrideUntil), OpenClosesAt: tptr(st.ClosesAt)})
		}
	}
	for _, e := range ended {
		out.EndedSessions = append(out.EndedSessions, s.SessionView(e))
	}
	return out
}

// ApplyPower records what Home Assistant says about a TV and acts on it: turning on with nobody
// watching asks "Who's watching?"; off ends its session (tv_off) and withdraws its prompt;
// unknown only forgets the last report. An open TV stays open either way.
func (s *Snapshot) ApplyPower(ctx context.Context, tvID uuid.UUID, power Power) (api.TvPowerResult, error) {
	t, err := s.mustTv(tvID)
	if err != nil {
		return api.TvPowerResult{}, err
	}
	was := Power(t.Power)
	if was != power {
		if err := s.q.SetTvPower(ctx, dbq.SetTvPowerParams{HouseholdID: s.H, ID: t.ID, Power: string(power), Now: s.Now}); err != nil {
			return api.TvPowerResult{}, err
		}
		now := s.Now
		t.Power, t.PowerChangedAt = string(power), &now
		if power == PowerOff {
			t.AttemptsSince, t.LastAttemptAt = nil, nil
		}
		s.changed = true
	}
	switch {
	case power == PowerOff:
		if sess := s.openSession(t.ID); sess != nil {
			if _, err := s.endSession(ctx, *sess, s.Now, EndTVOff, nil); err != nil {
				return api.TvPowerResult{}, err
			}
		}
		if _, err := s.resolve(ctx, t.ID, "withdrawn", Who{}, nil); err != nil {
			return api.TvPowerResult{}, err
		}
	case power == PowerOn && was != PowerOn && s.openSession(t.ID) == nil && !s.State(*t).Open && s.pending(t.ID) == nil:
		if err := s.raise(ctx, *t, SourceHomeAssistant); err != nil {
			return api.TvPowerResult{}, err
		}
	}
	return s.powerResult(*t), nil
}

func (s *Snapshot) powerResult(t dbq.Tv) api.TvPowerResult {
	return api.TvPowerResult{TvId: t.ID, Power: api.TvPower(t.Power), Online: s.State(t).Open || s.liveSession(t.ID) != nil, Asking: s.asking(t) != nil}
}

// ReportPower is Home Assistant's intake: a media_player's state (and last_changed, zero when not
// known) for the TV with that entity. The tv:report endpoint calls it, and so will slice 15's
// own Home Assistant poll. Call it under the household lock with the command's time. States
// that say nothing about the TV change nothing.
func (d Domain) ReportPower(ctx context.Context, q *dbq.Queries, householdID uuid.UUID, now time.Time, entityID, state string, lastChanged time.Time) (api.TvPowerResult, error) {
	s, err := d.Begin(ctx, q, householdID, now)
	if err != nil {
		return api.TvPowerResult{}, err
	}
	var t *dbq.Tv
	for i := range s.TVs {
		if s.TVs[i].HaEntityID != nil && *s.TVs[i].HaEntityID == entityID {
			t = &s.TVs[i]
		}
	}
	if t == nil {
		return api.TvPowerResult{}, ErrUnknownEntity
	}
	out := s.powerResult(*t)
	if power, ok := PowerFromHomeAssistant(state, lastChanged, now); ok {
		if out, err = s.ApplyPower(ctx, t.ID, power); err != nil {
			return api.TvPowerResult{}, err
		}
	}
	return out, s.Notify(ctx)
}

// ForgetPower is for when Home Assistant's report no longer describes TVs (slice 15: the
// connection was replaced, removed or failed verification): their power is unknown again and
// the router's attempt run starts over. Nil tvIDs is every TV. Sessions and prompts are left
// alone.
func (d Domain) ForgetPower(ctx context.Context, q *dbq.Queries, householdID uuid.UUID, now time.Time, tvIDs []uuid.UUID) error {
	tvs, err := q.ListLiveTvs(ctx, householdID)
	if err != nil {
		return err
	}
	var n int64
	for _, t := range tvs {
		if tvIDs != nil && !slices.Contains(tvIDs, t.ID) {
			continue
		}
		k, err := q.ForgetTvPower(ctx, dbq.ForgetTvPowerParams{HouseholdID: householdID, ID: t.ID, Now: now})
		if err != nil {
			return err
		}
		n += k
	}
	if n == 0 {
		return nil
	}
	s := &Snapshot{q: q, H: householdID, changed: true}
	return s.Notify(ctx)
}

// The router's attempts (ReportAttempts).
const (
	// AttemptRun is how long a TV without Home Assistant reports must keep trying before it asks.
	AttemptRun = 180 * time.Second
	// attemptGap is the longest pause between tries that still continues a run.
	attemptGap = 150 * time.Second
	// staleAttempt is how old a try may be and still ask.
	staleAttempt = 180 * time.Second
	// Quiet is how long a TV stays quiet after "Not now" or a session ending, unless Home
	// Assistant sees it turn on again.
	Quiet = 30 * time.Minute
)

// Attempt is a MAC the router blocked, and when it last tried.
type Attempt struct {
	MAC      string
	LastSeen time.Time
}

// Attempt results.
const (
	AttemptRequested  = "requested"
	AttemptPending    = "pending"
	AttemptOpen       = "open"
	AttemptSuppressed = "suppressed"
	AttemptUnknown    = "unknown"
)

// ReportAttempts decides, for each TV whose MAC the router blocked, whether that is a TV turning
// on worth asking about. A TV whose Home Assistant entity has reported asks only while it says
// on; one without asks once it has kept trying for AttemptRun.
func (s *Snapshot) ReportAttempts(ctx context.Context, attempts []Attempt) (api.TvAttemptResults, error) {
	macs := make([]string, 0, len(attempts))
	for i := range attempts {
		attempts[i].MAC = NormalizeMAC(attempts[i].MAC)
		macs = append(macs, attempts[i].MAC)
	}
	owners, err := s.q.TvMacOwners(ctx, dbq.TvMacOwnersParams{HouseholdID: s.H, Macs: macs})
	if err != nil {
		return api.TvAttemptResults{}, err
	}
	tvOf := map[string]uuid.UUID{}
	seen := map[uuid.UUID]time.Time{}
	for _, o := range owners {
		tvOf[o.Mac] = o.TvID
	}
	for _, a := range attempts {
		if id, ok := tvOf[a.MAC]; ok && a.LastSeen.After(seen[id]) {
			seen[id] = a.LastSeen
		}
	}
	states := map[uuid.UUID]string{}
	for _, t := range s.TVs {
		at, ok := seen[t.ID]
		if !ok {
			continue
		}
		if at.After(s.Now) {
			at = s.Now
		}
		if states[t.ID], err = s.attempt(ctx, s.tv(t.ID), at); err != nil {
			return api.TvAttemptResults{}, err
		}
	}
	out := api.TvAttemptResults{ServerTime: s.Now, Results: make([]api.TvAttemptResult, 0, len(attempts))}
	for _, a := range attempts {
		state := AttemptUnknown
		if id, ok := tvOf[a.MAC]; ok {
			state = states[id]
		}
		out.Results = append(out.Results, api.TvAttemptResult{Mac: a.MAC, State: api.TvAttemptResultState(state)})
	}
	return out, nil
}

func (s *Snapshot) attempt(ctx context.Context, t *dbq.Tv, seen time.Time) (string, error) {
	since, last := seen, seen
	if t.LastAttemptAt != nil && t.AttemptsSince != nil && seen.Sub(*t.LastAttemptAt) <= attemptGap {
		since = *t.AttemptsSince
		if t.LastAttemptAt.After(seen) {
			last = *t.LastAttemptAt
		}
	}
	if err := s.q.SetTvAttempts(ctx, dbq.SetTvAttemptsParams{HouseholdID: s.H, ID: t.ID, AttemptsSince: &since, LastAttemptAt: &last}); err != nil {
		return "", err
	}
	t.AttemptsSince, t.LastAttemptAt = &since, &last
	switch {
	case s.liveSession(t.ID) != nil || s.State(*t).Open:
		return AttemptOpen, nil
	case s.pending(t.ID) != nil:
		return AttemptPending, nil
	case s.Now.Sub(seen) > staleAttempt:
		return AttemptSuppressed, nil
	}
	reported := t.HaEntityID != nil && t.Power != string(PowerUnknown)
	if reported && t.Power != string(PowerOn) {
		return AttemptSuppressed, nil
	}
	quiet, err := s.quietSince(ctx, t.ID)
	if err != nil {
		return "", err
	}
	turnedOn := t.Power == string(PowerOn) && t.PowerChangedAt != nil && t.PowerChangedAt.After(quiet)
	if s.Now.Sub(quiet) < Quiet && !turnedOn {
		return AttemptSuppressed, nil
	}
	if !reported && last.Sub(since) < AttemptRun {
		return AttemptSuppressed, nil
	}
	return AttemptRequested, s.raise(ctx, *t, SourceRouter)
}

// quietSince is the later of the TV's last "Not now" and its last session's end.
func (s *Snapshot) quietSince(ctx context.Context, tvID uuid.UUID) (time.Time, error) {
	dismissed, err := s.q.LastTvPromptDismissed(ctx, dbq.LastTvPromptDismissedParams{HouseholdID: s.H, TvID: tvID})
	if err != nil {
		return time.Time{}, err
	}
	ended, err := s.q.LastTvSessionEnd(ctx, dbq.LastTvSessionEndParams{HouseholdID: s.H, TvID: tvID})
	if err != nil {
		return time.Time{}, err
	}
	if ended.After(dismissed) {
		return ended, nil
	}
	return dismissed, nil
}

// TvInput is a TV's editable fields, validated by the spec; MACs normalized here.
type TvInput struct {
	Name   string
	Room   *string
	MACs   []string
	Entity *string
	Hours  *OpenHours
}

func hoursCols(h *OpenHours) (start, end, days pgtype.Int2) {
	if h == nil {
		return
	}
	start = pgtype.Int2{Int16: int16(h.Start), Valid: true} //nolint:gosec // 0-1439
	end = pgtype.Int2{Int16: int16(h.End), Valid: true}     //nolint:gosec // 0-1439
	if h.Days != 0 && h.Days != EveryDay {
		days = pgtype.Int2{Int16: int16(h.Days), Valid: true}
	}
	return
}

// checkTaken refuses MACs or an entity another live TV already has.
func (s *Snapshot) checkTaken(ctx context.Context, self uuid.UUID, in *TvInput) error {
	for i := range in.MACs {
		in.MACs[i] = NormalizeMAC(in.MACs[i])
	}
	slices.Sort(in.MACs)
	in.MACs = slices.Compact(in.MACs)
	owners, err := s.q.TvMacOwners(ctx, dbq.TvMacOwnersParams{HouseholdID: s.H, Macs: in.MACs})
	if err != nil {
		return err
	}
	for _, o := range owners {
		if o.TvID != self {
			return conflict("mac_taken", "MAC address %s is already listed for %s.", o.Mac, o.Name)
		}
	}
	if in.Entity != nil {
		for _, t := range s.TVs {
			if t.ID != self && t.HaEntityID != nil && *t.HaEntityID == *in.Entity {
				return conflict("entity_taken", "That Home Assistant entity is already used for %s.", t.Name)
			}
		}
	}
	return nil
}

func (s *Snapshot) setMACs(ctx context.Context, id uuid.UUID, macs []string) error {
	if err := s.q.DeleteTvMacs(ctx, dbq.DeleteTvMacsParams{HouseholdID: s.H, TvID: id}); err != nil {
		return err
	}
	for _, m := range macs {
		if err := s.q.AddTvMac(ctx, dbq.AddTvMacParams{HouseholdID: s.H, TvID: id, Mac: m}); err != nil {
			return err
		}
	}
	s.MACs[id] = macs
	return nil
}

// Create adds a TV.
func (s *Snapshot) Create(ctx context.Context, in TvInput) (dbq.Tv, error) {
	if len(s.TVs) >= MaxTVs {
		return dbq.Tv{}, errTooManyTVs
	}
	if err := s.checkTaken(ctx, uuid.Nil, &in); err != nil {
		return dbq.Tv{}, err
	}
	start, end, days := hoursCols(in.Hours)
	t, err := s.q.CreateTv(ctx, dbq.CreateTvParams{HouseholdID: s.H, Name: in.Name, Room: in.Room, HaEntityID: in.Entity, OpenStart: start, OpenEnd: end, OpenDays: days})
	if err != nil {
		return dbq.Tv{}, err
	}
	s.TVs = append(s.TVs, t)
	s.changed = true
	return t, s.setMACs(ctx, t.ID, in.MACs)
}

// Update replaces a TV's fields at version. A new or cleared entity forgets its power; open
// hours open now end its session at once.
func (s *Snapshot) Update(ctx context.Context, id uuid.UUID, version int32, in TvInput) (dbq.Tv, error) {
	old, err := s.mustTv(id)
	if err != nil {
		return dbq.Tv{}, err
	}
	if old.Version != version {
		return dbq.Tv{}, errVersion
	}
	if err := s.checkTaken(ctx, id, &in); err != nil {
		return dbq.Tv{}, err
	}
	start, end, days := hoursCols(in.Hours)
	t, err := s.q.UpdateTv(ctx, dbq.UpdateTvParams{HouseholdID: s.H, ID: id, Version: version, Name: in.Name, Room: in.Room, HaEntityID: in.Entity,
		OpenStart: start, OpenEnd: end, OpenDays: days})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Tv{}, errVersion
	} else if err != nil {
		return dbq.Tv{}, err
	}
	if !equalPtr(old.HaEntityID, in.Entity) {
		if _, err := s.q.ForgetTvPower(ctx, dbq.ForgetTvPowerParams{HouseholdID: s.H, ID: id, Now: s.Now}); err != nil {
			return dbq.Tv{}, err
		}
		if t, err = s.q.GetLiveTv(ctx, dbq.GetLiveTvParams{HouseholdID: s.H, ID: id}); err != nil {
			return dbq.Tv{}, err
		}
	}
	*old = t
	s.changed = true
	if err := s.setMACs(ctx, id, in.MACs); err != nil {
		return dbq.Tv{}, err
	}
	return t, s.Settle(ctx, &id)
}

func equalPtr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Remove archives a TV: its session ends, its prompt is withdrawn and its MACs are forgotten.
func (s *Snapshot) Remove(ctx context.Context, id uuid.UUID, who Who) error {
	if _, err := s.mustTv(id); err != nil {
		return err
	}
	if sess := s.openSession(id); sess != nil {
		if _, err := s.endSession(ctx, *sess, s.Now, EndParentEnded, who.Adult); err != nil {
			return err
		}
	}
	if _, err := s.resolve(ctx, id, "withdrawn", Who{}, nil); err != nil {
		return err
	}
	if err := s.q.DeleteTvMacs(ctx, dbq.DeleteTvMacsParams{HouseholdID: s.H, TvID: id}); err != nil {
		return err
	}
	if err := s.q.ArchiveTv(ctx, dbq.ArchiveTvParams{HouseholdID: s.H, ID: id, Now: s.Now}); err != nil {
		return err
	}
	s.TVs = slices.DeleteFunc(s.TVs, func(t dbq.Tv) bool { return t.ID == id })
	delete(s.MACs, id)
	s.changed = true
	return nil
}

// SettleHousehold settles one household's TVs at now (the settlement job). Call it under the
// household lock.
func (d Domain) SettleHousehold(ctx context.Context, q *dbq.Queries, householdID uuid.UUID, now time.Time) error {
	s, err := d.Begin(ctx, q, householdID, now)
	if err != nil {
		return err
	}
	return s.Notify(ctx)
}
