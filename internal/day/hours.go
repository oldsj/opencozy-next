package day

import (
	"errors"
	"time"
)

// AllowedHours is when a child may start spending their own screen time: one window on school
// nights (Sunday to Thursday, the days before a school day) and one on weekends (Friday and
// Saturday). Each runs from From to a later To on the same local day. A day's window is chosen by
// that household day's weekday, so Friday 23:00 is judged by Friday's weekend window and Sunday
// 18:00 by Sunday's school-night window. DST follows At's rules.
type AllowedHours struct {
	SchoolNight, Weekend Window
}

// ErrHours means a window of allowed hours is empty, runs past midnight, or is out of range.
var ErrHours = errors.New("day: allowed hours run from a start to a later end on the same day")

// Valid checks both windows: each starts before it ends, on the same day, before 24:00.
func (h AllowedHours) Valid() error {
	for _, w := range []Window{h.SchoolNight, h.Weekend} {
		if w.From < 0 || w.To >= Midnight || w.From >= w.To {
			return ErrHours
		}
	}
	return nil
}

// For is the window of date d.
func (h AllowedHours) For(d Date) Window {
	if wd := d.Weekday(); wd == time.Friday || wd == time.Saturday {
		return h.Weekend
	}
	return h.SchoolNight
}

// HoursNow is where a child stands at an instant: inside today's window (Start to End), or
// outside it until OpensAt (later today, or tomorrow's start).
type HoursNow struct {
	Open       bool
	Start, End time.Time // set when Open
	OpensAt    time.Time // set when not Open
}

// At is where t falls against the allowed hours.
func (h AllowedHours) At(t time.Time, loc *time.Location) HoursNow {
	today := Of(t, loc)
	start, end := h.For(today).On(today, loc)
	switch {
	case !t.Before(start) && t.Before(end):
		return HoursNow{Open: true, Start: start, End: end}
	case t.Before(start):
		return HoursNow{OpensAt: start}
	default:
		next := today.AddDays(1)
		s, _ := h.For(next).On(next, loc)
		return HoursNow{OpensAt: s}
	}
}

// AllDayEnd is when a parent's "all day" ends: the end of today's allowed hours if that is still
// ahead, otherwise the next midnight (also for a child with no rule).
func AllDayEnd(h *AllowedHours, now time.Time, loc *time.Location) time.Time {
	today := Of(now, loc)
	if h != nil {
		if _, end := h.For(today).On(today, loc); end.After(now) {
			return end
		}
	}
	return today.End(loc)
}
