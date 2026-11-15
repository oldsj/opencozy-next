package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/keyring"
)

func withKeys(t *testing.T) func(*config.Config) {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	kr, err := keyring.Parse("k1:" + base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return func(c *config.Config) { c.SecretKeys = kr }
}

func TestCalendarLinks(t *testing.T) {
	e := newEnv(t, withKeys(t))
	h := e.house("a", nil)
	adult := h.actors["adult"]
	link := map[string]any{"name": " Sports ", "url": "webcal://calendar.example.test/sports.ics?token=fictional", "color": "peach", "showOnDisplay": false}
	r := e.send(adult, "POST", "/api/calendars", link)
	if r.status != 201 || r.body["name"] != "Sports" || r.body["provider"] != "ics" || r.body["status"] != "ok" || r.body["lastSyncAt"] != nil {
		t.Fatalf("add: %d %s", r.status, r.raw)
	}
	if strings.Contains(string(r.raw), "calendar.example.test") {
		t.Fatalf("the link came back: %s", r.raw)
	}
	id := r.body["id"].(string)
	// The same link again (https spelling) is refused; a bad one too.
	link["url"] = "https://calendar.example.test/sports.ics?token=fictional"
	if r := e.send(adult, "POST", "/api/calendars", link); r.status != 409 || r.code() != "calendar_exists" {
		t.Fatalf("duplicate: %d %s", r.status, r.raw)
	}
	for _, bad := range []string{"ftp://calendar.example.test/a.ics", "https://me:pw@calendar.example.test/a.ics", "not a link"} {
		link["url"] = bad
		if r := e.send(adult, "POST", "/api/calendars", link); r.status != 422 || r.code() != "invalid_link" {
			t.Errorf("%s: %d %s", bad, r.status, r.raw)
		}
	}
	// Stored encrypted, with no plaintext anywhere in the row.
	var stored string
	if err := e.owner.QueryRow(t.Context(), `SELECT row_to_json(s)::text FROM calendar_source s WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "example.test") || strings.Contains(stored, "fictional") || !strings.Contains(stored, `"feed_url":"v1.k1.`) {
		t.Fatalf("stored: %s", stored)
	}
	list := e.send(adult, "GET", "/api/calendars", nil)
	if n := len(list.body["calendars"].([]any)); n != 2 || strings.Contains(string(list.raw), "example.test") { // plus the fixture's
		t.Fatalf("list: %s", list.raw)
	}
	edit := map[string]any{"name": "Football", "color": "sage", "showOnDisplay": true, "expectedVersion": 1}
	if r := e.send(adult, "PATCH", "/api/calendars/"+id, edit); r.status != 200 || r.body["version"] != float64(2) || r.body["name"] != "Football" {
		t.Fatalf("edit: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PATCH", "/api/calendars/"+id, edit); r.status != 409 {
		t.Fatalf("stale edit: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "DELETE", "/api/calendars/"+id+"?expectedVersion=1", nil); r.status != 409 {
		t.Fatalf("stale delete: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "DELETE", "/api/calendars/"+id+"?expectedVersion=2", nil); r.status != 204 {
		t.Fatalf("delete: %d %s", r.status, r.raw)
	}
	other := e.house("b", nil)
	if r := e.send(other.actors["adult"], "PATCH", "/api/calendars/"+h.calendar.String(), edit); r.status != 404 {
		t.Fatalf("other household: %d", r.status)
	}
	// Without secret keys, links can't be stored.
	bare := newEnv(t, nil)
	bh := bare.house("c", nil)
	link["url"] = "https://calendar.example.test/x.ics"
	if r := bare.send(bh.actors["adult"], "POST", "/api/calendars", link); r.status != 422 || r.code() != "not_configured" {
		t.Fatalf("no keys: %d %s", r.status, r.raw)
	}
}

func TestCalendarEventsAndAssignments(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult, display := h.actors["adult"], h.actors["display-calendar"]
	// A hidden calendar with a weekly timed event; the fixture's "School" is shown on displays.
	hidden := e.id(`INSERT INTO calendar_source (household_id, provider, name, color, show_on_display, feed_url, feed_url_mac)
		VALUES ($1, 'ics', 'Work', 'teal', false, 'v1.k1.x.x', 'h1.k1.hidden') RETURNING id`, h.id)
	series := e.id(`INSERT INTO calendar_series (household_id, source_id, uid, recurring) VALUES ($1, $2, 'w@example.test', true) RETURNING id`, h.id, hidden)
	var ids []string
	for _, k := range []string{"20260914T193000Z", "20260921T193000Z"} {
		start := k[:4] + "-" + k[4:6] + "-" + k[6:8] + " 19:30Z"
		ids = append(ids, e.id(`INSERT INTO calendar_event (household_id, source_id, series_id, recurrence_key, all_day, starts_at, ends_at, title, description, status)
			VALUES ($1, $2, $3, $4, false, $5::timestamptz, $5::timestamptz + interval '1 hour', 'Swim', 'Bring goggles.', 'tentative') RETURNING id`,
			h.id, hidden, series, k, start).String())
	}
	week := "/api/calendar?from=2026-09-14&to=2026-09-21"
	r := e.send(adult, "GET", week, nil)
	evs := r.body["events"].([]any)
	if r.status != 200 || len(evs) != 2 {
		t.Fatalf("adult week: %d %s", r.status, r.raw)
	}
	swim := evs[0].(map[string]any)
	// RFC3339 in the household's zone (the fixture's), the same instant.
	start, err := time.Parse(time.RFC3339, swim["start"].(string))
	if err != nil || !start.Equal(time.Date(2026, 9, 14, 19, 30, 0, 0, time.UTC)) || strings.HasSuffix(swim["start"].(string), "Z") ||
		swim["status"] != "tentative" || swim["description"] != nil {
		t.Fatalf("swim: %v", swim)
	}
	if pic := evs[1].(map[string]any); pic["allDay"] != true || pic["start"] != "2026-09-16" || pic["end"] != "2026-09-17" {
		t.Fatalf("picture day: %v", pic)
	}
	if r := e.send(display, "GET", week, nil); len(r.body["events"].([]any)) != 1 {
		t.Fatalf("display sees the hidden calendar: %s", r.raw)
	}
	for _, q := range []string{"from=2026-09-14&to=2026-09-14", "from=2026-09-14&to=2026-10-27", "from=2026-09-21&to=2026-09-14"} {
		if r := e.send(adult, "GET", "/api/calendar?"+q, nil); r.status != 400 {
			t.Errorf("%s: %d", q, r.status)
		}
	}
	if r := e.send(adult, "GET", "/api/calendar?from=2026-09-14&to=2026-10-26", nil); r.status != 200 {
		t.Errorf("42 days: %d %s", r.status, r.raw)
	}
	// Detail carries the description; displays can't ask for it.
	if r := e.send(adult, "GET", "/api/calendar/events/"+ids[0], nil); r.status != 200 || r.body["description"] != "Bring goggles." || r.body["recurring"] != true {
		t.Fatalf("detail: %d %s", r.status, r.raw)
	}
	if r := e.send(display, "GET", "/api/calendar/events/"+ids[0], nil); r.status != 403 {
		t.Fatalf("display detail: %d", r.status)
	}
	// The series goes to child1; the second week to child2 only; then the series to nobody,
	// which clears the per-week choice.
	children := func(id string) string {
		t.Helper()
		r := e.send(adult, "GET", "/api/calendar/events/"+id, nil)
		return fmt.Sprint(r.body["children"])
	}
	c1, c2 := h.child1.String(), h.child2.String()
	if r := e.send(adult, "PUT", "/api/calendar/events/"+ids[0]+"/children", map[string]any{"children": []string{c1}, "scope": "series"}); r.status != 200 {
		t.Fatalf("assign series: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", "/api/calendar/events/"+ids[1]+"/children", map[string]any{"children": []string{c2}, "scope": "occurrence"}); r.status != 200 || fmt.Sprint(r.body["children"]) != "["+c2+"]" {
		t.Fatalf("assign occurrence: %d %s", r.status, r.raw)
	}
	if a, b := children(ids[0]), children(ids[1]); a != "["+c1+"]" || b != "["+c2+"]" {
		t.Fatalf("after: %s %s", a, b)
	}
	if r := e.send(adult, "PUT", "/api/calendar/events/"+ids[0]+"/children", map[string]any{"children": []string{}, "scope": "series"}); r.status != 200 {
		t.Fatalf("clear: %d", r.status)
	}
	if a, b := children(ids[0]), children(ids[1]); a != "[]" || b != "[]" {
		t.Fatalf("cleared: %s %s", a, b)
	}
	// Another household's child, or a repeated one, is refused.
	other := e.house("b", nil)
	if r := e.send(adult, "PUT", "/api/calendar/events/"+ids[0]+"/children", map[string]any{"children": []string{other.child1.String()}, "scope": "series"}); r.status != 422 {
		t.Fatalf("other child: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", "/api/calendar/events/"+ids[0]+"/children", map[string]any{"children": []string{c1, c1}, "scope": "series"}); r.status != 400 {
		t.Fatalf("repeated child: %d %s", r.status, r.raw)
	}
	if r := e.send(other.actors["adult"], "GET", "/api/calendar/events/"+ids[0], nil); r.status != 404 {
		t.Fatalf("other household's event: %d", r.status)
	}
}
