// Package screentime is a child's iPad time (slice 8). Minutes a child gets are banked as a
// credit: a screen-time reward bought with points, a parent's grant, or a parent's yes to "Ask
// for more time". Nothing runs until the time is spent. Spending draws on the oldest credits first
// and writes unlock windows, back to back after any window already queued; the child's devices
// read them. Windows are append-only: a parent ending time early writes a cut, and every reader
// uses a window's effective end.
//
// A child's own spending is limited by their allowed hours (internal/day): refused outside them,
// and cut off at their end with the rest left in the bank. An adult's is not. Spent time is never
// refunded, except when a parent ends it early and chooses to give the unused time back.
//
// Writes take a *command.Cmd and need the child locked; times are the command's.
package screentime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

const (
	// MaxMinutes is the most a child spends, or a parent gives, at once.
	MaxMinutes = 240
	// MaxGrantMinutes is the most one parent grant can hold ("all day" on a 25-hour day).
	MaxGrantMinutes = 1440
	// AllowanceCap is the most a device is told it may stay unlocked without asking again, so a
	// device that loses the server locks within it (design §3 rule 6).
	AllowanceCap = 300 * time.Second
)

// Credit sources.
const (
	SourceRedemption  = "redemption"
	SourceGrant       = "grant"
	SourceTimeRequest = "time_request"
)

var (
	// ErrNotLocked means the child's lock isn't held.
	ErrNotLocked = errors.New("screentime: child not locked")
	// ErrNothingBanked means there is no time left to spend.
	ErrNothingBanked = errors.New("screentime: nothing banked")
	// ErrNothingToEnd means no time is running or queued (or, for "queued", nothing is queued).
	ErrNothingToEnd = errors.New("screentime: nothing to end")
	// ErrAllUsed means a redemption's time has all been spent, so cancelling it gives nothing back.
	ErrAllUsed = errors.New("screentime: all used")
	// ErrNoCredit means a screen-time redemption has no iPad credit, so what was used is unknown.
	ErrNoCredit = errors.New("screentime: no credit for the redemption")
	// ErrRewardCredit refuses taking back a reward's time directly: cancel the reward instead.
	ErrRewardCredit = errors.New("screentime: a reward's credit")
	// ErrNothingLeft means a credit is all used.
	ErrNothingLeft = errors.New("screentime: nothing left")
)

// ErrClosed refuses a child's own spending outside their allowed hours.
type ErrClosed struct{ OpensAt time.Time }

func (e ErrClosed) Error() string { return fmt.Sprintf("screentime: closed until %s", e.OpensAt) }

// ErrQueueFull refuses a child's own spending when time is already queued to the end of today's
// allowed hours.
type ErrQueueFull struct{ Until time.Time }

func (e ErrQueueFull) Error() string { return fmt.Sprintf("screentime: queued until %s", e.Until) }

// ---------------------------------------------------------------- allowed hours

// Rule is a child's allowed hours (nil: no rule) and the household's zone.
type Rule struct {
	Hours    *day.AllowedHours
	Zone     *time.Location
	Archived bool
}

// ChildRule reads the child's allowed hours. pgx.ErrNoRows when there is no such child.
func ChildRule(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID) (Rule, error) {
	row, err := q.GetChildHours(ctx, dbq.GetChildHoursParams{HouseholdID: householdID, ID: childID})
	if err != nil {
		return Rule{}, err
	}
	loc, err := day.Zone(row.TimeZone)
	if err != nil {
		return Rule{}, err
	}
	r := Rule{Zone: loc, Archived: row.ArchivedAt != nil}
	if row.SchoolNightStart.Valid && row.SchoolNightEnd.Valid && row.WeekendStart.Valid && row.WeekendEnd.Valid {
		r.Hours = &day.AllowedHours{
			SchoolNight: day.Window{From: day.TimeOfDay(row.SchoolNightStart.Int16), To: day.TimeOfDay(row.SchoolNightEnd.Int16)},
			Weekend:     day.Window{From: day.TimeOfDay(row.WeekendStart.Int16), To: day.TimeOfDay(row.WeekendEnd.Int16)},
		}
	}
	return r, nil
}

// SetRule replaces the child's allowed hours; nil clears them.
func SetRule(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID, h *day.AllowedHours) error {
	p := dbq.SetChildHoursParams{HouseholdID: householdID, ID: childID}
	if h != nil {
		if err := h.Valid(); err != nil {
			return err
		}
		p.SchoolNightStart = pgtype.Int2{Int16: int16(h.SchoolNight.From), Valid: true} //nolint:gosec // < 1440
		p.SchoolNightEnd = pgtype.Int2{Int16: int16(h.SchoolNight.To), Valid: true}     //nolint:gosec // < 1440
		p.WeekendStart = pgtype.Int2{Int16: int16(h.Weekend.From), Valid: true}         //nolint:gosec // < 1440
		p.WeekendEnd = pgtype.Int2{Int16: int16(h.Weekend.To), Valid: true}             //nolint:gosec // < 1440
	}
	return q.SetChildHours(ctx, p)
}

// Hours is where a child stands against their allowed hours at an instant. Rule is false for a
// child with no rule, who may always start; otherwise Open until ClosesAt, or closed until OpensAt.
type Hours struct {
	Rule, Open        bool
	ClosesAt, OpensAt time.Time
}

// Status is where the rule stands at now.
func (r Rule) Status(now time.Time) Hours {
	if r.Hours == nil {
		return Hours{Open: true}
	}
	at := r.Hours.At(now, r.Zone)
	if at.Open {
		return Hours{Rule: true, Open: true, ClosesAt: at.End}
	}
	return Hours{Rule: true, OpensAt: at.OpensAt}
}

// HoursStatus is where the child stands at now (for the TV gate, slice 9).
func HoursStatus(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID, now time.Time) (Hours, error) {
	r, err := ChildRule(ctx, q, householdID, childID)
	if err != nil {
		return Hours{}, err
	}
	return r.Status(now), nil
}

// ---------------------------------------------------------------- reading

// Window is an unlock window, to its effective end.
type Window struct {
	ID, CreditID     uuid.UUID
	StartsAt, EndsAt time.Time
}

// Windows lists the child's windows still running or queued after at, in queue order.
func Windows(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID, at time.Time) ([]Window, error) {
	rows, err := q.OpenWindows(ctx, dbq.OpenWindowsParams{HouseholdID: householdID, ChildID: childID, At: at})
	if err != nil {
		return nil, err
	}
	out := make([]Window, len(rows))
	for i, r := range rows {
		out[i] = Window{ID: r.ID, CreditID: r.CreditID, StartsAt: r.StartsAt, EndsAt: r.EndsAt}
	}
	return out, nil
}

// UnlockedUntil is the end of the back-to-back run of windows covering now, or nil when locked.
func UnlockedUntil(ws []Window, now time.Time) *time.Time {
	var until *time.Time
	for _, w := range ws {
		from := now
		if until != nil {
			from = *until
		}
		if !w.StartsAt.After(from) && w.EndsAt.After(from) {
			end := w.EndsAt
			until = &end
		}
	}
	return until
}

// queueEnd is when the child's last window ends, if one is still to run after at.
func queueEnd(ws []Window) *time.Time {
	var last *time.Time
	for _, w := range ws {
		if last == nil || w.EndsAt.After(*last) {
			end := w.EndsAt
			last = &end
		}
	}
	return last
}

// Banked is the child's seconds not yet spent.
func Banked(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID) (int64, error) {
	return q.BankedSeconds(ctx, dbq.BankedSecondsParams{HouseholdID: householdID, ChildID: childID})
}

// ---------------------------------------------------------------- writing

// Credit is time to bank.
type Credit struct {
	Child        uuid.UUID
	Source       string
	RedemptionID *uuid.UUID
	GrantedBy    *uuid.UUID
	// GrantedVia is the assistant credential a grant came through, if any.
	GrantedVia *uuid.UUID
	Minutes    int32
	// Seconds defaults to the whole minutes; less when a grant ends at an exact time.
	Seconds int32
}

// AddCredit banks time for the child.
func AddCredit(ctx context.Context, c *command.Cmd, cr Credit) (dbq.ScreenTimeCredit, error) {
	if !c.Holds(cr.Child) {
		return dbq.ScreenTimeCredit{}, ErrNotLocked
	}
	now, err := c.Now(ctx)
	if err != nil {
		return dbq.ScreenTimeCredit{}, err
	}
	if cr.Seconds == 0 {
		cr.Seconds = cr.Minutes * 60
	}
	return c.Q.InsertCredit(ctx, dbq.InsertCreditParams{
		HouseholdID: c.HouseholdID(), ChildID: cr.Child, Source: cr.Source, RedemptionID: cr.RedemptionID,
		GrantedBy: cr.GrantedBy, GrantedVia: cr.GrantedVia, Minutes: cr.Minutes, Seconds: cr.Seconds, Now: now,
	})
}

// Spent is a spend's run: StartsAt to EndsAt (several back-to-back windows when it drew on
// several credits). Clipped when allowed hours cut it short.
type Spent struct {
	Seconds          int64
	StartsAt, EndsAt time.Time
	Clipped          bool
}

// Spend spends up to seconds of the child's banked time (what is left, when less), starting when
// their queue ends. limit is the end of today's allowed hours for a child's own spending (nil for
// an adult, or no rule): the run stops there. first is a credit spent before older ones (a
// parent's "start now" spends the grant it just made).
func Spend(ctx context.Context, c *command.Cmd, child uuid.UUID, seconds int64, first *uuid.UUID, limit *time.Time) (Spent, error) {
	if !c.Holds(child) {
		return Spent{}, ErrNotLocked
	}
	h := c.HouseholdID()
	now, err := c.Now(ctx)
	if err != nil {
		return Spent{}, err
	}
	credits, err := c.Q.UnspentCredits(ctx, dbq.UnspentCreditsParams{HouseholdID: h, ChildID: child})
	if err != nil {
		return Spent{}, err
	}
	if first != nil {
		for i, cr := range credits {
			if cr.ID == *first {
				credits = append(append([]dbq.ScreenTimeCredit{cr}, credits[:i]...), credits[i+1:]...)
				break
			}
		}
	}
	var banked int64
	for _, cr := range credits {
		banked += int64(cr.Seconds - cr.UsedSeconds)
	}
	want := min(seconds, banked)
	if want <= 0 {
		return Spent{}, ErrNothingBanked
	}
	ws, err := Windows(ctx, c.Q, h, child, now)
	if err != nil {
		return Spent{}, err
	}
	start := now
	if end := queueEnd(ws); end != nil {
		start = *end
	}
	end, clipped := start.Add(time.Duration(want)*time.Second), false
	if limit != nil && end.After(*limit) {
		if !start.Before(*limit) {
			return Spent{}, ErrQueueFull{Until: start}
		}
		end, clipped = *limit, true
	}
	// Charge whole seconds, rounded up (never free); the rest stays banked.
	charge := min(want, int64((end.Sub(start)+time.Second-1)/time.Second))
	cursor, owed := start, charge
	for _, cr := range credits {
		if owed <= 0 {
			break
		}
		take := min(owed, int64(cr.Seconds-cr.UsedSeconds))
		owed -= take
		wEnd := cursor.Add(time.Duration(take) * time.Second)
		if owed == 0 {
			wEnd = end
		}
		if _, err := c.Q.InsertWindow(ctx, dbq.InsertWindowParams{
			HouseholdID: h, ChildID: child, CreditID: cr.ID, StartsAt: cursor, EndsAt: wEnd, Now: now,
		}); err != nil {
			return Spent{}, err
		}
		if err := c.Q.SetCreditUsed(ctx, dbq.SetCreditUsedParams{HouseholdID: h, ID: cr.ID, UsedSeconds: cr.UsedSeconds + int32(take)}); err != nil { //nolint:gosec // at most the credit's seconds
			return Spent{}, err
		}
		cursor = wEnd
	}
	return Spent{Seconds: charge, StartsAt: start, EndsAt: end, Clipped: clipped}, nil
}

// SpendLimit is the latest a child's own spending may run at now: the end of today's allowed
// hours, nil with no rule, or ErrClosed outside them.
func (r Rule) SpendLimit(now time.Time) (*time.Time, error) {
	s := r.Status(now)
	switch {
	case !s.Rule:
		return nil, nil
	case !s.Open:
		return nil, ErrClosed{OpensAt: s.OpensAt}
	}
	return &s.ClosesAt, nil
}

// Scope says what ending time early ends.
type Scope string

// Scopes.
const (
	// Now locks the device now and cancels everything queued.
	Now Scope = "now"
	// Queued lets the running window finish and cancels what is queued after it.
	Queued Scope = "queued"
)

// Ended is what ending time early did. Cut seconds that weren't refunded are forfeited.
type Ended struct {
	Windows              int
	CutSeconds, Refunded int64
	// EndedAt is when the device locked (scope Now); KeptUntil is when the running window still
	// ends (scope Queued).
	EndedAt, KeptUntil *time.Time
}

// End ends a child's time early: each affected window gets a cut. With refund, the unused
// seconds go back to the credits they came from (never to a cancelled redemption's).
func End(ctx context.Context, c *command.Cmd, child uuid.UUID, scope Scope, refund bool, by *uuid.UUID) (Ended, error) {
	if !c.Holds(child) {
		return Ended{}, ErrNotLocked
	}
	h := c.HouseholdID()
	now, err := c.Now(ctx)
	if err != nil {
		return Ended{}, err
	}
	ws, err := Windows(ctx, c.Q, h, child, now)
	if err != nil {
		return Ended{}, err
	}
	var running *Window
	var targets []Window
	for i, w := range ws {
		if !w.StartsAt.After(now) {
			running = &ws[i]
		}
		if scope == Now || w.StartsAt.After(now) {
			targets = append(targets, w)
		}
	}
	if len(targets) == 0 {
		return Ended{}, ErrNothingToEnd
	}
	var out Ended
	for _, w := range targets {
		cutAt := now
		if w.StartsAt.After(now) {
			cutAt = w.StartsAt
		}
		unused := int64(w.EndsAt.Sub(cutAt) / time.Second)
		var refunded int64
		if refund && unused > 0 {
			cr, err := c.Q.GetCredit(ctx, dbq.GetCreditParams{HouseholdID: h, ID: w.CreditID})
			if err != nil {
				return Ended{}, err
			}
			ok, err := refundable(ctx, c.Q, h, cr)
			if err != nil {
				return Ended{}, err
			}
			if ok {
				refunded = min(unused, int64(cr.UsedSeconds))
				if err := c.Q.SetCreditUsed(ctx, dbq.SetCreditUsedParams{HouseholdID: h, ID: cr.ID, UsedSeconds: cr.UsedSeconds - int32(refunded)}); err != nil { //nolint:gosec // at most used
					return Ended{}, err
				}
			}
		}
		if err := c.Q.InsertCut(ctx, dbq.InsertCutParams{
			HouseholdID: h, WindowID: w.ID, CutAt: cutAt, RefundedSeconds: int32(refunded), CutBy: by, Now: now, //nolint:gosec // at most a day
		}); err != nil {
			return Ended{}, err
		}
		out.Windows++
		out.CutSeconds += unused
		out.Refunded += refunded
	}
	if scope == Now {
		out.EndedAt = &now
	} else if running != nil {
		out.KeptUntil = &running.EndsAt
	}
	return out, nil
}

// refundable is false for a credit whose redemption was cancelled, or that a parent took back: its
// rest stays forfeited.
func refundable(ctx context.Context, q *dbq.Queries, h uuid.UUID, cr dbq.ScreenTimeCredit) (bool, error) {
	if cr.ForfeitedAt != nil {
		return false, nil
	}
	if cr.RedemptionID == nil {
		return true, nil
	}
	red, err := q.GetRedemptionRow(ctx, dbq.GetRedemptionRowParams{HouseholdID: h, ID: *cr.RedemptionID})
	if err != nil {
		return false, err
	}
	return red.State != "cancelled", nil
}

// CancelRedemption forfeits the unspent rest of a screen-time redemption's credit and returns the
// points to give back: all of cost when none was spent, otherwise the unspent share, rounded
// down. ErrAllUsed when everything is spent.
func CancelRedemption(ctx context.Context, c *command.Cmd, redemptionID uuid.UUID, cost int32) (int32, error) {
	h := c.HouseholdID()
	cr, err := c.Q.CreditForRedemption(ctx, dbq.CreditForRedemptionParams{HouseholdID: h, RedemptionID: &redemptionID})
	if errors.Is(err, pgx.ErrNoRows) {
		// No record of how much was used (an imported redemption whose credit wasn't carried, or
		// another screen's): fail closed rather than refund time that may have been watched.
		return 0, ErrNoCredit
	} else if err != nil {
		return 0, err
	}
	if !c.Holds(cr.ChildID) {
		return 0, ErrNotLocked
	}
	if cr.UsedSeconds >= cr.Seconds {
		return 0, ErrAllUsed
	}
	refund := cost
	if cr.UsedSeconds > 0 {
		refund = int32(int64(cost) * int64(cr.Seconds-cr.UsedSeconds) / (int64(cr.Minutes) * 60)) //nolint:gosec // at most cost
	}
	if err := c.Q.SetCreditSeconds(ctx, dbq.SetCreditSecondsParams{HouseholdID: h, ID: cr.ID, Seconds: cr.UsedSeconds}); err != nil {
		return 0, err
	}
	return refund, nil
}

// Forfeit takes back the unused rest of a parent's grant or "yes" (the child locked by the caller)
// and returns how many seconds went. A reward's credit is refused (ErrRewardCredit: cancel the
// reward, which refunds its points); ErrNothingLeft when it is all used; pgx.ErrNoRows when the
// child has no such credit.
func Forfeit(ctx context.Context, c *command.Cmd, child, credit uuid.UUID, by uuid.UUID) (int32, error) {
	if !c.Holds(child) {
		return 0, ErrNotLocked
	}
	h := c.HouseholdID()
	cr, err := c.Q.LockCredit(ctx, dbq.LockCreditParams{HouseholdID: h, ChildID: child, ID: credit})
	if err != nil {
		return 0, err
	}
	switch {
	case cr.Source == SourceRedemption:
		return 0, ErrRewardCredit
	case cr.UsedSeconds >= cr.Seconds:
		return 0, ErrNothingLeft
	}
	now, err := c.Now(ctx)
	if err != nil {
		return 0, err
	}
	if err := c.Q.ForfeitCredit(ctx, dbq.ForfeitCreditParams{HouseholdID: h, ID: cr.ID, ForfeitedBy: &by, Now: &now}); err != nil {
		return 0, err
	}
	return cr.Seconds - cr.UsedSeconds, nil
}
