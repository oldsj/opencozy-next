package day

import (
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := Zone(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func tod(t *testing.T, s string) TimeOfDay {
	t.Helper()
	v, err := ParseTimeOfDay(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The US and EU transitions of 2026, plus a zone whose spring-forward gap is at midnight.
func TestAtAcrossDST(t *testing.T) {
	cases := []struct {
		zone, date, time string
		want             string // RFC 3339, in the zone's offset
	}{
		// US spring forward, 2026-03-08: 02:00 EST becomes 03:00 EDT.
		{"America/New_York", "2026-03-08", "01:59", "2026-03-08T01:59:00-05:00"},
		{"America/New_York", "2026-03-08", "02:00", "2026-03-08T03:00:00-04:00"}, // skipped: offset from before
		{"America/New_York", "2026-03-08", "02:30", "2026-03-08T03:30:00-04:00"},
		{"America/New_York", "2026-03-08", "03:00", "2026-03-08T03:00:00-04:00"},
		// US fall back, 2026-11-01: 01:00-01:59 happens twice; the first is EDT.
		{"America/New_York", "2026-11-01", "00:59", "2026-11-01T00:59:00-04:00"},
		{"America/New_York", "2026-11-01", "01:30", "2026-11-01T01:30:00-04:00"},
		{"America/New_York", "2026-11-01", "02:00", "2026-11-01T02:00:00-05:00"},
		// EU spring forward, 2026-03-29: 02:00 CET becomes 03:00 CEST.
		{"Europe/Berlin", "2026-03-29", "02:30", "2026-03-29T03:30:00+02:00"},
		{"Europe/Berlin", "2026-03-29", "03:00", "2026-03-29T03:00:00+02:00"},
		// EU fall back, 2026-10-25: 02:00-02:59 happens twice; the first is CEST.
		{"Europe/Berlin", "2026-10-25", "02:30", "2026-10-25T02:30:00+02:00"},
		{"Europe/Berlin", "2026-10-25", "03:00", "2026-10-25T03:00:00+01:00"},
		{"Europe/London", "2026-03-29", "01:30", "2026-03-29T02:30:00+01:00"},
		{"Europe/London", "2026-10-25", "01:30", "2026-10-25T01:30:00+01:00"},
		// Chile springs forward at midnight, 2026-09-06: the day starts at 01:00.
		{"America/Santiago", "2026-09-06", "00:00", "2026-09-06T01:00:00-03:00"},
		{"America/Santiago", "2026-09-05", "24:00", "2026-09-06T01:00:00-03:00"},
		// No DST.
		{"Asia/Tokyo", "2026-03-08", "02:30", "2026-03-08T02:30:00+09:00"},
		{"UTC", "2026-03-08", "24:00", "2026-03-09T00:00:00Z"},
	}
	for _, c := range cases {
		loc := zone(t, c.zone)
		d, err := ParseDate(c.date)
		if err != nil {
			t.Fatal(err)
		}
		got := d.At(tod(t, c.time), loc).In(loc).Format(time.RFC3339)
		if got != c.want {
			t.Errorf("%s %s %s: got %s, want %s", c.zone, c.date, c.time, got, c.want)
		}
	}
}

func TestDayLength(t *testing.T) {
	cases := []struct {
		zone, date string
		hours      float64
	}{
		{"America/New_York", "2026-03-08", 23},
		{"America/New_York", "2026-03-09", 24},
		{"America/New_York", "2026-11-01", 25},
		{"Europe/Berlin", "2026-03-29", 23},
		{"Europe/Berlin", "2026-10-25", 25},
		{"America/Santiago", "2026-09-06", 23},
	}
	for _, c := range cases {
		loc := zone(t, c.zone)
		d, _ := ParseDate(c.date)
		if got := d.End(loc).Sub(d.Start(loc)).Hours(); got != c.hours {
			t.Errorf("%s %s: %v hours, want %v", c.zone, c.date, got, c.hours)
		}
		// Every instant of the day is on that day, and the next instant is not.
		if Of(d.Start(loc), loc) != d || Of(d.End(loc).Add(-time.Nanosecond), loc) != d || Of(d.End(loc), loc) == d {
			t.Errorf("%s %s: Start/End disagree with Of", c.zone, c.date)
		}
	}
}

func TestOf(t *testing.T) {
	ny := zone(t, "America/New_York")
	// 03:30 UTC on the 9th is 23:30 on the 8th in New York.
	if got := Of(time.Date(2026, 3, 9, 3, 30, 0, 0, time.UTC), ny); got.String() != "2026-03-08" {
		t.Fatalf("got %s", got)
	}
	d, _ := ParseDate("2026-12-31")
	if d.AddDays(1).String() != "2027-01-01" || d.AddDays(-365).String() != "2025-12-31" || d.Weekday() != time.Thursday {
		t.Fatal("date arithmetic")
	}
	if d.Compare(d.AddDays(1)) != -1 || d.Compare(d) != 0 {
		t.Fatal("compare")
	}
}

func TestWindowContains(t *testing.T) {
	ny := zone(t, "America/New_York")
	at := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	evening := Window{tod(t, "19:00"), tod(t, "21:00")}
	overnight := Window{tod(t, "22:00"), tod(t, "06:00")}
	toMidnight := Window{tod(t, "18:00"), Midnight}
	cases := []struct {
		w    Window
		t    string
		want bool
	}{
		{evening, "2026-03-07T19:00:00-05:00", true},
		{evening, "2026-03-07T21:00:00-05:00", false}, // the end is outside
		{evening, "2026-03-07T18:59:59-05:00", false},
		{overnight, "2026-03-07T23:00:00-05:00", true},
		{overnight, "2026-03-08T05:59:00-04:00", true}, // across the spring-forward night
		{overnight, "2026-03-08T06:00:00-04:00", false},
		{overnight, "2026-03-08T12:00:00-04:00", false},
		{overnight, "2026-11-01T01:30:00-05:00", true}, // the second 01:30 is still inside
		{toMidnight, "2026-03-07T23:59:59-05:00", true},
		{toMidnight, "2026-03-08T00:00:00-05:00", false},
		{AllDay, "2026-11-01T01:30:00-05:00", true},
	}
	for _, c := range cases {
		if got := c.w.Contains(at(c.t), ny); got != c.want {
			t.Errorf("%v-%v at %s: got %v", c.w.From, c.w.To, c.t, got)
		}
	}
	// The overnight window across fall-back is 9 hours, across spring-forward 7.
	fall, _ := ParseDate("2026-10-31")
	if s, e := overnight.On(fall, ny); e.Sub(s) != 9*time.Hour {
		t.Errorf("fall-back night: %v", e.Sub(s))
	}
	spring, _ := ParseDate("2026-03-07")
	if s, e := overnight.On(spring, ny); e.Sub(s) != 7*time.Hour {
		t.Errorf("spring-forward night: %v", e.Sub(s))
	}
}

func TestParsing(t *testing.T) {
	for _, s := range []string{"", "7:30", "07:60", "24:01", "25:00", "+1:00", "07-30", "0a:00"} {
		if _, err := ParseTimeOfDay(s); err == nil {
			t.Errorf("ParseTimeOfDay(%q) accepted", s)
		}
	}
	for _, s := range []string{"00:00", "23:59", "24:00"} {
		if v, err := ParseTimeOfDay(s); err != nil || v.String() != s {
			t.Errorf("ParseTimeOfDay(%q) = %v, %v", s, v, err)
		}
	}
	for _, s := range []string{"", "Local", "Mars/Olympus", "../etc"} {
		if _, err := Zone(s); err == nil {
			t.Errorf("Zone(%q) accepted", s)
		}
	}
	for _, w := range []Window{{0, 0}, {60, 60}, {Midnight, 60}, {0, Midnight + 1}, {-1, 60}} {
		if w.Valid() == nil {
			t.Errorf("window %v accepted", w)
		}
	}
	if AllDay.Valid() != nil || (Window{22 * 60, 6 * 60}).Valid() != nil {
		t.Error("valid windows refused")
	}
	if _, err := ParseDate("2026-02-30"); err == nil {
		t.Error("2026-02-30 accepted")
	}
}
