// Package day is the household day: dates, times of day and daily windows in the household's
// IANA time zone. Everything that asks "which day is it", "when does today start" or "is this
// within the allowed hours" uses it, so they agree across DST changes:
//
//   - A local time that does not exist (skipped when clocks go forward) takes the offset from
//     before the gap: 02:30 on the US spring-forward day is 03:30 daylight time.
//   - A local time that happens twice (when clocks go back) means its first occurrence.
//   - A day starts at its first instant (00:00, or the end of a gap at midnight) and ends where
//     the next day starts, so it can be 23 or 25 hours long.
//
// The time zone database is built in, so results do not depend on the host's.
package day

import (
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // the same zone rules on every host
)

// Zone loads a household's IANA time zone. "Local" and "" are refused: a household's zone is
// always named.
func Zone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("day: %q is not an IANA time zone", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("day: %q is not an IANA time zone", name)
	}
	return loc, nil
}

// Date is a calendar day, without a zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Of is the household day that t falls on.
func Of(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{y, m, d}
}

// ParseDate reads YYYY-MM-DD.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("day: %q is not a date (YYYY-MM-DD)", s)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// AddDays is the date n days later (earlier for negative n).
func (d Date) AddDays(n int) Date {
	y, m, dd := time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC).Date()
	return Date{y, m, dd}
}

// Compare is -1, 0 or +1 as d is before, the same as or after e.
func (d Date) Compare(e Date) int {
	a := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
	return a.Compare(time.Date(e.Year, e.Month, e.Day, 0, 0, 0, 0, time.UTC))
}

// Weekday is the date's day of the week.
func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC).Weekday()
}

// Start is the first instant of the day.
func (d Date) Start(loc *time.Location) time.Time { return d.At(0, loc) }

// End is the first instant of the next day: a day is [Start, End).
func (d Date) End(loc *time.Location) time.Time { return d.AddDays(1).Start(loc) }

// At is the instant of time-of-day t on the date, with the gap and repeat rules above. At(Midnight)
// is the next day's start.
func (d Date) At(t TimeOfDay, loc *time.Location) time.Time {
	if t == Midnight {
		return d.End(loc)
	}
	// The wall time, written as if it were UTC.
	wall := time.Date(d.Year, d.Month, d.Day, int(t)/60, int(t)%60, 0, 0, time.UTC)
	// The offsets a day before and after bracket any transition near this wall time.
	_, before := wall.Add(-24 * time.Hour).In(loc).Zone()
	_, after := wall.Add(24 * time.Hour).In(loc).Zone()
	first := wall.Add(-time.Duration(before) * time.Second)
	second := wall.Add(-time.Duration(after) * time.Second)
	firstOK := offsetAt(first, loc) == before
	secondOK := offsetAt(second, loc) == after
	switch {
	case firstOK && secondOK: // repeated, or no transition: the first occurrence
		if second.Before(first) {
			return second
		}
		return first
	case firstOK:
		return first
	case secondOK:
		return second
	default: // skipped: the offset from before the gap
		return first
	}
}

func offsetAt(t time.Time, loc *time.Location) int {
	_, off := t.In(loc).Zone()
	return off
}

// TimeOfDay is minutes after midnight, from 0 (00:00) to Midnight (24:00, the end of the day).
type TimeOfDay int

// Midnight is 24:00: the end of a day, for windows that run to midnight.
const Midnight TimeOfDay = 24 * 60

// ParseTimeOfDay reads HH:MM, 00:00 to 24:00.
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	bad := fmt.Errorf("day: %q is not a time (HH:MM)", s)
	if len(s) != 5 || s[2] != ':' {
		return 0, bad
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, bad
		}
	}
	h, m := int(s[0]-'0')*10+int(s[1]-'0'), int(s[3]-'0')*10+int(s[4]-'0')
	if m > 59 || h > 24 || (h == 24 && m != 0) {
		return 0, bad
	}
	return TimeOfDay(h*60 + m), nil
}

func (t TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", int(t)/60, int(t)%60) }

// Window is a daily span of local time, [From, To). To before From runs past midnight into the
// next day. AllDay is 00:00 to 24:00.
type Window struct {
	From, To TimeOfDay
}

// AllDay is the whole day.
var AllDay = Window{0, Midnight}

// ErrWindow means a window is empty or out of range.
var ErrWindow = errors.New("day: a window runs from 00:00-23:59 to a different time, up to 24:00")

// Valid checks the window: From before 24:00, To after 00:00, and not empty.
func (w Window) Valid() error {
	if w.From < 0 || w.From >= Midnight || w.To <= 0 || w.To > Midnight || w.From == w.To {
		return ErrWindow
	}
	return nil
}

// On is the window's span that starts on date d. When To is before From, it ends the next day.
func (w Window) On(d Date, loc *time.Location) (start, end time.Time) {
	start = d.At(w.From, loc)
	if w.To > w.From {
		return start, d.At(w.To, loc)
	}
	return start, d.AddDays(1).At(w.To, loc)
}

// Contains reports whether t is inside the window, including a span that began the day before.
func (w Window) Contains(t time.Time, loc *time.Location) bool {
	today := Of(t, loc)
	for _, d := range []Date{today.AddDays(-1), today} {
		s, e := w.On(d, loc)
		if !t.Before(s) && t.Before(e) {
			return true
		}
	}
	return false
}
