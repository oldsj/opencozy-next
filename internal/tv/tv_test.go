package tv

import (
	"testing"
	"time"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/day"
)

func mustLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func hhmm(t *testing.T, s string) day.TimeOfDay {
	t.Helper()
	v, err := day.ParseTimeOfDay(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenHoursAcrossMidnightAndDST(t *testing.T) {
	loc := mustLoc(t)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	nights := OpenHours{Start: hhmm(t, "21:00"), End: hhmm(t, "07:00")}
	for _, c := range []struct {
		name   string
		t      string
		open   bool
		edge   string // ClosesAt when open, else OpensAt
		length time.Duration
	}{
		{"before", "2026-09-15 20:59", false, "2026-09-15 21:00", 0},
		{"start is inclusive", "2026-09-15 21:00", true, "2026-09-16 07:00", 10 * time.Hour},
		{"after midnight", "2026-09-16 03:00", true, "2026-09-16 07:00", 10 * time.Hour},
		{"end is exclusive", "2026-09-16 07:00", false, "2026-09-16 21:00", 0},
		// Clocks spring forward: the night is an hour shorter; and fall back: an hour longer.
		{"spring forward", "2026-03-08 06:30", true, "2026-03-08 07:00", 9 * time.Hour},
		{"fall back", "2026-11-01 01:30", true, "2026-11-01 07:00", 11 * time.Hour},
	} {
		s := nights.Status(at(c.t), loc)
		if s.Open != c.open {
			t.Errorf("%s: open %v", c.name, s.Open)
			continue
		}
		edge := s.OpensAt
		if s.Open {
			edge = s.ClosesAt
			if got := s.ClosesAt.Sub(s.OpenedAt); got != c.length {
				t.Errorf("%s: a %v night", c.name, got)
			}
		}
		if !edge.Equal(at(c.edge)) {
			t.Errorf("%s: edge %v, want %s", c.name, edge, c.edge)
		}
	}

	// Friday and Saturday nights only: Sunday morning is still Saturday's night.
	weekend := nights
	weekend.Days = 1<<time.Friday | 1<<time.Saturday
	if s := weekend.Status(at("2026-09-20 06:00"), loc); !s.Open { // a Sunday
		t.Error("Saturday's night closed on Sunday morning")
	}
	if s := weekend.Status(at("2026-09-20 22:00"), loc); s.Open || !s.OpensAt.Equal(at("2026-09-25 21:00")) {
		t.Errorf("Sunday night: %+v", s)
	}
}

func TestStateAtJoinsOverrideAndHours(t *testing.T) {
	loc := mustLoc(t)
	now := time.Date(2026, 9, 15, 20, 0, 0, 0, loc)
	hours := &OpenHours{Start: hhmm(t, "21:00"), End: hhmm(t, "07:00")}
	until := now.Add(time.Hour) // the override ends as the hours begin: one run
	st := StateAt(hours, &until, now, loc)
	if !st.Open || !st.OverrideUntil.Equal(until) || !st.ClosesAt.Equal(time.Date(2026, 9, 16, 7, 0, 0, 0, loc)) {
		t.Errorf("joined: %+v", st)
	}
	past := now.Add(-time.Minute)
	if st := StateAt(hours, &past, now, loc); st.Open || !st.OverrideUntil.IsZero() {
		t.Errorf("expired override: %+v", st)
	}
	if st := StateAt(nil, nil, now, loc); st.Open || st.Hours != nil {
		t.Errorf("gated: %+v", st)
	}
	if end := OverrideEnd(OpenAllDay, now, loc); !end.Equal(time.Date(2026, 9, 16, 0, 0, 0, 0, loc)) {
		t.Errorf("all day: %v", end)
	}
}

func TestParseOpenHours(t *testing.T) {
	if h, err := ParseOpenHours(nil); h != nil || err != nil {
		t.Errorf("none: %v %v", h, err)
	}
	all := []api.Weekday{api.Sun, api.Mon, api.Tue, api.Wed, api.Thu, api.Fri, api.Sat}
	h, err := ParseOpenHours(&api.OpenHours{Start: "21:00", End: "07:00", Days: &all})
	if err != nil || h.Days != 0 {
		t.Errorf("every day: %+v %v", h, err)
	}
	some := []api.Weekday{api.Fri, api.Sat}
	if h, err := ParseOpenHours(&api.OpenHours{Start: "21:00", End: "07:00", Days: &some}); err != nil || h.Days != 0x60 {
		t.Errorf("weekend: %+v %v", h, err)
	}
	if v := OpenHoursView(h); v == nil || v.Days != nil {
		t.Errorf("every day reads as no days: %+v", v)
	}
	for _, bad := range []api.OpenHours{{Start: "09:00", End: "09:00"}, {Start: "24:00", End: "07:00"}, {Start: "9", End: "07:00"}} {
		if _, err := ParseOpenHours(&bad); err == nil {
			t.Errorf("%+v: no error", bad)
		}
	}
}

func TestPowerFromHomeAssistant(t *testing.T) {
	now := time.Date(2026, 9, 15, 20, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		state   string
		changed time.Duration // ago; 0 is not known
		want    Power
		ok      bool
	}{
		{"off", 0, PowerOff, true},
		{"standby", 0, PowerOff, true},
		{"playing", time.Hour, PowerOn, true},
		{"on", 0, PowerOn, true},
		{"paused", 0, PowerOn, true},
		{"paused", StalePause, PowerOn, true},
		{"paused", StalePause + time.Second, PowerUnknown, true}, // a stale pause is not "off"
		{"idle", time.Hour, PowerUnknown, true},
		{"unavailable", 0, "", false},
		{"unknown", 0, "", false},
		{"something new", 0, "", false},
	} {
		var changed time.Time
		if c.changed > 0 {
			changed = now.Add(-c.changed)
		}
		if p, ok := PowerFromHomeAssistant(c.state, changed, now); p != c.want || ok != c.ok {
			t.Errorf("%s %v ago: %q %v", c.state, c.changed, p, ok)
		}
	}
}

func TestRouterSeconds(t *testing.T) {
	for remaining, want := range map[time.Duration]int{
		0:                                0,
		EarlyCut:                         0,
		EarlyCut + time.Second:           1,
		10 * time.Minute:                 300,
		AllowanceCap + EarlyCut:          300,
		AllowanceCap + EarlyCut - 1500e6: 298, // whole seconds, rounded down
	} {
		if got := RouterSeconds(remaining); got != want {
			t.Errorf("%v: %d, want %d", remaining, got, want)
		}
	}
	now := time.Now()
	if got := OpenSeconds(now.Add(time.Hour), now); got != 300 {
		t.Errorf("open: %d", got)
	}
	if got := OpenSeconds(now.Add(30*time.Second), now); got != 30 {
		t.Errorf("open, closing: %d", got)
	}
}

func TestNormalizeMAC(t *testing.T) {
	if got := NormalizeMAC("02-00-5E-10-00-0A"); got != "02:00:5e:10:00:0a" {
		t.Error(got)
	}
}
