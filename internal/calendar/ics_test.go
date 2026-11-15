package calendar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oldsj/opencozy-next/internal/day"
)

func ny(t *testing.T) *time.Location {
	t.Helper()
	loc, err := day.Zone("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func cal(events ...string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//example.test//t//EN\r\n" + strings.Join(events, "") + "END:VCALENDAR\r\n")
}

func ev(lines ...string) string {
	return "BEGIN:VEVENT\r\nDTSTAMP:20260101T000000Z\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VEVENT\r\n"
}

var today = day.Date{Year: 2026, Month: time.September, Day: 14}

func parse(t *testing.T, body []byte) Feed {
	t.Helper()
	f, err := Parse(t.Context(), body, ny(t), WindowAround(today))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func keys(s Series) string {
	var out []string
	for _, e := range s.Events {
		out = append(out, e.Key)
	}
	return strings.Join(out, ",")
}

func TestSingleAndAllDay(t *testing.T) {
	f := parse(t, cal(
		ev("UID:a@example.test", "DTSTART;TZID=America/New_York:20260914T153000", "DTEND;TZID=America/New_York:20260914T163000",
			"SUMMARY:Swimming lesson", "LOCATION:Riverside Pool\\, lane 3", "DESCRIPTION:Bring goggles.\\nTowel too."),
		ev("UID:b@example.test", "DTSTART;VALUE=DATE:20260915", "DTEND;VALUE=DATE:20260917", "SUMMARY:Camp", "STATUS:TENTATIVE"),
		ev("UID:c@example.test", "DTSTART;VALUE=DATE:20260916", "SUMMARY:Picture day"), // no end: one day
		ev("UID:d@example.test", "DTSTART:20260914T120000Z", "DURATION:PT45M"),
	))
	if len(f.Series) != 4 || f.Skipped != 0 {
		t.Fatalf("%+v", f)
	}
	a := f.Series[0].Events[0]
	if a.Key != "single" || a.AllDay || a.Start.UTC().Format(time.RFC3339) != "2026-09-14T19:30:00Z" || a.End.Sub(a.Start) != time.Hour ||
		a.Title != "Swimming lesson" || a.Location != "Riverside Pool, lane 3" || a.Description != "Bring goggles.\nTowel too." {
		t.Errorf("a: %+v", a)
	}
	b := f.Series[1].Events[0]
	if !b.AllDay || b.StartDate.String() != "2026-09-15" || b.EndDate.String() != "2026-09-17" || !b.Tentative {
		t.Errorf("b: %+v", b)
	}
	if c := f.Series[2].Events[0]; c.EndDate.String() != "2026-09-17" {
		t.Errorf("c: %+v", c)
	}
	if d := f.Series[3].Events[0]; d.End.Sub(d.Start) != 45*time.Minute || d.Title != "" {
		t.Errorf("d: %+v", d)
	}
}

// A weekly event keeps its wall-clock time across the DST change, honours EXDATE lists and
// overrides, and drops a cancelled instance.
func TestRecurrence(t *testing.T) {
	f := parse(t, cal(
		ev("UID:w@example.test", "DTSTART;TZID=America/New_York:20261021T170000", "DTEND;TZID=America/New_York:20261021T180000",
			"RRULE:FREQ=WEEKLY;COUNT=6", "EXDATE;TZID=America/New_York:20261028T170000,20261104T170000", "SUMMARY:Practice"),
		ev("UID:w@example.test", "RECURRENCE-ID;TZID=America/New_York:20261111T170000",
			"DTSTART;TZID=America/New_York:20261111T183000", "DTEND;TZID=America/New_York:20261111T193000", "SUMMARY:Practice (late)"),
		ev("UID:w@example.test", "RECURRENCE-ID;TZID=America/New_York:20261118T170000",
			"DTSTART;TZID=America/New_York:20261118T170000", "STATUS:CANCELLED"),
	))
	if len(f.Series) != 1 || !f.Series[0].Recurring {
		t.Fatalf("%+v", f)
	}
	s := f.Series[0]
	// 10/21 (EDT, 21:00Z), 11/11 overridden, 11/25 (EST, 22:00Z); 10/28 and 11/04 excluded, 11/18 cancelled.
	if got := keys(s); got != "20261021T210000Z,20261111T220000Z,20261125T220000Z" {
		t.Fatalf("keys %s", got)
	}
	ny := ny(t)
	for _, e := range s.Events {
		if h := e.Start.In(ny).Hour(); h != 17 && (e.Key != "20261111T220000Z" || h != 18) {
			t.Errorf("%s starts at %d:00 local", e, h)
		}
	}
	if o := s.Events[1]; o.Title != "Practice (late)" || o.Start.In(ny).Minute() != 30 {
		t.Errorf("override %s", o)
	}
}

func TestAllDayRecurrenceAndWindow(t *testing.T) {
	f := parse(t, cal(
		// Daily forever from long ago: only the window is stored.
		ev("UID:daily@example.test", "DTSTART;VALUE=DATE:20200101", "RRULE:FREQ=DAILY", "SUMMARY:Daily"),
		// Yearly until a date (inclusive).
		ev("UID:bday@example.test", "DTSTART;VALUE=DATE:20250916", "RRULE:FREQ=YEARLY;UNTIL=20260916", "SUMMARY:Birthday"),
		// Entirely before the window: listed with no events, so its row and assignments stay.
		ev("UID:old@example.test", "DTSTART:20200101T100000Z", "DTEND:20200101T110000Z"),
	))
	if len(f.Series) != 3 || f.Series[2].UID != "old@example.test" || len(f.Series[2].Events) != 0 {
		t.Fatalf("%+v", f.Series[2])
	}
	d := f.Series[0]
	if n := len(d.Events); n != WindowPast+WindowFuture {
		t.Errorf("daily: %d events", n)
	}
	if first := d.Events[0]; first.StartDate != today.AddDays(-WindowPast) || first.Key != strings.ReplaceAll(first.StartDate.String(), "-", "") {
		t.Errorf("first %s", first)
	}
	if got := keys(f.Series[1]); got != "20260916" {
		t.Errorf("birthday %s", got)
	}
}

func TestCapsAndHostileInput(t *testing.T) {
	// More events than the cap.
	var many []string
	for i := 0; i < MaxEvents+10; i++ {
		many = append(many, ev(fmt.Sprintf("UID:%d@example.test", i), "DTSTART:20260914T120000Z", "DTEND:20260914T130000Z"))
	}
	// Every series is still listed (their assignments stay); only the events stop at the cap.
	if f := parse(t, cal(many...)); !f.TooMany || len(f.Series) != MaxEvents+10 || len(f.Series[MaxEvents].Events) != 0 {
		t.Errorf("cap: too many %v, %d series", f.TooMany, len(f.Series))
	}
	// A feed whose rules together need more work than one feed may is refused, not cut short.
	var heavy []string
	for i := 0; i < 12; i++ {
		heavy = append(heavy, ev(fmt.Sprintf("UID:heavy%d@example.test", i), "DTSTART:19000101T000000Z", "RRULE:FREQ=HOURLY"))
	}
	if _, err := Parse(t.Context(), cal(heavy...), ny(t), WindowAround(today)); !errors.Is(err, ErrTooComplex) {
		t.Errorf("heavy feed: %v", err)
	}
	// So is one whose deadline has passed.
	done, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Parse(done, cal(heavy[0]), ny(t), WindowAround(today)); !errors.Is(err, ErrTooComplex) {
		t.Errorf("cancelled: %v", err)
	}
	// Rules that would never end within the budget, or step by the second, are cut off or skipped.
	start := time.Now()
	f := parse(t, cal(
		ev("UID:s@example.test", "DTSTART:19000101T000000Z", "RRULE:FREQ=SECONDLY"),
		ev("UID:h@example.test", "DTSTART:19000101T000000Z", "RRULE:FREQ=HOURLY"),
		ev("UID:nouid-follows", "DTSTART:bad"),
		ev("DTSTART:20260914T120000Z"),
		ev("UID:tz@example.test", "DTSTART;TZID=Eastern Standard Time:20260914T090000", "DTEND;TZID=/mozilla.org/20070129_1/Europe/Paris:20260914T180000"),
	))
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	if f.Skipped != 3 {
		t.Errorf("skipped %d", f.Skipped)
	}
	for _, s := range f.Series {
		if s.UID == "tz@example.test" {
			e := s.Events[0]
			// 09:00 in the household zone (Windows name unknown), 18:00 Paris = 16:00Z.
			if e.Start.UTC().Format(time.RFC3339) != "2026-09-14T13:00:00Z" || e.End.UTC().Format(time.RFC3339) != "2026-09-14T16:00:00Z" {
				t.Errorf("tz: %s", e)
			}
		}
	}
	// Not a calendar, or broken.
	for _, body := range []string{"", "<html>nope</html>", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nX;=\"\r\n", "BEGIN:VCALENDAR\r\n;;;\r\n"} {
		if _, err := Parse(t.Context(), []byte(body), ny(t), WindowAround(today)); err == nil {
			t.Errorf("%q: parsed", body)
		}
	}
	// Control characters and long text are cleaned.
	long := strings.Repeat("é", 600)
	f = parse(t, cal(ev("UID:x@example.test", "DTSTART:20260914T120000Z", "SUMMARY:a\x07b\\nc", "LOCATION:"+long)))
	if e := f.Series[0].Events[0]; e.Title != "ab c" || len([]rune(e.Location)) != 500 {
		t.Errorf("%q %d", e.Title, len([]rune(e.Location)))
	}
}
