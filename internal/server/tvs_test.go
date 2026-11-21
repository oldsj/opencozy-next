package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/clock"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/tv"
)

// tvEnv is a household with the TV fixture (h.tv, asking "Who's watching?"), the commands'
// clock faked from now, and service credentials for the router and Home Assistant.
type tvEnv struct {
	*env
	h             house
	fake          *clock.Fake
	router, hass  cred
	adult, device cred
}

func newTVEnv(t *testing.T) tvEnv {
	t.Helper()
	e := newEnv(t, nil)
	h := e.house("a", e.srv.table.Scopes())
	fake := clock.NewFake(time.Now().Add(time.Second).Truncate(time.Second))
	e.srv.runner.Clock = fake
	service := func(scope string) cred {
		r := e.send(h.actors["adult"], "POST", "/api/credentials", map[string]any{"kind": "service", "name": scope, "scopes": []string{scope}, "allChildren": true})
		if r.status != 201 {
			t.Fatalf("credential %s: %d %s", scope, r.status, r.raw)
		}
		// Only a home server may hold it, never an assistant.
		if r := e.send(h.actors["adult"], "POST", "/api/credentials", map[string]any{"kind": "assistant", "name": scope, "scopes": []string{scope}, "allChildren": true}); r.status != 400 || r.code() != "unknown_scope" {
			t.Fatalf("assistant %s: %d %s", scope, r.status, r.raw)
		}
		return cred{bearer: r.body["secret"].(string)}
	}
	return tvEnv{env: e, h: h, fake: fake, router: service("tv:gate"), hass: service("tv:report"), adult: h.actors["adult"], device: h.actors["device"]}
}

// give gives a child TV time directly (the reward that adds it is slice 8's).
func (e tvEnv) give(child uuid.UUID, seconds int) {
	e.exec(`INSERT INTO tv_credit (household_id, child_id, granted_seconds, seconds, created_at) VALUES ($1, $2, $3, $3, $4)`,
		e.h.id, child, seconds, e.now())
}

func (e tvEnv) now() time.Time {
	t, _ := e.fake.Now(context.Background(), nil)
	return t
}

func (e tvEnv) allowances() map[string]float64 {
	r := e.send(e.router, "GET", "/api/tv/allowances", nil)
	if r.status != 200 {
		e.t.Fatalf("allowances: %d %s", r.status, r.raw)
	}
	out := map[string]float64{}
	for _, a := range r.body["allowances"].([]any) {
		m := a.(map[string]any)
		out[m["mac"].(string)] = m["secondsRemaining"].(float64)
	}
	return out
}

func (e tvEnv) tvState(id uuid.UUID) map[string]any {
	r := e.send(e.adult, "GET", "/api/tvs", nil)
	if r.status != 200 {
		e.t.Fatalf("list: %d %s", r.status, r.raw)
	}
	for _, t := range r.body["tvs"].([]any) {
		if m := t.(map[string]any); m["id"] == id.String() {
			return m
		}
	}
	e.t.Fatalf("no TV %s", id)
	return nil
}

// settle runs the settlement job once at the fake time.
func (e tvEnv) settle() {
	if err := e.srv.tv.SettleAll(context.Background(), poolTx{e.srv}, e.fake); err != nil {
		e.t.Fatal(err)
	}
}

type poolTx struct{ s *Server }

func (p poolTx) Tx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	tx, err := p.s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	if err := fn(dbq.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestTVRegistry(t *testing.T) {
	e := newTVEnv(t)
	r := e.send(e.adult, "POST", "/api/tvs", map[string]any{"name": " Living room TV ", "room": "Living room", "macs": []string{"02-00-5E-10-00-0A", "02:00:5e:10:00:0b"},
		"haEntityId": "media_player.living_room", "openHours": map[string]any{"start": "21:00", "end": "07:00", "days": []string{"fri", "sat"}}})
	if r.status != 201 || r.body["name"] != "Living room TV" || r.body["power"] != "unknown" || len(r.body["macs"].([]any)) != 2 || r.body["macs"].([]any)[0] != "02:00:5e:10:00:0a" {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	id := r.body["id"].(string)
	if days := r.body["openHours"].(map[string]any)["days"].([]any); len(days) != 2 || days[0] != "fri" {
		t.Fatalf("open hours: %s", r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs", map[string]any{"name": "Other", "macs": []string{"02:00:5e:10:00:01"}}); r.status != 409 || r.code() != "mac_taken" {
		t.Fatalf("taken MAC: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs", map[string]any{"name": "Other", "macs": []string{"02:00:5e:10:00:0c"}, "haEntityId": "media_player.living_room"}); r.status != 409 || r.code() != "entity_taken" {
		t.Fatalf("taken entity: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs", map[string]any{"name": "Other", "macs": []string{"02:00:5e:10:00:0c"}, "openHours": map[string]any{"start": "09:00", "end": "09:00"}}); r.status != 422 {
		t.Fatalf("empty open hours: %d %s", r.status, r.raw)
	}

	// Home Assistant has reported; changing the entity forgets that.
	if r := e.send(e.hass, "POST", "/api/tv/power", map[string]any{"entityId": "media_player.living_room", "state": "off"}); r.status != 200 || r.body["power"] != "off" {
		t.Fatalf("power: %d %s", r.status, r.raw)
	}
	edit := map[string]any{"name": "Lounge TV", "room": nil, "macs": []string{"02:00:5e:10:00:0b"}, "haEntityId": "media_player.lounge", "openHours": nil, "expectedVersion": 1}
	r = e.send(e.adult, "PUT", "/api/tvs/"+id, edit)
	if r.status != 200 || r.body["power"] != "unknown" || r.body["version"] != float64(2) || r.body["room"] != nil || r.body["openHours"] != nil {
		t.Fatalf("edit: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "PUT", "/api/tvs/"+id, edit); r.status != 409 || r.code() != "version_conflict" {
		t.Fatalf("stale edit: %d %s", r.status, r.raw)
	}
	// The dropped MAC is free again.
	if r := e.send(e.adult, "POST", "/api/tvs", map[string]any{"name": "Bedroom TV", "macs": []string{"02:00:5e:10:00:0a"}}); r.status != 201 {
		t.Fatalf("reuse MAC: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "DELETE", "/api/tvs/"+id, nil); r.status != 204 {
		t.Fatalf("remove: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "DELETE", "/api/tvs/"+id, nil); r.status != 404 {
		t.Fatalf("remove again: %d %s", r.status, r.raw)
	}
	if n := len(e.send(e.adult, "GET", "/api/tvs", nil).body["tvs"].([]any)); n != 2 {
		t.Fatalf("TVs: %d", n)
	}
	// A display sees names and state, never MACs or entities.
	r = e.send(e.h.actors["display-tv"], "GET", "/api/tvs/display", nil)
	if r.status != 200 || len(r.body["tvs"].([]any)) != 2 || r.body["tvs"].([]any)[0].(map[string]any)["macs"] != nil {
		t.Fatalf("display: %d %s", r.status, r.raw)
	}
	// A display learns its scopes, so its board offers a TV tab only with tv.
	if r := e.send(e.h.actors["display-today"], "GET", "/api/display/session", nil); r.status != 200 || len(r.body["scopes"].([]any)) != 1 || r.body["scopes"].([]any)[0] != "today" {
		t.Fatalf("display session: %d %s", r.status, r.raw)
	}
}

func TestTVChildSessionSpendsTimeAndEnds(t *testing.T) {
	e := newTVEnv(t)
	mac := "02:00:5e:10:00:01"
	state := e.tvState(e.h.tv)
	prompt := state["prompt"].(map[string]any)
	if len(e.allowances()) != 0 {
		t.Fatal("a gated TV is allowed")
	}
	answer := func(body map[string]any) resp {
		return e.send(e.adult, "POST", "/api/tv-prompts/"+prompt["id"].(string)+"/answer", body)
	}
	if r := answer(map[string]any{"expectedVersion": 1, "answer": "child", "childId": e.h.child1.String()}); r.status != 409 || r.code() != "no_tv_time" {
		t.Fatalf("no time: %d %s", r.status, r.raw)
	}
	if r := answer(map[string]any{"expectedVersion": 1, "answer": "child"}); r.status != 422 {
		t.Fatalf("child without a child: %d %s", r.status, r.raw)
	}
	e.give(e.h.child1, 600)
	r := answer(map[string]any{"expectedVersion": 1, "answer": "child", "childId": e.h.child1.String()})
	if r.status != 200 || r.body["prompt"].(map[string]any)["state"] != "answered" || r.body["session"].(map[string]any)["secondsRemaining"] != float64(600) {
		t.Fatalf("answer: %d %s", r.status, r.raw)
	}
	if r := answer(map[string]any{"expectedVersion": 1, "answer": "parents"}); r.status != 409 || r.code() != "already_answered" {
		t.Fatalf("second answer: %d %s", r.status, r.raw)
	}
	// The router is given at most 300 seconds, and a session's MACs drop out a minute early.
	if a := e.allowances(); a[mac] != 300 {
		t.Fatalf("allowance: %v", a)
	}
	e.fake.Advance(400 * time.Second)
	if a := e.allowances(); a[mac] != 140 {
		t.Fatalf("allowance near the end: %v", a)
	}
	kid := e.send(e.device, "GET", "/api/children/"+e.h.child1.String()+"/tv", nil)
	if kid.status != 200 || kid.body["secondsRemaining"] != float64(200) || kid.body["watching"].(map[string]any)["tvName"] != "Den TV" {
		t.Fatalf("kid view: %d %s", kid.status, kid.raw)
	}
	// Reads see the end before anything settles it; the job then records it at its real end.
	e.fake.Advance(300 * time.Second)
	if a := e.allowances(); len(a) != 0 {
		t.Fatalf("allowed after the end: %v", a)
	}
	e.settle()
	r = e.send(e.adult, "GET", "/api/tvs/history", nil)
	s := r.body["sessions"].([]any)[0].(map[string]any)
	if s["endReason"] != "time_up" || s["watchedSeconds"] != float64(600) || s["childName"] == nil {
		t.Fatalf("history: %s", r.raw)
	}
	var used int
	if err := e.owner.QueryRow(t.Context(), `SELECT used_seconds FROM tv_credit WHERE child_id = $1`, e.h.child1).Scan(&used); err != nil || used != 600 {
		t.Fatalf("used: %d %v", used, err)
	}
}

func TestTVDevicesAndDisplays(t *testing.T) {
	e := newTVEnv(t)
	display := e.h.actors["display-tv"]
	tvID := e.h.tv.String()
	e.give(e.h.child1, 1800)
	// The kid view lists the TV asking; the child starts their own time from their device.
	kid := e.send(e.device, "GET", "/api/children/"+e.h.child1.String()+"/tv", nil)
	if w := kid.body["waiting"].([]any); len(w) != 1 || w[0].(map[string]any)["tvId"] != tvID {
		t.Fatalf("waiting: %s", kid.raw)
	}
	r := e.send(e.device, "POST", "/api/children/"+e.h.child1.String()+"/tv/watch", map[string]any{"tvId": tvID})
	if r.status != 200 || r.body["watcher"] != "child" || r.body["childId"] != e.h.child1.String() {
		t.Fatalf("watch: %d %s", r.status, r.raw)
	}
	if r := e.send(e.device, "POST", "/api/children/"+e.h.child1.String()+"/tv/watch", map[string]any{"tvId": tvID}); r.status != 409 || r.code() != "tv_on" {
		t.Fatalf("watch again: %d %s", r.status, r.raw)
	}
	// Asking for a TV that is on answers on.
	if r := e.send(display, "POST", "/api/tvs/"+tvID+"/ask", nil); r.status != 200 || r.body["on"] != true {
		t.Fatalf("ask while on: %d %s", r.status, r.raw)
	}
	if r := e.send(e.device, "POST", "/api/tvs/"+tvID+"/done", nil); r.status != 200 || r.body["session"].(map[string]any)["endReason"] != "done" {
		t.Fatalf("done: %d %s", r.status, r.raw)
	}
	// The display asks; a child's device can't end a parents session, the display can.
	if r := e.send(display, "POST", "/api/tvs/"+tvID+"/ask", nil); r.status != 200 || r.body["asking"] != true {
		t.Fatalf("ask: %d %s", r.status, r.raw)
	}
	if r := e.send(display, "POST", "/api/tvs/"+tvID+"/ask", nil); r.status != 200 || r.body["asking"] != true {
		t.Fatalf("ask again: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/"+tvID+"/session", map[string]any{"watcher": "parents"}); r.status != 200 || r.body["endsAt"] == nil {
		t.Fatalf("parents: %d %s", r.status, r.raw)
	}
	if state := e.tvState(e.h.tv); state["prompt"] != nil {
		t.Fatalf("the prompt stayed: %v", state)
	}
	if r := e.send(e.device, "POST", "/api/tvs/"+tvID+"/done", nil); r.status != 403 || r.code() != "not_your_session" {
		t.Fatalf("device ends parents: %d %s", r.status, r.raw)
	}
	r = e.send(display, "GET", "/api/tvs/display", nil)
	if s := r.body["tvs"].([]any)[0].(map[string]any)["session"].(map[string]any); s["watcher"] != "parents" || s["secondsRemaining"] != nil {
		t.Fatalf("display session: %s", r.raw)
	}
	if r := e.send(display, "POST", "/api/tvs/"+tvID+"/done", nil); r.status != 200 || r.body["session"].(map[string]any)["endReason"] != "done" {
		t.Fatalf("display done: %d %s", r.status, r.raw)
	}
	if r := e.send(display, "POST", "/api/tvs/"+tvID+"/done", nil); r.status != 200 || r.body["session"] != nil {
		t.Fatalf("done twice: %d %s", r.status, r.raw)
	}
}

func TestTVOpenAndAllowedHours(t *testing.T) {
	e := newTVEnv(t)
	tvID := e.h.tv.String()
	e.give(e.h.child1, 3600)
	if r := e.send(e.adult, "POST", "/api/tvs/"+tvID+"/session", map[string]any{"watcher": "child", "childId": e.h.child1.String()}); r.status != 200 {
		t.Fatalf("start: %d %s", r.status, r.raw)
	}
	e.fake.Advance(10 * time.Minute)
	r := e.send(e.adult, "POST", "/api/tvs/open", map[string]any{"tvIds": []string{tvID}, "duration": "1h"})
	if r.status != 200 || len(r.body["endedSessions"].([]any)) != 1 || r.body["endedSessions"].([]any)[0].(map[string]any)["endReason"] != "parent_override" {
		t.Fatalf("open: %d %s", r.status, r.raw)
	}
	if a := e.allowances(); a["02:00:5e:10:00:01"] != 300 {
		t.Fatalf("open allowance: %v", a)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/"+tvID+"/session", map[string]any{"watcher": "parents"}); r.status != 409 || r.code() != "tv_open" {
		t.Fatalf("start on an open TV: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/open", map[string]any{"all": true, "tvIds": []string{tvID}, "duration": "1h"}); r.status != 422 {
		t.Fatalf("all and some: %d %s", r.status, r.raw)
	}
	r = e.send(e.adult, "POST", "/api/tvs/extend", map[string]any{"all": true})
	if r.status != 200 || !e.now().Add(2*time.Hour).Equal(parseTime(t, r.body["tvs"].([]any)[0].(map[string]any)["openUntil"])) {
		t.Fatalf("extend: %d %s", r.status, r.raw)
	}
	for range 22 {
		e.send(e.adult, "POST", "/api/tvs/extend", map[string]any{"all": true})
	}
	if r := e.send(e.adult, "POST", "/api/tvs/extend", map[string]any{"all": true}); r.status != 409 || r.code() != "open_limit" {
		t.Fatalf("past 24 hours: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/close", map[string]any{"all": true}); r.status != 200 || r.body["tvs"].([]any)[0].(map[string]any)["openUntil"] != nil {
		t.Fatalf("close: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/extend", map[string]any{"all": true}); r.status != 409 || r.code() != "not_open" {
		t.Fatalf("extend closed: %d %s", r.status, r.raw)
	}
	h := e.send(e.adult, "GET", "/api/tvs/history", nil)
	if o := h.body["openings"].([]any); len(o) != 25 || o[len(o)-1].(map[string]any)["endedSessionId"] == nil || o[0].(map[string]any)["action"] != "close" {
		t.Fatalf("openings: %d", len(o))
	}
	// The child kept the time opening didn't use; allowed hours refuse outside them.
	if k := e.send(e.adult, "GET", "/api/children/"+e.h.child1.String()+"/tv", nil); k.body["secondsRemaining"] != float64(3000) {
		t.Fatalf("balance: %s", k.raw)
	}
	e.srv.tv.Hours = closedHours{opens: e.now().Add(time.Hour)}
	if r := e.send(e.adult, "POST", "/api/tvs/"+tvID+"/session", map[string]any{"watcher": "child", "childId": e.h.child1.String()}); r.status != 409 || r.code() != "outside_hours" {
		t.Fatalf("outside hours: %d %s", r.status, r.raw)
	}
}

type closedHours struct{ opens time.Time }

func (c closedHours) Status(context.Context, *dbq.Queries, uuid.UUID, uuid.UUID, time.Time) (tv.HoursStatus, error) {
	return tv.HoursStatus{Rule: true, OpensAt: c.opens}, nil
}

func parseTime(t *testing.T, v any) time.Time {
	t.Helper()
	s, _ := v.(string)
	out, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("time %v: %v", v, err)
	}
	return out
}

func TestTVPowerAndRouterAttempts(t *testing.T) {
	e := newTVEnv(t)
	mac := "02:00:5e:10:00:01"
	attempt := func(at time.Time) string {
		r := e.send(e.router, "POST", "/api/tv/attempts", map[string]any{"attempts": []map[string]any{{"mac": mac, "lastSeenAt": at}, {"mac": "02:00:5e:99:99:99", "lastSeenAt": at}}})
		if r.status != 200 || r.body["results"].([]any)[1].(map[string]any)["state"] != "unknown" {
			t.Fatalf("attempts: %d %s", r.status, r.raw)
		}
		return r.body["results"].([]any)[0].(map[string]any)["state"].(string)
	}
	if got := attempt(e.now()); got != "pending" {
		t.Fatalf("asking TV: %s", got)
	}
	if r := e.send(e.adult, "POST", "/api/tv-prompts/"+e.tvState(e.h.tv)["prompt"].(map[string]any)["id"].(string)+"/answer", map[string]any{"expectedVersion": 1, "answer": "not_now"}); r.status != 200 {
		t.Fatalf("not now: %d %s", r.status, r.raw)
	}
	// Quiet after "Not now"; then a TV without Home Assistant asks once it has kept trying for
	// three minutes.
	e.fake.Advance(31 * time.Minute)
	if got := attempt(e.now()); got != "suppressed" {
		t.Fatalf("first try: %s", got)
	}
	e.fake.Advance(2 * time.Minute)
	if got := attempt(e.now()); got != "suppressed" {
		t.Fatalf("two minutes: %s", got)
	}
	e.fake.Advance(time.Minute)
	if got := attempt(e.now()); got != "requested" {
		t.Fatalf("three minutes: %s", got)
	}
	if got := attempt(e.now()); got != "pending" {
		t.Fatalf("again: %s", got)
	}

	// With an entity, Home Assistant decides: off withdraws the prompt and keeps attempts quiet.
	tv := e.tvState(e.h.tv)
	edit := map[string]any{"name": "Den TV", "room": nil, "macs": []string{mac}, "haEntityId": "media_player.den_tv", "openHours": nil, "expectedVersion": tv["version"]}
	if r := e.send(e.adult, "PUT", "/api/tvs/"+e.h.tv.String(), edit); r.status != 200 {
		t.Fatalf("entity: %d %s", r.status, r.raw)
	}
	power := func(state string, changed *time.Time) resp {
		body := map[string]any{"entityId": "media_player.den_tv", "state": state}
		if changed != nil {
			body["lastChanged"] = changed
		}
		return e.send(e.hass, "POST", "/api/tv/power", body)
	}
	if r := power("standby", nil); r.status != 200 || r.body["power"] != "off" || r.body["asking"] != false {
		t.Fatalf("off: %d %s", r.status, r.raw)
	}
	e.fake.Advance(5 * time.Minute)
	if got := attempt(e.now()); got != "suppressed" {
		t.Fatalf("off and trying: %s", got)
	}
	// Turning on asks; playing again changes nothing; off ends the session.
	if r := power("playing", nil); r.status != 200 || r.body["asking"] != true || r.body["online"] != false {
		t.Fatalf("on: %d %s", r.status, r.raw)
	}
	if r := power("playing", nil); r.status != 200 || r.body["asking"] != true {
		t.Fatalf("on again: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/"+e.h.tv.String()+"/session", map[string]any{"watcher": "parents"}); r.status != 200 {
		t.Fatalf("parents: %d %s", r.status, r.raw)
	}
	// A pause unchanged for 20 minutes is no evidence: power unknown, the session keeps running.
	stale := e.now().Add(-20 * time.Minute)
	if r := power("paused", &stale); r.status != 200 || r.body["power"] != "unknown" || r.body["online"] != true {
		t.Fatalf("stale pause: %d %s", r.status, r.raw)
	}
	if r := power("unavailable", nil); r.status != 200 || r.body["power"] != "unknown" {
		t.Fatalf("unavailable: %d %s", r.status, r.raw)
	}
	if r := power("off", nil); r.status != 200 || r.body["online"] != false {
		t.Fatalf("off: %d %s", r.status, r.raw)
	}
	h := e.send(e.adult, "GET", "/api/tvs/history", nil)
	if s := h.body["sessions"].([]any)[0].(map[string]any); s["endReason"] != "tv_off" {
		t.Fatalf("history: %s", h.raw)
	}
	if r := e.send(e.hass, "POST", "/api/tv/power", map[string]any{"entityId": "media_player.nowhere", "state": "on"}); r.status != 404 || r.code() != "unknown_entity" {
		t.Fatalf("unknown entity: %d %s", r.status, r.raw)
	}
}

// TV time end to end: a TV reward bought on the child's device is TV time (not iPad time),
// watched on a TV, refunded in part on cancel (ending the session it paid for), and given by a
// parent; the child's allowed hours hold TV time too.
func TestTVRewards(t *testing.T) {
	e := newTVEnv(t)
	child1 := "/api/children/" + e.h.child1.String()
	tvSeconds := func() float64 {
		t.Helper()
		r := e.send(e.device, "GET", child1+"/tv", nil)
		if r.status != 200 {
			t.Fatalf("child TV: %d %s", r.status, r.raw)
		}
		return r.body["secondsRemaining"].(float64)
	}
	points := func() int {
		t.Helper()
		var n int
		if err := e.owner.QueryRow(t.Context(), `SELECT COALESCE(sum(delta), 0) FROM point_ledger WHERE child_id = $1`, e.h.child1).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	e.exec(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id) VALUES ($1, $2, 20, 'award', 'fixture', gen_random_uuid())`, e.h.id, e.h.child1)
	r := e.send(e.adult, "POST", "/api/rewards", map[string]any{"title": "30 min of TV", "cost": 6, "kind": "screen_time", "minutes": 30, "target": "tv"})
	if r.status != 201 {
		t.Fatalf("TV reward: %d %s", r.status, r.raw)
	}
	reward := r.body["id"].(string)
	before := points()
	r = e.send(e.h.actors["device-web"], "POST", child1+"/redemptions", map[string]any{"rewardId": reward})
	if r.status != 201 || r.body["status"] != "fulfilled" {
		t.Fatalf("buy TV time: %d %s", r.status, r.raw)
	}
	redemption := r.body["id"].(string)
	if got := tvSeconds(); got != 1800 || points() != before-6 {
		t.Fatalf("bought: %v seconds, %d points", got, points()-before)
	}
	if r := e.send(e.adult, "GET", child1+"/screen-time", nil); r.body["bankedSeconds"] != float64(0) {
		t.Fatalf("TV time banked as iPad time: %s", r.raw)
	}

	// Watched for 10 minutes, then cancelled: 20 of 30 minutes come back as 4 of 6 points, and
	// the session it paid for ends.
	prompt := e.tvState(e.h.tv)["prompt"].(map[string]any)["id"].(string)
	if r := e.send(e.adult, "POST", "/api/tv-prompts/"+prompt+"/answer", map[string]any{"expectedVersion": 1, "answer": "child", "childId": e.h.child1.String()}); r.status != 200 {
		t.Fatalf("watch: %d %s", r.status, r.raw)
	}
	e.fake.Advance(10 * time.Minute)
	r = e.send(e.adult, "POST", "/api/redemptions/"+redemption+"/cancel", map[string]any{"expectedVersion": 1})
	if r.status != 200 || r.body["refundedPoints"] != float64(4) || r.body["usedMinutes"] != float64(10) {
		t.Fatalf("cancel: %d %s", r.status, r.raw)
	}
	if got := tvSeconds(); got != 0 || points() != before-2 {
		t.Fatalf("after cancel: %v seconds, %d points", got, points()-before)
	}
	if s := e.tvState(e.h.tv)["session"]; s != nil {
		t.Fatalf("the session outlived its time: %v", s)
	}
	if r := e.send(e.adult, "POST", "/api/redemptions/"+redemption+"/cancel", map[string]any{"expectedVersion": 2}); r.status != 409 {
		t.Fatalf("cancel twice: %d %s", r.status, r.raw)
	}

	// TV time bought before it was kept here has no record of what was watched: no refund.
	old := e.id(`INSERT INTO redemption (household_id, child_id, reward_id, title, cost, minutes, screen_time_target, state, fulfilled_at)
		VALUES ($1, $2, $3, '30 min of TV', 6, 30, 'tv', 'fulfilled', now()) RETURNING id`, e.h.id, e.h.child1, reward)
	if r := e.send(e.adult, "POST", "/api/redemptions/"+old.String()+"/cancel", map[string]any{"expectedVersion": 1}); r.status != 409 || r.code() != "no_credit" {
		t.Fatalf("cancel without a credit: %d %s", r.status, r.raw)
	}

	// A parent gives TV time; outside the child's allowed hours it can't start.
	if r := e.send(e.h.actors["phone"], "POST", child1+"/screen-time/grant", map[string]any{"minutes": 20, "target": "tv"}); r.status != 200 {
		t.Fatalf("grant: %d %s", r.status, r.raw)
	}
	if got := tvSeconds(); got != 1200 {
		t.Fatalf("granted: %v", got)
	}
	ams, _ := time.LoadLocation("Europe/Amsterdam")
	window := map[string]any{"start": "20:00", "end": "21:00"}
	if e.now().In(ams).Hour() >= 19 {
		window = map[string]any{"start": "01:00", "end": "02:00"}
	}
	if r := e.send(e.adult, "PUT", child1+"/allowed-hours", map[string]any{"allowedHours": map[string]any{"schoolNight": window, "weekend": window}}); r.status != 200 {
		t.Fatalf("hours: %d %s", r.status, r.raw)
	}
	if r := e.send(e.adult, "POST", "/api/tvs/"+e.h.tv.String()+"/session", map[string]any{"watcher": "child", "childId": e.h.child1.String()}); r.status != 409 || r.code() != "outside_hours" {
		t.Fatalf("outside hours: %d %s", r.status, r.raw)
	}
}
