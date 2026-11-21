package tv

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Reads compute from a Snapshot and never write: a session that has run out, or whose TV
// opened, reads as over before Settle records it.

func secondsUntil(end, now time.Time) int {
	d := end.Sub(now)
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func tptr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// OpenHoursView is open hours on the wire.
func OpenHoursView(h *OpenHours) *api.OpenHours {
	if h == nil {
		return nil
	}
	out := &api.OpenHours{Start: h.Start.String(), End: h.End.String()}
	if h.Days != 0 && h.Days != EveryDay {
		var days []api.Weekday
		for i, d := range weekdays {
			if h.Days&(1<<uint(i)) != 0 {
				days = append(days, d)
			}
		}
		out.Days = &days
	}
	return out
}

var weekdays = []api.Weekday{api.Sun, api.Mon, api.Tue, api.Wed, api.Thu, api.Fri, api.Sat}

// ParseOpenHours reads open hours from the wire; nil is none.
func ParseOpenHours(in *api.OpenHours) (*OpenHours, error) {
	if in == nil {
		return nil, nil
	}
	start, err := day.ParseTimeOfDay(in.Start)
	if err != nil {
		return nil, ErrOpenHours
	}
	end, err := day.ParseTimeOfDay(in.End)
	if err != nil {
		return nil, ErrOpenHours
	}
	h := &OpenHours{Start: start, End: end}
	if in.Days != nil {
		for _, d := range *in.Days {
			h.Days |= 1 << uint(slices.Index(weekdays, d)) //nolint:gosec // 0-6: the spec's enum
		}
		if h.Days == EveryDay {
			h.Days = 0
		}
	}
	return h, h.Valid()
}

// NormalizeMAC is a MAC in the stored form: lower case, colons.
func NormalizeMAC(mac string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(mac)), "-", ":")
}

func windowView(open bool, closesAt, opensAt time.Time) *api.WindowStatus {
	if open {
		return &api.WindowStatus{Open: true, ClosesAt: &closesAt}
	}
	return &api.WindowStatus{Open: false, OpensAt: &opensAt}
}

func hoursView(h HoursStatus) *api.WindowStatus {
	if !h.Rule {
		return nil
	}
	return windowView(h.Open, h.ClosesAt, h.OpensAt)
}

// TvView is a TV row on the wire, without its state.
func (s *Snapshot) TvView(t dbq.Tv) api.Tv {
	macs := s.MACs[t.ID]
	if macs == nil {
		macs = []string{}
	}
	return api.Tv{Id: t.ID, Name: t.Name, Room: t.Room, Macs: macs, HaEntityId: t.HaEntityID, Power: api.TvPower(t.Power),
		PowerChangedAt: t.PowerChangedAt, OpenHours: OpenHoursView(OpenHoursOf(t)), Version: int(t.Version)}
}

// sessionView is a session on the wire. Running sessions carry their end and time left.
func (s *Snapshot) sessionView(sess dbq.TvSession, tvName string, childName func(*uuid.UUID) string) api.TvSession {
	out := api.TvSession{Id: sess.ID, TvId: sess.TvID, TvName: tvName, Watcher: api.TvSessionWatcher(sess.Watcher), ChildId: sess.ChildID,
		StartedAt: sess.StartedAt, EndedAt: sess.EndedAt}
	if sess.EndReason != nil {
		r := api.TvSessionEndReason(*sess.EndReason)
		out.EndReason = &r
	}
	if sess.ChildID != nil {
		n := childName(sess.ChildID)
		out.ChildName = &n
	}
	until := s.Now
	if sess.EndedAt != nil {
		until = *sess.EndedAt
	} else {
		end := s.sessionEnd(sess, true)
		left := secondsUntil(end, s.Now)
		out.EndsAt, out.SecondsRemaining = &end, &left
		out.EndsWithHours = sess.Watcher == WatcherChild && end.Before(s.sessionEnd(sess, false))
		if end.Before(until) {
			until = end
		}
	}
	out.WatchedSeconds = max(0, int(until.Sub(sess.StartedAt)/time.Second))
	return out
}

func (s *Snapshot) tvName(id uuid.UUID) string {
	if t := s.tv(id); t != nil {
		return t.Name
	}
	return "TV"
}

// SessionView is a session on the wire, named from this snapshot.
func (s *Snapshot) SessionView(sess dbq.TvSession) api.TvSession {
	return s.sessionView(sess, s.tvName(sess.TvID), s.childName)
}

// IsChild reports whether id is a live child of the household.
func (s *Snapshot) IsChild(id uuid.UUID) bool { return s.child(id) != nil }

// children is each live child as "Turn on for…" offers them.
func (s *Snapshot) children(ctx context.Context) ([]api.TvChild, error) {
	paired, err := s.q.ChildrenWithDevices(ctx, s.H)
	if err != nil {
		return nil, err
	}
	out := make([]api.TvChild, 0, len(s.Children))
	for _, c := range s.Children {
		k, err := s.kid(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, api.TvChild{ChildId: c.ID, Name: c.Name, Color: c.Color, TvSecondsRemaining: k.seconds, WatchingTvId: k.watchingTv,
			Hours: hoursView(k.hours), CanWatch: k.canWatch(nil), CanStartOwn: slices.Contains(paired, c.ID) && k.canWatch(nil) && k.watchingTv == nil})
	}
	return out, nil
}

type kid struct {
	seconds    int
	watchingTv *uuid.UUID
	watching   *dbq.TvSession
	hours      HoursStatus
}

// canWatch: TV time left, inside their hours, and not watching a TV other than tv.
func (k kid) canWatch(tv *uuid.UUID) bool {
	return k.seconds > 0 && (!k.hours.Rule || k.hours.Open) && (k.watchingTv == nil || (tv != nil && *k.watchingTv == *tv))
}

func (s *Snapshot) kid(ctx context.Context, child uuid.UUID) (kid, error) {
	h, err := s.HoursFor(ctx, child)
	if err != nil {
		return kid{}, err
	}
	k := kid{hours: h, seconds: int(s.unspent(child) / time.Second)}
	if sess := s.childSession(child); sess != nil && s.isLive(*sess) {
		// The real balance, even when allowed hours will end the session sooner.
		k.seconds = secondsUntil(s.sessionEnd(*sess, false), s.Now)
		k.watchingTv, k.watching = &sess.TvID, sess
	}
	return k, nil
}

// List is every TV with its state, and each child's TV time.
func (s *Snapshot) List(ctx context.Context) (api.TvList, error) {
	kids, err := s.children(ctx)
	if err != nil {
		return api.TvList{}, err
	}
	out := api.TvList{ServerTime: s.Now, Children: kids, Tvs: make([]api.TvState, 0, len(s.TVs))}
	for _, t := range s.TVs {
		v := s.TvView(t)
		st := s.State(t)
		row := api.TvState{Id: v.Id, Name: v.Name, Room: v.Room, Macs: v.Macs, HaEntityId: v.HaEntityId, Power: v.Power, PowerChangedAt: v.PowerChangedAt,
			OpenHours: v.OpenHours, Version: v.Version, Open: st.Open, OpenClosesAt: tptr(st.ClosesAt), OpenUntil: tptr(st.OverrideUntil)}
		if st.Hours != nil {
			row.OpenHoursNow = windowView(st.Hours.Open, st.Hours.ClosesAt, st.Hours.OpensAt)
		}
		if st.Open {
			r := api.TvStateOpenReasonParent
			if st.Hours != nil && st.Hours.Open {
				r = api.TvStateOpenReasonOpenHours
			}
			row.OpenReason = &r
		}
		if sess := s.liveSession(t.ID); sess != nil {
			v := s.SessionView(*sess)
			row.Session = &v
		}
		if p := s.asking(t); p != nil {
			row.Prompt = &api.TvPrompt{Id: p.ID, Version: int(p.Version), Source: api.TvPromptSource(p.Source), CreatedAt: p.CreatedAt}
		}
		out.Tvs = append(out.Tvs, row)
	}
	return out, nil
}

// Display is what a wall display sees: names and state, no MACs or entities.
func (s *Snapshot) Display() api.DisplayTvList {
	out := api.DisplayTvList{ServerTime: s.Now, Tvs: make([]api.DisplayTv, 0, len(s.TVs))}
	for _, t := range s.TVs {
		st := s.State(t)
		row := api.DisplayTv{Id: t.ID, Name: t.Name, Room: t.Room, Power: api.TvPower(t.Power), Asking: s.asking(t) != nil && s.liveSession(t.ID) == nil,
			OpenClosesAt: tptr(st.ClosesAt), OpenUntil: tptr(st.OverrideUntil)}
		if sess := s.liveSession(t.ID); sess != nil {
			v := s.SessionView(*sess)
			ds := api.DisplayTvSession{Watcher: api.DisplayTvSessionWatcher(sess.Watcher), ChildId: v.ChildId, ChildName: v.ChildName}
			if sess.Watcher == WatcherChild {
				ds.EndsAt, ds.SecondsRemaining = v.EndsAt, v.SecondsRemaining
			}
			row.Session = &ds
		}
		out.Tvs = append(out.Tvs, row)
	}
	return out
}

// waiting is the TVs asking "Who's watching?" with nobody on them, oldest prompt first.
func (s *Snapshot) waiting() []api.WaitingTv {
	out := []api.WaitingTv{}
	for _, p := range s.Prompts { // oldest first
		t := s.tv(p.TvID)
		if t == nil || s.asking(*t) == nil || s.liveSession(t.ID) != nil {
			continue
		}
		out = append(out, api.WaitingTv{TvId: t.ID, TvName: t.Name, Room: t.Room})
	}
	return out
}

// Child is a child's TV time as the kid view shows it.
func (s *Snapshot) Child(ctx context.Context, child uuid.UUID) (api.ChildTv, error) {
	k, err := s.kid(ctx, child)
	if err != nil {
		return api.ChildTv{}, err
	}
	credits, err := s.q.OpenTvCredits(ctx, dbq.OpenTvCreditsParams{HouseholdID: s.H, ChildID: child})
	if err != nil {
		return api.ChildTv{}, err
	}
	out := api.ChildTv{ServerTime: s.Now, SecondsRemaining: k.seconds, Credits: make([]api.TvCredit, 0, len(credits)), Waiting: s.waiting(), Hours: hoursView(k.hours)}
	for _, c := range credits {
		v := api.TvCredit{Id: c.ID, Source: api.TvCreditSourceGrant, RedemptionId: c.RedemptionID, RewardTitle: c.RewardTitle, GrantedBy: c.GrantedByName,
			Minutes: int(c.GrantedSeconds / 60), Seconds: int(c.Seconds), UsedSeconds: int(c.UsedSeconds), CreatedAt: c.CreatedAt}
		if c.RedemptionID != nil {
			v.Source = api.TvCreditSourceRedemption
		}
		out.Credits = append(out.Credits, v)
	}
	if k.watching != nil {
		v := s.SessionView(*k.watching)
		out.Watching = &api.Watching{TvId: v.TvId, TvName: v.TvName, EndsAt: *v.EndsAt, SecondsRemaining: *v.SecondsRemaining, EndsWithHours: v.EndsWithHours}
	}
	return out, nil
}

// Allowances is the router's answer: each MAC of a TV that is open or has a running session.
func (s *Snapshot) Allowances() api.TvAllowances {
	out := api.TvAllowances{ServerTime: s.Now, Allowances: []api.TvAllowance{}}
	for _, t := range s.TVs {
		seconds := 0
		if st := s.State(t); st.Open {
			seconds = OpenSeconds(st.ClosesAt, s.Now)
		}
		if sess := s.liveSession(t.ID); sess != nil {
			seconds = max(seconds, RouterSeconds(s.sessionEnd(*sess, true).Sub(s.Now)))
		}
		if seconds <= 0 {
			continue
		}
		for _, mac := range s.MACs[t.ID] {
			out.Allowances = append(out.Allowances, api.TvAllowance{Mac: mac, SecondsRemaining: seconds})
		}
	}
	slices.SortFunc(out.Allowances, func(a, b api.TvAllowance) int { return strings.Compare(a.Mac, b.Mac) })
	return out
}

// History is the latest sessions and openings, newest first.
func (s *Snapshot) History(ctx context.Context) (api.TvHistory, error) {
	tvs, err := s.q.ListTvNames(ctx, s.H)
	if err != nil {
		return api.TvHistory{}, err
	}
	tvNames := map[uuid.UUID]string{}
	for _, t := range tvs {
		tvNames[t.ID] = t.Name
	}
	kids, err := s.q.ListChildren(ctx, dbq.ListChildrenParams{HouseholdID: s.H, IncludeArchived: true})
	if err != nil {
		return api.TvHistory{}, err
	}
	names := map[uuid.UUID]string{}
	for _, c := range kids {
		names[c.ID] = c.Name
	}
	childName := func(id *uuid.UUID) string { return names[*id] }
	ended, err := s.q.RecentTvSessions(ctx, dbq.RecentTvSessionsParams{HouseholdID: s.H, Max: recent})
	if err != nil {
		return api.TvHistory{}, err
	}
	out := api.TvHistory{Sessions: make([]api.TvSession, 0, len(ended)), Openings: []api.TvOpening{}}
	for _, sess := range ended {
		out.Sessions = append(out.Sessions, s.sessionView(sess, tvNames[sess.TvID], childName))
	}
	events, err := s.q.ListTvOpenEvents(ctx, dbq.ListTvOpenEventsParams{HouseholdID: s.H, Max: recent})
	if err != nil {
		return api.TvHistory{}, err
	}
	for _, e := range events {
		out.Openings = append(out.Openings, api.TvOpening{Id: e.ID, At: e.At, Action: api.TvOpeningAction(e.Action), TvId: e.TvID, TvName: e.TvName,
			Until: e.Until, ByName: e.ByName, ViaAssistant: e.ViaCredential != nil, EndedSessionId: e.EndedSessionID})
	}
	return out, nil
}
