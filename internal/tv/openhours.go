package tv

import (
	"errors"
	"time"

	"github.com/oldsj/opencozy-next/internal/day"
)

// OpenHours is a TV's daily window, in the household's time zone, when it is simply online: no
// "Who's watching?", nobody charged, and the router lists its MACs whatever its power or
// sessions. An end before the start crosses midnight (21:00 to 07:00). The start is inclusive
// and the end exclusive; wall times become instants through internal/day, so DST gaps and
// repeats read as everywhere else. Days is a weekday bitmask (bit 0 Sunday … bit 6 Saturday) of
// the days a window may start on, 0 for every day; a window crossing midnight belongs to the day
// it starts.
type OpenHours struct {
	Start, End day.TimeOfDay // 00:00 to 23:59, different
	Days       uint8
}

// EveryDay is the bitmask of all seven days.
const EveryDay uint8 = 0x7f

// ErrOpenHours means open hours that start and end at the same time, or out of range.
var ErrOpenHours = errors.New("tv: open hours start and end at different times, 00:00 to 23:59")

// Valid checks the window.
func (h OpenHours) Valid() error {
	if h.Start < 0 || h.Start >= day.Midnight || h.End < 0 || h.End >= day.Midnight || h.Start == h.End || h.Days > EveryDay {
		return ErrOpenHours
	}
	return nil
}

func (h OpenHours) window() day.Window {
	end := h.End
	if end == 0 {
		end = day.Midnight
	}
	return day.Window{From: h.Start, To: end}
}

func (h OpenHours) startsOn(d day.Date) bool {
	return h.Days == 0 || h.Days == EveryDay || h.Days&(1<<uint(d.Weekday())) != 0
}

// OpenStatus is where open hours stand at an instant: open from OpenedAt until ClosesAt, or
// closed until OpensAt.
type OpenStatus struct {
	Open               bool
	OpenedAt, ClosesAt time.Time
	OpensAt            time.Time
}

// Status is where h stands at t.
func (h OpenHours) Status(t time.Time, loc *time.Location) OpenStatus {
	today, w := day.Of(t, loc), h.window()
	// Yesterday's window may still be open (it crossed midnight), else today's.
	for _, d := range []day.Date{today.AddDays(-1), today} {
		if !h.startsOn(d) {
			continue
		}
		if s, e := w.On(d, loc); !t.Before(s) && t.Before(e) {
			return OpenStatus{Open: true, OpenedAt: s, ClosesAt: e}
		}
	}
	// The next window to start: today's if still ahead, else within a week.
	for i := 0; i <= 8; i++ {
		d := today.AddDays(i)
		if !h.startsOn(d) {
			continue
		}
		if s, _ := w.On(d, loc); s.After(t) {
			return OpenStatus{OpensAt: s}
		}
	}
	return OpenStatus{} // unreachable: some day of the next eight starts a window
}

// Override durations for "Open TVs".
const (
	Open1h     = "1h"
	Open2h     = "2h"
	OpenAllDay = "all_day"
)

// ExtendBy is what "+1 hour" adds to an override.
const ExtendBy = time.Hour

// MaxOverride is how far ahead an override (opened or extended) may end.
const MaxOverride = 24 * time.Hour

// OverrideEnd is when an override chosen at t ends: 1 or 2 hours later, or all day, until the
// next household midnight.
func OverrideEnd(duration string, t time.Time, loc *time.Location) time.Time {
	switch duration {
	case Open1h:
		return t.Add(time.Hour)
	case Open2h:
		return t.Add(2 * time.Hour)
	default:
		return day.Of(t, loc).End(loc)
	}
}

// OpenState is where a TV stands at an instant, counting its open hours and a parent's
// override. ClosesAt is the end of the whole open run (an override and open hours that touch or
// overlap are one run), zero while gated. OverrideUntil is the override's own end while one is
// active. Hours is the open hours' status, nil without open hours.
type OpenState struct {
	Open          bool
	ClosesAt      time.Time
	OverrideUntil time.Time
	Hours         *OpenStatus
}

// StateAt works out a TV's OpenState at t.
func StateAt(hours *OpenHours, openUntil *time.Time, t time.Time, loc *time.Location) OpenState {
	var st OpenState
	var end time.Time
	if hours != nil {
		s := hours.Status(t, loc)
		st.Hours = &s
		if s.Open {
			end = s.ClosesAt
		}
	}
	if openUntil != nil && openUntil.After(t) {
		st.OverrideUntil = *openUntil
		if openUntil.After(end) {
			end = *openUntil
		}
	}
	// A window opening exactly when (or before) this run ends joins it.
	for i := 0; !end.IsZero() && hours != nil && i < 3; i++ {
		next := hours.Status(end, loc)
		if !next.Open {
			break
		}
		end = next.ClosesAt
	}
	st.Open, st.ClosesAt = !end.IsZero(), end
	return st
}
