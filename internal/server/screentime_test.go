package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/clock"
)

// iPad time end to end: bought, spent within allowed hours, given by a parent, ended early,
// refunded on cancel, asked for and answered.
func TestScreenTime(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	other := e.house("b", nil)
	adult, device := h.actors["adult"], h.actors["device"]
	ams, _ := time.LoadLocation("Europe/Amsterdam")
	// A Wednesday, a school night, at 18:45 in the household's zone.
	clk := clock.NewFake(time.Date(2026, 11, 18, 18, 45, 0, 0, ams))
	e.srv.runner.Clock = clk
	at := func(hh, mm int) string { return time.Date(2026, 11, 18, hh, mm, 0, 0, ams).UTC().Format(time.RFC3339) }
	child1 := "/api/children/" + h.child1.String()
	child2 := "/api/children/" + h.child2.String()
	ok := func(r resp, what string) resp {
		t.Helper()
		if r.status != 200 && r.status != 201 {
			t.Fatalf("%s: %d %s", what, r.status, r.raw)
		}
		return r
	}
	refused := func(r resp, status int, code, what string) {
		t.Helper()
		if r.status != status || r.code() != code {
			t.Errorf("%s: %d %s, want %d %s", what, r.status, r.raw, status, code)
		}
	}
	get := func(c cred, child string) map[string]any {
		t.Helper()
		return ok(e.send(c, "GET", child+"/screen-time", nil), "get screen time").body
	}
	num := func(m map[string]any, k string) float64 { t.Helper(); f, _ := m[k].(float64); return f }
	sub := func(m map[string]any, k string) map[string]any { t.Helper(); s, _ := m[k].(map[string]any); return s }

	ok(e.send(adult, "PUT", child1+"/allowed-hours", map[string]any{"allowedHours": map[string]any{
		"schoolNight": map[string]any{"start": "15:00", "end": "19:00"}, "weekend": map[string]any{"start": "09:00", "end": "20:00"},
	}}), "set allowed hours")
	refused(e.send(adult, "PUT", child1+"/allowed-hours", map[string]any{"allowedHours": map[string]any{
		"schoolNight": map[string]any{"start": "19:00", "end": "15:00"}, "weekend": map[string]any{"start": "09:00", "end": "20:00"},
	}}), 422, "invalid_hours", "hours ending before they start")
	refused(e.send(device, "POST", child1+"/screen-time/spend", map[string]any{"minutes": 30}), 409, "nothing_banked", "spend with nothing banked")

	// Bought from the child's own device: no approval, banked at once.
	e.exec(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id) VALUES ($1, $2, 20, 'award', 'fixture', gen_random_uuid())`, h.id, h.child1)
	r := ok(e.send(adult, "POST", "/api/rewards", map[string]any{"title": "30 min of iPad", "cost": 6, "kind": "screen_time", "minutes": 30}), "create reward")
	if r.body["target"] != "ipad" {
		t.Errorf("reward target: %v", r.body["target"])
	}
	reward := r.body["id"]
	refused(e.send(adult, "POST", "/api/rewards", map[string]any{"title": "Ice cream", "cost": 6, "kind": "item", "target": "tv"}), 422, "target_mismatch", "an item for the TV")
	// An edit that names no screen keeps the reward's; a TV reward can't be bought yet.
	tvReward := ok(e.send(adult, "POST", "/api/rewards", map[string]any{"title": "TV", "cost": 6, "kind": "screen_time", "minutes": 30, "target": "tv"}), "TV reward").body["id"].(string)
	if r := ok(e.send(adult, "PUT", "/api/rewards/"+tvReward, map[string]any{"title": "TV time", "cost": 6, "kind": "screen_time", "minutes": 45, "available": true, "expectedVersion": 1}), "edit TV reward"); r.body["target"] != "tv" {
		t.Errorf("edited TV reward: %s", r.raw)
	}
	refused(e.send(h.actors["device-web"], "POST", child1+"/redemptions", map[string]any{"rewardId": tvReward}), 422, "tv_not_ready", "buy TV time")
	r = ok(e.send(h.actors["device-web"], "POST", child1+"/redemptions", map[string]any{"rewardId": reward}), "redeem")
	if r.body["status"] != "fulfilled" {
		t.Errorf("screen time redemption: %s", r.raw)
	}
	redemption := r.body["id"].(string)
	st := get(device, child1)
	if num(st, "bankedSeconds") != 1800 || len(st["windows"].([]any)) != 0 || st["unlockedUntil"] != nil || num(st, "allowanceSeconds") != 0 {
		t.Errorf("banked: %v", st)
	}
	if hn := sub(st, "hoursNow"); hn["open"] != true || hn["closesAt"] != at(19, 0) {
		t.Errorf("hours now: %v", hn)
	}

	// The child spends 30 minutes at 18:45: cut off at 19:00, the rest stays banked.
	r = ok(e.send(device, "POST", child1+"/screen-time/spend", map[string]any{"minutes": 30}), "spend")
	if sp := sub(r.body, "spend"); num(sp, "seconds") != 900 || sp["clipped"] != true || sp["endsAt"] != at(19, 0) {
		t.Errorf("clipped spend: %v", sp)
	}
	if st := sub(r.body, "screenTime"); num(st, "bankedSeconds") != 900 || num(st, "allowanceSeconds") != 300 || st["unlockedUntil"] != at(19, 0) {
		t.Errorf("after spending: %v", st)
	}
	refused(e.send(device, "POST", child1+"/screen-time/spend", map[string]any{"minutes": 5}), 409, "hours_full", "spend past the end of allowed hours")

	// After hours the child can't start any; a parent can.
	clk.Set(time.Date(2026, 11, 18, 19, 30, 0, 0, ams))
	r = e.send(device, "POST", child1+"/screen-time/spend", map[string]any{"minutes": 5})
	refused(r, 409, "outside_hours", "spend after hours")
	if msg := sub(r.body, "error")["message"]; msg != "Screen time starts at 3:00 PM tomorrow." {
		t.Errorf("after-hours message: %v", msg)
	}
	r = ok(e.send(adult, "POST", child1+"/screen-time/spend", map[string]any{"minutes": 30}), "a parent spends")
	if sp := sub(r.body, "spend"); num(sp, "seconds") != 900 || sp["clipped"] != false || sp["startsAt"] != at(19, 30) || sp["endsAt"] != at(19, 45) {
		t.Errorf("a parent's spend: %v", sp)
	}
	r = ok(e.send(h.actors["phone"], "POST", child1+"/screen-time/grant", map[string]any{"minutes": 20, "startNow": true}), "grant and start")
	if sp := sub(r.body, "spend"); sp["startsAt"] != at(19, 45) || sp["endsAt"] != at(20, 5) {
		t.Errorf("granted time queues after: %v", sp)
	}
	refused(e.send(adult, "POST", child1+"/screen-time/grant", map[string]any{"minutes": 20, "target": "tv"}), 422, "tv_not_ready", "TV time")

	// Cancel what is queued, putting it back; then end the rest now, forfeiting it.
	r = ok(e.send(adult, "POST", child1+"/screen-time/end", map[string]any{"scope": "queued", "refund": true}), "end queued")
	if num(r.body, "windows") != 1 || num(r.body, "refundedSeconds") != 1200 || r.body["keptUntil"] != at(19, 45) || num(sub(r.body, "screenTime"), "bankedSeconds") != 1200 {
		t.Errorf("end queued: %s", r.raw)
	}
	clk.Advance(5 * time.Minute)
	r = ok(e.send(adult, "POST", child1+"/screen-time/end", map[string]any{"scope": "now"}), "end now")
	if num(r.body, "cutSeconds") != 600 || num(r.body, "forfeitedSeconds") != 600 || sub(r.body, "screenTime")["unlockedUntil"] != nil || num(sub(r.body, "screenTime"), "bankedSeconds") != 1200 {
		t.Errorf("end now: %s", r.raw)
	}
	refused(e.send(adult, "POST", child1+"/screen-time/end", map[string]any{}), 409, "nothing_to_end", "end with nothing running")
	// All 30 bought minutes were drawn: nothing to refund.
	refused(e.send(adult, "POST", "/api/redemptions/"+redemption+"/cancel", map[string]any{"expectedVersion": 1}), 409, "already_used", "cancel used time")

	// Cancelling part-used time refunds the unused share, rounded down, and takes the rest back.
	e.exec(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id) VALUES ($1, $2, 10, 'award', 'fixture', gen_random_uuid())`, h.id, h.child2)
	r = ok(e.send(adult, "POST", child2+"/redemptions", map[string]any{"rewardId": reward}), "an adult redeems for child2")
	red2 := r.body["id"].(string)
	ok(e.send(adult, "POST", child2+"/screen-time/spend", map[string]any{"minutes": 10}), "child2 spends 10")
	r = ok(e.send(adult, "GET", "/api/redemptions", nil), "list redemptions")
	for _, x := range r.body["redemptions"].([]any) {
		if x := x.(map[string]any); x["id"] == red2 && (x["usedMinutes"] != float64(10) || x["target"] != "ipad") {
			t.Errorf("used minutes: %v", x)
		}
	}
	r = ok(e.send(adult, "POST", "/api/redemptions/"+red2+"/cancel", map[string]any{"expectedVersion": 1}), "cancel part-used")
	if r.body["refundedPoints"] != float64(4) {
		t.Errorf("prorated refund: %s", r.raw)
	}
	if st := get(adult, child2); num(st, "bankedSeconds") != 0 {
		t.Errorf("banked after cancelling: %v", st)
	}

	// Others can't see or change it.
	refused(e.send(other.actors["adult"], "GET", child1+"/screen-time", nil), 404, "not_found", "another household")
	refused(e.send(device, "GET", child2+"/screen-time", nil), 404, "not_found", "a device's other child")

	// Ask for more time: the inbox gets it; asking again waits on the same one.
	r = e.send(device, "POST", child1+"/time-requests", map[string]any{})
	if r.status != 201 || r.body["state"] != "pending" {
		t.Fatalf("ask: %d %s", r.status, r.raw)
	}
	asked := r.body["id"]
	if r := e.send(h.actors["device-web"], "POST", child1+"/time-requests", map[string]any{}); r.status != 200 || r.body["id"] != asked {
		t.Errorf("ask again: %d %s", r.status, r.raw)
	}
	item := func() string {
		t.Helper()
		for _, it := range ok(e.send(adult, "GET", "/api/inbox", nil), "inbox").body["items"].([]any) {
			if it := it.(map[string]any); it["kind"] == "time_request" {
				return it["id"].(string)
			}
		}
		t.Fatal("no time request in the inbox")
		return ""
	}
	inbox := func(id, how string, body any) resp {
		return e.send(adult, "POST", fmt.Sprintf("/api/inbox/%s/%s", id, how), body)
	}
	first := item()
	refused(inbox(first, "approve", map[string]any{}), 422, "choice_required", "approve without a choice")
	refused(inbox(h.approval.String(), "answer", map[string]any{"choice": "30min"}), 422, "not_a_time_request", "answer a chore")
	r = ok(inbox(first, "answer", map[string]any{"choice": "30min"}), "answer 30 min")
	if tr := sub(r.body, "timeRequest"); r.body["state"] != "approved" || tr["grantedUntil"] != at(20, 5) {
		t.Errorf("30 min: %s", r.raw)
	}
	if st := get(device, child1); st["unlockedUntil"] != at(20, 5) || sub(st, "timeRequest")["choice"] != "30min" {
		t.Errorf("after a yes: %v", st)
	}
	if r := inbox(first, "answer", map[string]any{"choice": "1h"}); r.status != 409 && r.status != 404 {
		t.Errorf("answer twice: %d %s", r.status, r.raw)
	}

	// A no, then asking again too soon; five minutes later it's fine.
	ok(e.send(device, "POST", child1+"/time-requests", map[string]any{}), "ask after a yes")
	if r := inbox(item(), "reject", map[string]any{}); r.status != 200 || r.body["state"] != "rejected" {
		t.Fatalf("no: %d %s", r.status, r.raw)
	}
	refused(e.send(device, "POST", child1+"/time-requests", map[string]any{}), 409, "asked_too_soon", "ask right after a no")
	clk.Advance(6 * time.Minute)
	ok(e.send(device, "POST", child1+"/time-requests", map[string]any{}), "ask after waiting")
	// All day, after today's hours: until midnight, after the time already queued.
	r = ok(inbox(item(), "answer", map[string]any{"choice": "all_day"}), "all day")
	if sub(r.body, "timeRequest")["grantedUntil"] != time.Date(2026, 11, 19, 0, 0, 0, 0, ams).UTC().Format(time.RFC3339) {
		t.Errorf("all day: %s", r.raw)
	}
	ok(e.send(device, "POST", child1+"/time-requests", map[string]any{}), "ask once more")
	refused(inbox(item(), "answer", map[string]any{"choice": "all_day"}), 409, "already_until", "all day again")

	// Shield events: each recorded once, however often they're sent.
	ev := uuid.NewString()
	body := map[string]any{"events": []map[string]any{{"id": ev, "kind": "one_more_minute", "secondsAgo": 90}, {"id": ev, "kind": "one_more_minute", "secondsAgo": 90}}}
	for range 2 {
		if r := ok(e.send(device, "POST", "/api/device/screen-time-events", body), "events"); len(r.body["recorded"].([]any)) != 1 {
			t.Errorf("recorded: %s", r.raw)
		}
	}
	var n int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM screen_time_event WHERE child_id = $1`, h.child1).Scan(&n); err != nil || n != 1 {
		t.Errorf("events stored: %d %v", n, err)
	}
}
