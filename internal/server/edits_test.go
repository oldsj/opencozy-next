package server

import (
	"testing"
)

func TestEditChoresAndRoutines(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	other := e.house("b", nil)

	// A chore: saved over its version, then a stale save is refused.
	chore := "/api/chores/" + h.chore.String()
	r := e.send(adult, "PUT", chore, map[string]any{"childId": h.child1, "title": " Feed the cat ", "points": 4, "days": []int{}, "expectedVersion": 1})
	if r.status != 200 || r.body["version"] != float64(2) || r.body["title"] != "Feed the cat" {
		t.Fatalf("edit chore: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", chore, map[string]any{"childId": h.child1, "title": "x", "points": 1, "days": []int{}, "expectedVersion": 1}); r.status != 409 || r.code() != "version_conflict" {
		t.Errorf("stale chore edit: %d %s", r.status, r.raw)
	}
	// Another household's child, or none: 422.
	if r := e.send(adult, "PUT", chore, map[string]any{"childId": other.child1, "title": "x", "points": 1, "days": []int{}, "expectedVersion": 2}); r.status != 422 || r.code() != "no_such_child" {
		t.Errorf("chore to another household's child: %d %s", r.status, r.raw)
	}
	// Reassigned: today it is the new child's.
	if r := e.send(adult, "PUT", chore, map[string]any{"childId": h.child2, "title": "Feed the cat", "points": 4, "days": []int{}, "expectedVersion": 2}); r.status != 200 || r.body["childId"] != h.child2.String() {
		t.Fatalf("reassign chore: %d %s", r.status, r.raw)
	}
	if !todayHas(e, adult, h.child2.String(), "Feed the cat") || todayHas(e, adult, h.child1.String(), "Feed the cat") {
		t.Error("today should list the chore under its new child only")
	}

	// A routine: add a step, tick one, reorder and replace the other; the tick stays.
	routine := "/api/routines/" + h.routine.String()
	r = e.send(adult, "PUT", routine, map[string]any{"childId": h.child1, "title": "Morning", "points": 3, "days": []int{},
		"steps": []map[string]any{{"id": h.step, "title": "Brush teeth"}, {"title": "Make the bed"}}, "expectedVersion": 1})
	if r.status != 200 || len(r.body["steps"].([]any)) != 2 {
		t.Fatalf("edit routine: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "POST", "/api/today/routines/"+h.routine.String()+"/steps/"+h.step.String()+"/check", nil); r.status != 200 {
		t.Fatalf("tick: %d %s", r.status, r.raw)
	}
	r = e.send(adult, "PUT", routine, map[string]any{"childId": h.child1, "title": "Morning", "points": 5, "days": []int{},
		"steps": []map[string]any{{"title": "Comb hair"}, {"id": h.step, "title": "Brush teeth"}}, "expectedVersion": 2})
	if r.status != 200 {
		t.Fatalf("reorder routine: %d %s", r.status, r.raw)
	}
	steps := r.body["steps"].([]any)
	if steps[0].(map[string]any)["title"] != "Comb hair" || steps[1].(map[string]any)["id"] != h.step.String() {
		t.Errorf("steps: %v", steps)
	}
	if item := todayItem(e, adult, h.child1.String(), h.step.String()); item == nil || item["state"] != "complete" {
		t.Errorf("the kept step's tick: %v", item)
	}
	if r := e.send(adult, "PUT", routine, map[string]any{"childId": h.child1, "title": "Morning", "points": 5, "days": []int{},
		"steps": []map[string]any{{"id": other.step, "title": "Brush teeth"}}, "expectedVersion": 3}); r.status != 422 || r.code() != "no_such_step" {
		t.Errorf("another routine's step: %d %s", r.status, r.raw)
	}
	// Reassigned: the open routine moves, its ticks cleared.
	if r := e.send(adult, "PUT", routine, map[string]any{"childId": h.child2, "title": "Morning", "points": 5, "days": []int{},
		"steps": []map[string]any{{"id": h.step, "title": "Brush teeth"}, {"title": "Comb hair"}}, "expectedVersion": 3}); r.status != 200 {
		t.Fatalf("reassign routine: %d %s", r.status, r.raw)
	}
	if item := todayItem(e, adult, h.child2.String(), h.step.String()); item == nil || item["state"] != "open" {
		t.Errorf("the moved routine's step: %v", item)
	}
}

func TestEditRewardsAndRedemptions(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	reward := "/api/rewards/" + h.reward.String()

	// Hidden from children: a display can't ask for it.
	r := e.send(adult, "PUT", reward, map[string]any{"title": "Choose a game", "cost": 3, "kind": "item", "available": false, "expectedVersion": 1})
	if r.status != 200 || r.body["available"] != false || r.body["version"] != float64(2) {
		t.Fatalf("hide reward: %d %s", r.status, r.raw)
	}
	if r := e.send(h.actors["display-today"], "POST", "/api/children/"+h.child1.String()+"/redemptions", map[string]any{"rewardId": h.reward}); r.status != 409 {
		t.Errorf("a hidden reward: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", reward, map[string]any{"title": "x", "cost": 3, "kind": "item", "available": true, "expectedVersion": 1}); r.status != 409 || r.code() != "version_conflict" {
		t.Errorf("stale reward edit: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", reward, map[string]any{"title": "x", "cost": 3, "kind": "screen_time", "available": true, "expectedVersion": 2}); r.status != 422 || r.code() != "minutes_mismatch" {
		t.Errorf("screen time without minutes: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PUT", reward, map[string]any{"title": "Choose a game", "cost": 3, "kind": "item", "available": true, "expectedVersion": 2}); r.status != 200 {
		t.Fatalf("show reward: %d %s", r.status, r.raw)
	}

	// The fixture's approved redemption: listed, given, and then not cancellable.
	list := func(status string) []any {
		t.Helper()
		path := "/api/redemptions"
		if status != "" {
			path += "?status=" + status
		}
		r := e.send(adult, "GET", path, nil)
		if r.status != 200 {
			t.Fatalf("list: %d %s", r.status, r.raw)
		}
		return r.body["redemptions"].([]any)
	}
	if got := list("approved"); len(got) != 1 || got[0].(map[string]any)["child"].(map[string]any)["id"] != h.child1.String() {
		t.Fatalf("approved: %v", got)
	}
	red := "/api/redemptions/" + h.redemption.String()
	if r := e.send(adult, "POST", red+"/fulfil", map[string]any{"expectedVersion": 2}); r.status != 409 || r.code() != "version_conflict" {
		t.Errorf("stale fulfil: %d %s", r.status, r.raw)
	}
	r = e.send(adult, "POST", red+"/fulfil", map[string]any{"expectedVersion": 1})
	if r.status != 200 || r.body["status"] != "fulfilled" || r.body["fulfilledAt"] == nil {
		t.Fatalf("fulfil: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "POST", red+"/fulfil", map[string]any{"expectedVersion": 2}); r.status != 409 || r.code() != "not_approved" {
		t.Errorf("fulfil twice: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "POST", red+"/cancel", map[string]any{"expectedVersion": 2}); r.status != 409 || r.code() != "not_cancellable" {
		t.Errorf("cancel a given item: %d %s", r.status, r.raw)
	}

	// An adult asks for it again (approved at once), then cancels: the points come back.
	points := func() float64 {
		t.Helper()
		r := e.send(adult, "GET", "/api/today", nil)
		for _, c := range r.body["children"].([]any) {
			if ch := c.(map[string]any)["child"].(map[string]any); ch["id"] == h.child1.String() {
				return ch["points"].(float64)
			}
		}
		t.Fatal("child1 not on today")
		return 0
	}
	e.exec(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id) VALUES ($1, $2, 10, 'award', 'fixture', gen_random_uuid())`, h.id, h.child1)
	before := points()
	r = e.send(adult, "POST", "/api/children/"+h.child1.String()+"/redemptions", map[string]any{"rewardId": h.reward})
	if r.status != 201 {
		t.Fatalf("redeem: %d %s", r.status, r.raw)
	}
	id := r.body["id"].(string)
	r = e.send(adult, "POST", "/api/redemptions/"+id+"/cancel", map[string]any{"expectedVersion": 1, "reason": "Changed our plans"})
	if r.status != 200 || r.body["status"] != "cancelled" || r.body["refundedPoints"] != float64(3) {
		t.Fatalf("cancel: %d %s", r.status, r.raw)
	}
	if after := points(); after != before {
		t.Errorf("points after cancelling: %v, want %v", after, before)
	}
	if r := e.send(adult, "POST", "/api/redemptions/"+id+"/cancel", map[string]any{"expectedVersion": 2}); r.status != 409 || r.code() != "not_cancellable" {
		t.Errorf("cancel twice: %d %s", r.status, r.raw)
	}
	if got := list("cancelled"); len(got) != 1 || got[0].(map[string]any)["id"] != id {
		t.Errorf("cancelled: %v", got)
	}
	if got := list(""); len(got) != 2 {
		t.Errorf("all: %d", len(got))
	}
	// Another household sees none of them.
	if r := e.send(e.house("b", nil).actors["adult"], "POST", "/api/redemptions/"+id+"/cancel", map[string]any{"expectedVersion": 3}); r.status != 404 {
		t.Errorf("another household's redemption: %d", r.status)
	}
}

func TestEditHousehold(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	r := e.send(adult, "PATCH", "/api/household", map[string]any{"name": " The Rivera household ", "expectedVersion": 1})
	if r.status != 200 || r.body["name"] != "The Rivera household" || r.body["version"] != float64(2) {
		t.Fatalf("rename: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PATCH", "/api/household", map[string]any{"name": "x", "expectedVersion": 1}); r.status != 409 || r.code() != "version_conflict" {
		t.Errorf("stale: %d %s", r.status, r.raw)
	}
	for _, tz := range []string{"EST", "Mars/Olympus_Mons", "Local"} {
		if r := e.send(adult, "PATCH", "/api/household", map[string]any{"timeZone": tz, "expectedVersion": 2}); r.status != 422 || r.code() != "time_zone_unknown" {
			t.Errorf("%s: %d %s", tz, r.status, r.raw)
		}
	}
	if r := e.send(adult, "PATCH", "/api/household", map[string]any{"expectedVersion": 2}); r.status != 400 {
		t.Errorf("nothing to change: %d %s", r.status, r.raw)
	}
	r = e.send(adult, "PATCH", "/api/household", map[string]any{"timeZone": "Pacific/Auckland", "expectedVersion": 2})
	if r.status != 200 || r.body["timeZone"] != "Pacific/Auckland" || r.body["name"] != "The Rivera household" {
		t.Fatalf("time zone: %d %s", r.status, r.raw)
	}
}

// todayItem finds an item (or a routine's step) in a child's day.
func todayItem(e *env, c cred, child, id string) map[string]any {
	r := e.send(c, "GET", "/api/today", nil)
	for _, ch := range r.body["children"].([]any) {
		tc := ch.(map[string]any)
		if tc["child"].(map[string]any)["id"] != child {
			continue
		}
		for _, g := range tc["groups"].([]any) {
			for _, it := range g.(map[string]any)["items"].([]any) {
				if item := it.(map[string]any); item["id"] == id {
					return item
				}
			}
		}
	}
	return nil
}

func todayHas(e *env, c cred, child, title string) bool {
	r := e.send(c, "GET", "/api/today", nil)
	for _, ch := range r.body["children"].([]any) {
		tc := ch.(map[string]any)
		if tc["child"].(map[string]any)["id"] != child {
			continue
		}
		for _, g := range tc["groups"].([]any) {
			for _, it := range g.(map[string]any)["items"].([]any) {
				if it.(map[string]any)["title"] == title {
					return true
				}
			}
		}
	}
	return false
}
