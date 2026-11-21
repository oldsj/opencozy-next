package day

import (
	"testing"
	"time"
)

func TestAllowedHours(t *testing.T) {
	ny, err := Zone("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	h := AllowedHours{SchoolNight: Window{15*60 + 30, 19*60 + 30}, Weekend: Window{8 * 60, 20 * 60}}
	if err := h.Valid(); err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, ny)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name, now      string
		open           bool
		start, end, op string
	}{
		{"school night, inside", "2026-11-16 16:00", true, "2026-11-16 15:30", "2026-11-16 19:30", ""},
		{"school night, before", "2026-11-16 09:00", false, "", "", "2026-11-16 15:30"},
		{"thursday after, friday is weekend", "2026-11-19 20:00", false, "", "", "2026-11-20 08:00"},
		{"friday late is weekend", "2026-11-20 19:59", true, "2026-11-20 08:00", "2026-11-20 20:00", ""},
		{"saturday after, sunday is a school night", "2026-11-21 21:00", false, "", "", "2026-11-22 15:30"},
		{"sunday is a school night", "2026-11-22 18:00", true, "2026-11-22 15:30", "2026-11-22 19:30", ""},
		{"end is exclusive", "2026-11-16 19:30", false, "", "", "2026-11-17 15:30"},
	}
	for _, c := range cases {
		got := h.At(at(c.now), ny)
		if got.Open != c.open {
			t.Fatalf("%s: open = %v", c.name, got.Open)
		}
		if c.open && (!got.Start.Equal(at(c.start)) || !got.End.Equal(at(c.end))) {
			t.Fatalf("%s: %v–%v", c.name, got.Start, got.End)
		}
		if !c.open && !got.OpensAt.Equal(at(c.op)) {
			t.Fatalf("%s: opens %v", c.name, got.OpensAt)
		}
	}

	// Spring forward (2026-03-08, a Sunday): a window starting at 02:30 opens at 03:30 EDT.
	early := AllowedHours{SchoolNight: Window{2*60 + 30, 4 * 60}, Weekend: Window{2*60 + 30, 4 * 60}}
	if got := early.At(at("2026-03-08 01:00"), ny); got.Open || !got.OpensAt.Equal(time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("spring forward: %+v", got)
	}

	// All day: the end of today's hours while ahead, otherwise midnight; midnight with no rule.
	if got := AllDayEnd(&h, at("2026-11-16 16:00"), ny); !got.Equal(at("2026-11-16 19:30")) {
		t.Fatalf("all day inside: %v", got)
	}
	if got := AllDayEnd(&h, at("2026-11-16 20:00"), ny); !got.Equal(at("2026-11-17 00:00")) {
		t.Fatalf("all day after: %v", got)
	}
	if got := AllDayEnd(nil, at("2026-11-16 09:00"), ny); !got.Equal(at("2026-11-17 00:00")) {
		t.Fatalf("all day no rule: %v", got)
	}
	// Fall back (2026-11-01): the day is 25 hours, so midnight is 25 h after the start.
	if got := AllDayEnd(nil, at("2026-11-01 00:00"), ny); got.Sub(at("2026-11-01 00:00")) != 25*time.Hour {
		t.Fatalf("all day fall back: %v", got)
	}

	for _, bad := range []AllowedHours{
		{SchoolNight: Window{600, 600}, Weekend: Window{0, 60}},
		{SchoolNight: Window{600, 500}, Weekend: Window{0, 60}},
		{SchoolNight: Window{0, Midnight}, Weekend: Window{0, 60}},
	} {
		if bad.Valid() == nil {
			t.Fatalf("%+v: want invalid", bad)
		}
	}
}
