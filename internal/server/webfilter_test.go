package server

import (
	"slices"
	"testing"
	"time"

	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/clock"
)

// Blocked websites, read with the iPad's screen time, and the router's games gate.
func TestWebFilterAndGamesGate(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	clk := clock.NewFake(time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC))
	e.srv.runner.Clock = clk

	if r := e.send(adult, "GET", "/api/household/web-filter", nil); r.status != 200 || len(r.body["gamePortals"].([]any)) != 0 {
		t.Fatalf("empty filter: %d %s", r.status, r.raw)
	}
	r := e.send(h.actors["phone"], "PUT", "/api/household/web-filter", map[string]any{
		"gamePortals": []string{" Portal.Example.test. ", "portal.example.test"}, "games": []string{"b.example.test", "a.example.test"},
	})
	if r.status != 200 {
		t.Fatalf("set filter: %d %s", r.status, r.raw)
	}
	strs := func(v any) []string {
		var out []string
		for _, x := range v.([]any) {
			out = append(out, x.(string))
		}
		return out
	}
	if !slices.Equal(strs(r.body["gamePortals"]), []string{"portal.example.test"}) || !slices.Equal(strs(r.body["games"]), []string{"a.example.test", "b.example.test"}) {
		t.Errorf("tidied filter: %s", r.raw)
	}
	for _, c := range []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"gamePortals": []string{"not a site"}, "games": []string{}}, "invalid_domain"},
		{map[string]any{"gamePortals": []string{"x.example.test"}, "games": []string{"x.example.test"}}, "on_both_lists"},
	} {
		if r := e.send(adult, "PUT", "/api/household/web-filter", c.body); r.status != 422 || r.code() != c.code {
			t.Errorf("%v: %d %s", c.body, r.status, r.raw)
		}
	}
	// The iPad reads it with its screen time.
	r = e.send(h.actors["device"], "GET", "/api/children/"+h.child1.String()+"/screen-time", nil)
	if f, _ := r.body["webFilter"].(map[string]any); r.status != 200 || f == nil || !slices.Equal(strs(f["games"]), []string{"a.example.test", "b.example.test"}) {
		t.Errorf("device's filter: %d %s", r.status, r.raw)
	}

	// A router allowed child1 only: child1's 10 running minutes show, capped at 5; child2's don't.
	tok, hash, _ := auth.NewToken(auth.PrefixService)
	router := e.id(`INSERT INTO credential (household_id, kind, name, token_hash, scopes, all_children, adult_id)
		VALUES ($1, 'service', 'Router', $2, '{games:gate,screentime:read}', false, $3) RETURNING id`, h.id, hash, h.adult)
	e.exec(`INSERT INTO credential_child (household_id, credential_id, child_id) VALUES ($1, $2, $3)`, h.id, router, h.child1)
	gate := cred{bearer: tok}
	allowances := func() []any {
		t.Helper()
		r := e.send(gate, "GET", "/api/games/allowances", nil)
		if r.status != 200 {
			t.Fatalf("gate: %d %s", r.status, r.raw)
		}
		return r.body["allowances"].([]any)
	}
	if got := allowances(); len(got) != 0 {
		t.Errorf("nothing running: %v", got)
	}
	for _, c := range []string{h.child1.String(), h.child2.String()} {
		if r := e.send(adult, "POST", "/api/children/"+c+"/screen-time/grant", map[string]any{"minutes": 10, "startNow": true}); r.status != 200 {
			t.Fatalf("grant: %d %s", r.status, r.raw)
		}
	}
	got := allowances()
	if len(got) != 1 || got[0].(map[string]any)["childId"] != h.child1.String() || got[0].(map[string]any)["secondsRemaining"] != float64(300) {
		t.Errorf("allowances: %v", got)
	}
	clk.Advance(9*time.Minute + 30*time.Second)
	if got := allowances(); len(got) != 1 || got[0].(map[string]any)["secondsRemaining"] != float64(30) {
		t.Errorf("near the end: %v", got)
	}
	clk.Advance(time.Minute)
	if got := allowances(); len(got) != 0 {
		t.Errorf("after the end: %v", got)
	}
	// screentime:read: its own child only.
	if r := e.send(gate, "GET", "/api/children/"+h.child1.String()+"/screen-time", nil); r.status != 200 {
		t.Errorf("service read: %d %s", r.status, r.raw)
	}
	if r := e.send(gate, "GET", "/api/children/"+h.child2.String()+"/screen-time", nil); r.status != 404 {
		t.Errorf("service read of another child: %d %s", r.status, r.raw)
	}
	// Without games:gate: refused.
	if r := e.send(h.actors["service-all"], "GET", "/api/games/allowances", nil); r.status != 403 {
		t.Errorf("no scope: %d %s", r.status, r.raw)
	}
}
