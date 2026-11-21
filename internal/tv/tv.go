// Package tv is the TV gate (slice 9). TVs are household devices with no child attached: a name,
// an optional room and Home Assistant entity, and the MACs the router gates.
//
//   - The router (service credential, tv:gate) polls the allowances: each MAC of a TV with a
//     live session or that is open, with its seconds left, capped at AllowanceCap.
//   - Home Assistant reports power (tv:report, or slice 15's own poll through ApplyPower). A TV
//     turning on with nobody watching raises "Who's watching?".
//   - A parents session is never charged and lasts at most ParentsMax. A child's session spends
//     that child's TV time (tv_credit rows, oldest first, paused while nobody watches), ends at
//     zero or when the child's allowed hours end, and cannot start outside them.
//   - A TV is open (simply online, nobody asked, nobody charged, no sessions) inside its open
//     hours or while a parent has opened it ("Open TVs"). When it opens, its session ends.
//
// Accounting is exact and lazy: a child session's end is start + charged + the child's unspent
// time, so reads compute everything and never write. Writes, and the settlement job, settle
// what has run out first so history records when it ended. Every write runs under the
// household lock (internal/command), which serializes the whole domain.
package tv

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
)

// ParentsMax caps a parents session, so a missed "off" cannot leave a TV online overnight.
const ParentsMax = 4 * time.Hour

// Limits.
const (
	MaxTVs  = 50
	MaxMACs = 8
	recent  = 50
	// remind is how often asking again for a TV that is already asking reminds the parents.
	remind = 2 * time.Minute
)

// Watchers and end reasons.
const (
	WatcherParents = "parents"
	WatcherChild   = "child"

	EndTVOff          = "tv_off"
	EndTimeUp         = "time_up"
	EndParentEnded    = "parent_ended"
	EndDone           = "done"
	EndOpenHours      = "open_hours"
	EndParentOverride = "parent_override"
)

// Error is a refusal with a stable code: Status is 403, 404 or 409.
type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func conflict(code, format string, a ...any) *Error {
	return &Error{Status: 409, Code: code, Message: fmt.Sprintf(format, a...)}
}

// ErrNotFound is a TV or prompt that isn't there (or isn't this household's).
var ErrNotFound = &Error{Status: 404, Code: "not_found", Message: "Not found."}

// Hours is a child's allowed hours (slice 8, internal/screentime). A child's TV time can't start
// outside them, and a session ends when they end.
type Hours interface {
	Status(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID, now time.Time) (HoursStatus, error)
}

// HoursStatus is where a child's allowed hours stand: with no rule (Rule false) they are always
// open; otherwise open until ClosesAt, or closed until OpensAt.
type HoursStatus struct {
	Rule, Open        bool
	ClosesAt, OpensAt time.Time
}

// NoHours is Hours with no rules, until slice 8's allowed hours are wired in.
type NoHours struct{}

// Status always says "no rule".
func (NoHours) Status(context.Context, *dbq.Queries, uuid.UUID, uuid.UUID, time.Time) (HoursStatus, error) {
	return HoursStatus{}, nil
}

// Who did something: an adult (perhaps through an assistant credential) or a child's device.
type Who struct {
	Adult      *uuid.UUID
	Credential *uuid.UUID // the assistant credential acting for Adult
	Device     *uuid.UUID
}

// Domain is the TV gate's rules, with the household's allowed hours plugged in.
type Domain struct {
	Hours Hours
}

func (d Domain) hours() Hours {
	if d.Hours == nil {
		return NoHours{}
	}
	return d.Hours
}

// Snapshot is a consistent picture of one household's TVs at Now, loaded in the caller's
// transaction. Writes keep it current as they go.
type Snapshot struct {
	q        *dbq.Queries
	d        Domain
	H        uuid.UUID
	Now      time.Time
	Loc      *time.Location
	TVs      []dbq.Tv
	MACs     map[uuid.UUID][]string
	Open     []dbq.TvSession
	Credits  map[uuid.UUID][]dbq.TvCredit
	Prompts  []dbq.TvPrompt
	Children []dbq.Child // live, oldest first
	hours    map[uuid.UUID]HoursStatus
	changed  bool
}

// Load reads the household's TV state at now.
func (d Domain) Load(ctx context.Context, q *dbq.Queries, householdID uuid.UUID, now time.Time) (*Snapshot, error) {
	hh, err := q.GetHousehold(ctx, householdID)
	if err != nil {
		return nil, err
	}
	loc, err := day.Zone(hh.TimeZone)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{q: q, d: d, H: householdID, Now: now, Loc: loc, MACs: map[uuid.UUID][]string{}, Credits: map[uuid.UUID][]dbq.TvCredit{}, hours: map[uuid.UUID]HoursStatus{}}
	if s.TVs, err = q.ListLiveTvs(ctx, householdID); err != nil {
		return nil, err
	}
	macs, err := q.ListTvMacs(ctx, householdID)
	if err != nil {
		return nil, err
	}
	for _, m := range macs {
		s.MACs[m.TvID] = append(s.MACs[m.TvID], m.Mac)
	}
	if s.Open, err = q.ListOpenTvSessions(ctx, householdID); err != nil {
		return nil, err
	}
	credits, err := q.ListUnspentTvCredits(ctx, householdID)
	if err != nil {
		return nil, err
	}
	for _, c := range credits {
		s.Credits[c.ChildID] = append(s.Credits[c.ChildID], c)
	}
	if s.Prompts, err = q.ListPendingTvPrompts(ctx, householdID); err != nil {
		return nil, err
	}
	if s.Children, err = q.ListChildren(ctx, dbq.ListChildrenParams{HouseholdID: householdID, IncludeArchived: false}); err != nil {
		return nil, err
	}
	return s, nil
}

// Changed reports whether a write changed anything (the caller tells live clients).
func (s *Snapshot) Changed() bool { return s.changed }

// Notify tells live clients about the change, when there was one, from the writing transaction.
func (s *Snapshot) Notify(ctx context.Context) error {
	if !s.changed {
		return nil
	}
	return live.Notify(ctx, s.q, s.H, live.Event{Topic: live.TV})
}

func (s *Snapshot) tv(id uuid.UUID) *dbq.Tv {
	for i := range s.TVs {
		if s.TVs[i].ID == id {
			return &s.TVs[i]
		}
	}
	return nil
}

func (s *Snapshot) child(id uuid.UUID) *dbq.Child {
	for i := range s.Children {
		if s.Children[i].ID == id {
			return &s.Children[i]
		}
	}
	return nil
}

func (s *Snapshot) childName(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if c := s.child(*id); c != nil {
		return c.Name
	}
	return "A child"
}

func (s *Snapshot) openSession(tvID uuid.UUID) *dbq.TvSession {
	for i := range s.Open {
		if s.Open[i].TvID == tvID {
			return &s.Open[i]
		}
	}
	return nil
}

func (s *Snapshot) childSession(child uuid.UUID) *dbq.TvSession {
	for i := range s.Open {
		if s.Open[i].Watcher == WatcherChild && *s.Open[i].ChildID == child {
			return &s.Open[i]
		}
	}
	return nil
}

func (s *Snapshot) pending(tvID uuid.UUID) *dbq.TvPrompt {
	for i := range s.Prompts {
		if s.Prompts[i].TvID == tvID {
			return &s.Prompts[i]
		}
	}
	return nil
}

// OpenHoursOf is a TV row's open hours, nil without.
func OpenHoursOf(t dbq.Tv) *OpenHours {
	if !t.OpenStart.Valid || !t.OpenEnd.Valid {
		return nil
	}
	h := &OpenHours{Start: day.TimeOfDay(t.OpenStart.Int16), End: day.TimeOfDay(t.OpenEnd.Int16)}
	if t.OpenDays.Valid {
		h.Days = uint8(t.OpenDays.Int16) //nolint:gosec // 1-126 by the table
	}
	return h
}

// State is where a TV stands now: its open hours and a parent's override.
func (s *Snapshot) State(t dbq.Tv) OpenState {
	return StateAt(OpenHoursOf(t), t.OpenUntil, s.Now, s.Loc)
}

// unspent is a child's TV time as stored (an open session's elapsed time beyond its charge is
// not taken yet).
func (s *Snapshot) unspent(child uuid.UUID) time.Duration {
	var n int64
	for _, c := range s.Credits[child] {
		n += int64(c.Seconds - c.UsedSeconds)
	}
	return time.Duration(n) * time.Second
}

// sessionEnd is when an open session ends at the latest: a parents session at ParentsMax, a
// child's when their time runs out or, withHours, when their allowed hours end, if sooner.
func (s *Snapshot) sessionEnd(sess dbq.TvSession, withHours bool) time.Time {
	if sess.Watcher == WatcherParents {
		return sess.StartedAt.Add(ParentsMax)
	}
	end := sess.StartedAt.Add(time.Duration(sess.ChargedSeconds)*time.Second + s.unspent(*sess.ChildID))
	if withHours && sess.EndsBy != nil && sess.EndsBy.Before(end) {
		return *sess.EndsBy
	}
	return end
}

// isLive reports whether an open session is still running now: not run out, its child live and
// its TV not open. A session that isn't is over even before it is settled.
func (s *Snapshot) isLive(sess dbq.TvSession) bool {
	t := s.tv(sess.TvID)
	if t == nil || s.State(*t).Open {
		return false
	}
	if sess.ChildID != nil && s.child(*sess.ChildID) == nil {
		return false
	}
	return s.sessionEnd(sess, true).After(s.Now)
}

// liveSession is a TV's running session, or nil.
func (s *Snapshot) liveSession(tvID uuid.UUID) *dbq.TvSession {
	if sess := s.openSession(tvID); sess != nil && s.isLive(*sess) {
		return sess
	}
	return nil
}

// asking is a TV's pending prompt, unless the TV is open (an open TV never asks).
func (s *Snapshot) asking(t dbq.Tv) *dbq.TvPrompt {
	if s.State(t).Open {
		return nil
	}
	return s.pending(t.ID)
}

// HoursFor is a child's allowed hours now, read once per snapshot.
func (s *Snapshot) HoursFor(ctx context.Context, child uuid.UUID) (HoursStatus, error) {
	if h, ok := s.hours[child]; ok {
		return h, nil
	}
	h, err := s.d.hours().Status(ctx, s.q, s.H, child, s.Now)
	if err != nil {
		return HoursStatus{}, err
	}
	s.hours[child] = h
	return h, nil
}

// charge takes a child session's elapsed whole seconds, up to at, from the child's credits,
// oldest first.
func (s *Snapshot) charge(ctx context.Context, sess *dbq.TvSession, at time.Time) error {
	if sess.Watcher != WatcherChild {
		return nil
	}
	child := *sess.ChildID
	until := s.sessionEnd(*sess, true)
	if at.Before(until) {
		until = at
	}
	owed := int32(until.Sub(sess.StartedAt)/time.Second) - sess.ChargedSeconds //nolint:gosec // a session lasts hours, not decades
	if owed <= 0 {
		return nil
	}
	credits := s.Credits[child]
	var taken int32
	for i := range credits {
		if owed <= 0 {
			break
		}
		c := &credits[i]
		take := min(owed, c.Seconds-c.UsedSeconds)
		if take <= 0 {
			continue
		}
		c.UsedSeconds += take
		if err := s.q.SpendTvCredit(ctx, dbq.SpendTvCreditParams{HouseholdID: s.H, ID: c.ID, UsedSeconds: c.UsedSeconds}); err != nil {
			return err
		}
		owed -= take
		taken += take
	}
	if taken == 0 {
		return nil
	}
	sess.ChargedSeconds += taken
	s.changed = true
	return s.q.ChargeTvSession(ctx, dbq.ChargeTvSessionParams{HouseholdID: s.H, ID: sess.ID, ChargedSeconds: sess.ChargedSeconds})
}

// endSession ends an open session at at, or when it ran out if that was earlier (time_up).
func (s *Snapshot) endSession(ctx context.Context, sess dbq.TvSession, at time.Time, reason string, by *uuid.UUID) (dbq.TvSession, error) {
	if err := s.charge(ctx, &sess, at); err != nil {
		return dbq.TvSession{}, err
	}
	if end := s.sessionEnd(sess, true); !end.After(at) {
		at, reason, by = end, EndTimeUp, nil
	}
	if at.Before(sess.StartedAt) {
		at = sess.StartedAt
	}
	ended, err := s.q.EndTvSession(ctx, dbq.EndTvSessionParams{HouseholdID: s.H, ID: sess.ID, EndedAt: &at, EndReason: &reason, EndedByAdult: by})
	if err != nil {
		return dbq.TvSession{}, err
	}
	s.Open = slices.DeleteFunc(s.Open, func(o dbq.TvSession) bool { return o.ID == sess.ID })
	s.changed = true
	return ended, nil
}

// resolve decides a TV's pending prompt, if it has one.
func (s *Snapshot) resolve(ctx context.Context, tvID uuid.UUID, state string, who Who, session *uuid.UUID) (*dbq.TvPrompt, error) {
	if s.pending(tvID) == nil {
		return nil, nil
	}
	p, err := s.q.ResolveTvPrompt(ctx, dbq.ResolveTvPromptParams{HouseholdID: s.H, TvID: tvID, State: state, Now: s.Now,
		DecidedByAdult: who.Adult, DecidedByDevice: who.Device, SessionID: session})
	if err != nil {
		return nil, err
	}
	s.Prompts = slices.DeleteFunc(s.Prompts, func(o dbq.TvPrompt) bool { return o.TvID == tvID })
	s.changed = true
	return &p, nil
}

// Settle ends every open session that is over: run out (time_up), its child archived
// (parent_ended), or its TV open: open hours began (open_hours, at their start, or now for a
// session started after it or on editedTv, whose hours were just changed) or a parent opened it
// (parent_override, now). Prompts on open TVs are withdrawn.
func (s *Snapshot) Settle(ctx context.Context, editedTv *uuid.UUID) error {
	for _, t := range s.TVs {
		if s.State(t).Open {
			if _, err := s.resolve(ctx, t.ID, "withdrawn", Who{}, nil); err != nil {
				return err
			}
		}
	}
	for _, sess := range slices.Clone(s.Open) {
		if err := s.settleOne(ctx, sess, editedTv); err != nil {
			return err
		}
	}
	return nil
}

func (s *Snapshot) settleOne(ctx context.Context, sess dbq.TvSession, editedTv *uuid.UUID) error {
	t := s.tv(sess.TvID)
	if t == nil { // a removed TV ends its session; a leftover ends now
		_, err := s.endSession(ctx, sess, s.Now, EndParentEnded, nil)
		return err
	}
	if st := s.State(*t); st.Open {
		if st.Hours == nil || !st.Hours.Open {
			_, err := s.endSession(ctx, sess, s.Now, EndParentOverride, nil)
			return err
		}
		at := s.Now
		if sess.StartedAt.Before(st.Hours.OpenedAt) && (editedTv == nil || *editedTv != t.ID) {
			at = st.Hours.OpenedAt
		}
		_, err := s.endSession(ctx, sess, at, EndOpenHours, nil)
		return err
	}
	if sess.ChildID != nil && s.child(*sess.ChildID) == nil {
		_, err := s.endSession(ctx, sess, s.Now, EndParentEnded, nil)
		return err
	}
	if !s.sessionEnd(sess, true).After(s.Now) {
		_, err := s.endSession(ctx, sess, s.Now, EndTimeUp, nil)
		return err
	}
	return nil
}

// AddCredit gives a child TV time (slice 8: a TV-target redemption or grant). Call it under the
// household lock with the command's time; settle first is not needed, since a session that ran
// out is not revived: it has already ended at its end in the books Settle keeps.
func (d Domain) AddCredit(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID, redemptionID *uuid.UUID, seconds int32, now time.Time) error {
	s, err := d.Load(ctx, q, householdID, now)
	if err != nil {
		return err
	}
	if err := s.Settle(ctx, nil); err != nil {
		return err
	}
	if _, err := q.AddTvCredit(ctx, dbq.AddTvCreditParams{HouseholdID: householdID, ChildID: childID, RedemptionID: redemptionID, Seconds: seconds, Now: now}); err != nil {
		return err
	}
	s.changed = true
	return s.Notify(ctx)
}

// ErrCreditUsed means a TV credit being cancelled has no unused time left.
var ErrCreditUsed = conflict("tv_time_used", "That TV time has already been used.")

// CancelCredit forfeits a redemption's unused TV time and returns the share of cost to refund
// (all of it when none was used), rounded down. A watching child is charged up to now first,
// and a session left with no time then ends. Call it under the household lock.
func (d Domain) CancelCredit(ctx context.Context, q *dbq.Queries, householdID, redemptionID uuid.UUID, cost int32, now time.Time) (int32, error) {
	s, err := d.Load(ctx, q, householdID, now)
	if err != nil {
		return 0, err
	}
	if err := s.Settle(ctx, nil); err != nil {
		return 0, err
	}
	c, err := q.GetTvCreditByRedemption(ctx, dbq.GetTvCreditByRedemptionParams{HouseholdID: householdID, RedemptionID: &redemptionID})
	if err != nil {
		return 0, err
	}
	if sess := s.childSession(c.ChildID); sess != nil {
		if err := s.charge(ctx, sess, now); err != nil {
			return 0, err
		}
		if c, err = q.GetTvCreditByRedemption(ctx, dbq.GetTvCreditByRedemptionParams{HouseholdID: householdID, RedemptionID: &redemptionID}); err != nil {
			return 0, err
		}
	}
	if c.UsedSeconds >= c.Seconds {
		return 0, ErrCreditUsed
	}
	refund := cost
	if c.UsedSeconds > 0 {
		refund = int32(int64(cost) * int64(c.Seconds-c.UsedSeconds) / int64(c.GrantedSeconds)) //nolint:gosec // at most cost
	}
	if err := q.CancelTvCredit(ctx, dbq.CancelTvCreditParams{HouseholdID: householdID, ID: c.ID}); err != nil {
		return 0, err
	}
	credits := s.Credits[c.ChildID]
	for i := range credits {
		if credits[i].ID == c.ID {
			credits[i].Seconds = credits[i].UsedSeconds
		}
	}
	s.changed = true
	if err := s.Settle(ctx, nil); err != nil {
		return 0, err
	}
	return refund, s.Notify(ctx)
}
