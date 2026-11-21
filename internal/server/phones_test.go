package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/clock"
	"github.com/oldsj/opencozy-next/internal/config"
)

const testPushToken = "0A1B2C3D4E5F60718293A4B5C6D7E8F90A1B2C3D4E5F60718293A4B5C6D7E8F9"

func setPassword(e *env, h house, pw string) {
	hash, err := auth.HashPassword(pw)
	if err != nil {
		e.t.Fatal(err)
	}
	e.exec(`UPDATE adult SET password_hash = $1, password_scheme = 'argon2id' WHERE id = $2`, hash, h.adult)
}

// phoneSignIn signs a phone in and returns its bearer credential and session ID.
func phoneSignIn(t *testing.T, e *env, h house, name string) (cred, string) {
	t.Helper()
	r := e.send(cred{}, "POST", "/api/phone/sessions", map[string]any{"email": "Sam.A@example.test", "password": "correct horse battery", "deviceName": name})
	if r.status != 201 {
		t.Fatalf("sign in: %d %s", r.status, r.raw)
	}
	tok := r.body["token"].(string)
	if !strings.HasPrefix(tok, "ocp_") {
		t.Fatalf("token %q", tok)
	}
	return cred{bearer: tok}, r.body["session"].(map[string]any)["id"].(string)
}

func TestPhoneSessions(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	setPassword(e, h, "correct horse battery")
	adult := h.actors["adult"]

	// Wrong password, unknown email and a blank name are refused.
	for _, body := range []map[string]any{
		{"email": "sam.a@example.test", "password": "wrong", "deviceName": "Phone"},
		{"email": "nobody@example.test", "password": "correct horse battery", "deviceName": "Phone"},
	} {
		if r := e.send(cred{}, "POST", "/api/phone/sessions", body); r.status != 401 || r.code() != "invalid_credentials" {
			t.Errorf("%v: %d %s", body, r.status, r.raw)
		}
	}
	if r := e.send(cred{}, "POST", "/api/phone/sessions", map[string]any{"email": "sam.a@example.test", "password": "correct horse battery", "deviceName": "  "}); r.status != 400 {
		t.Errorf("blank name: %d %s", r.status, r.raw)
	}

	phone, id := phoneSignIn(t, e, h, "Sam's iPhone")
	r := e.send(phone, "GET", "/api/phone/session", nil)
	if r.status != 200 {
		t.Fatalf("session: %d %s", r.status, r.raw)
	}
	s := r.body["session"].(map[string]any)
	if s["id"] != id || s["name"] != "Sam's iPhone" || s["current"] != true || s["pushRegistered"] != false || s["adultName"] != "Sam" {
		t.Errorf("session %v", s)
	}
	if exp, _ := time.Parse(time.RFC3339Nano, s["expiresAt"].(string)); exp.Before(time.Now().Add(89*24*time.Hour)) || exp.After(time.Now().Add(91*24*time.Hour)) {
		t.Errorf("expires %v", exp)
	}
	// The phone acts as its adult: the inbox and the household.
	if r := e.send(phone, "GET", "/api/inbox", nil); r.status != 200 {
		t.Errorf("inbox: %d %s", r.status, r.raw)
	}

	// Push: the token is stored lower-case; registering it on another phone moves it there.
	if r := e.send(phone, "PUT", "/api/phone/session/push", map[string]any{"token": testPushToken, "environment": "development"}); r.status != 200 || r.body["environment"] != "development" {
		t.Fatalf("register: %d %s", r.status, r.raw)
	}
	var stored string
	if err := e.owner.QueryRow(context.Background(), `SELECT push_token FROM phone_session WHERE id = $1`, id).Scan(&stored); err != nil || stored != strings.ToLower(testPushToken) {
		t.Fatalf("stored %q %v", stored, err)
	}
	second, secondID := phoneSignIn(t, e, h, "Sam's iPad")
	if r := e.send(second, "PUT", "/api/phone/session/push", map[string]any{"token": strings.ToLower(testPushToken), "environment": "production"}); r.status != 200 {
		t.Fatalf("register again: %d %s", r.status, r.raw)
	}
	registered := func() map[string]bool {
		t.Helper()
		r := e.send(adult, "GET", "/api/phones", nil)
		if r.status != 200 {
			t.Fatalf("list: %d %s", r.status, r.raw)
		}
		out := map[string]bool{}
		for _, it := range r.body["items"].([]any) {
			p := it.(map[string]any)
			if p["current"] != false {
				t.Errorf("current on the web: %v", p)
			}
			out[p["id"].(string)] = p["pushRegistered"].(bool)
		}
		return out
	}
	if got := registered(); got[id] || !got[secondID] {
		t.Errorf("the token did not move: %v", got)
	}
	// A kid device that is the same app install takes it too.
	if r := e.send(h.actors["device"], "PUT", "/api/device/push", map[string]any{"token": testPushToken, "environment": "production"}); r.status != 200 {
		t.Fatalf("device register: %d %s", r.status, r.raw)
	}
	if got := registered(); got[secondID] {
		t.Errorf("the device did not take the token: %v", got)
	}
	if r := e.send(h.actors["device"], "DELETE", "/api/device/push", nil); r.status != 204 {
		t.Errorf("device unregister: %d %s", r.status, r.raw)
	}
	if r := e.send(second, "PUT", "/api/phone/session/push", map[string]any{"token": "nothex", "environment": "production"}); r.status != 400 {
		t.Errorf("bad token: %d %s", r.status, r.raw)
	}

	// An adult signs a phone out in Manage: at once, and once.
	if r := e.send(adult, "DELETE", "/api/phones/"+secondID, nil); r.status != 204 {
		t.Fatalf("revoke: %d %s", r.status, r.raw)
	}
	if r := e.send(second, "GET", "/api/phone/session", nil); r.status != 401 {
		t.Errorf("revoked phone: %d", r.status)
	}
	if r := e.send(adult, "DELETE", "/api/phones/"+secondID, nil); r.status != 404 {
		t.Errorf("revoke twice: %d", r.status)
	}
	got := registered()
	if _, listed := got[secondID]; listed || len(got) != 3 { // this test's first phone and the fixture's two
		t.Errorf("list after revoke: %v", got)
	}

	// The phone signs itself out.
	if r := e.send(phone, "DELETE", "/api/phone/session", nil); r.status != 204 {
		t.Fatalf("sign out: %d %s", r.status, r.raw)
	}
	if r := e.send(phone, "GET", "/api/inbox", nil); r.status != 401 {
		t.Errorf("signed-out phone: %d", r.status)
	}
}

func TestPushMute(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult, phone := h.actors["adult"], h.actors["phone"]
	for _, until := range []string{time.Now().Add(-time.Minute).Format(time.RFC3339), time.Now().Add(91 * 24 * time.Hour).Format(time.RFC3339)} {
		if r := e.send(adult, "PUT", "/api/push/mute", map[string]any{"until": until}); r.status != 400 || r.code() != "invalid_until" {
			t.Errorf("%s: %d %s", until, r.status, r.raw)
		}
	}
	until := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if r := e.send(adult, "PUT", "/api/push/mute", map[string]any{"until": until.Format(time.RFC3339)}); r.status != 200 {
		t.Fatalf("mute: %d %s", r.status, r.raw)
	}
	r := e.send(phone, "GET", "/api/push/mute", nil)
	if got, _ := time.Parse(time.RFC3339, r.body["mutedUntil"].(string)); !got.Equal(until) {
		t.Errorf("muted until %v, want %v", r.body["mutedUntil"], until)
	}
	if r := e.send(phone, "GET", "/api/phone/session", nil); r.body["pushMutedUntil"] == nil {
		t.Errorf("phone session: %s", r.raw)
	}
	if r := e.send(phone, "PUT", "/api/push/mute", map[string]any{"until": nil}); r.status != 200 || r.body["mutedUntil"] != nil {
		t.Errorf("unmute: %d %s", r.status, r.raw)
	}
}

// pushRows lists the outbox: dedupe key and the notification's type.
func pushRows(e *env) map[string]string {
	rows, err := e.owner.Query(context.Background(), `SELECT dedupe_key, payload->>'type' FROM push_outbox`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, typ string
		if err := rows.Scan(&k, &typ); err != nil {
			e.t.Fatal(err)
		}
		out[k] = typ
	}
	return out
}

func TestApprovalPushes(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.APNsKeyID = "KEY1234567" })
	h := e.house("a", nil)
	child := h.actors["display-today"]

	// No phone can receive a push yet: nothing is queued.
	if r := e.send(child, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil); r.status != 200 {
		t.Fatalf("complete: %d %s", r.status, r.raw)
	}
	if rows := pushRows(e); len(rows) != 0 {
		t.Fatalf("queued without a phone: %v", rows)
	}

	if r := e.send(h.actors["phone"], "PUT", "/api/phone/session/push", map[string]any{"token": testPushToken, "environment": "production"}); r.status != 200 {
		t.Fatalf("register: %d %s", r.status, r.raw)
	}
	// The waiting earlier chore (h.approval) is decided: every parent's phone hears, silently.
	if r := e.send(h.actors["adult"], "POST", "/api/inbox/"+h.approval.String()+"/approve", map[string]any{}); r.status != 200 {
		t.Fatalf("approve: %d %s", r.status, r.raw)
	}
	want := "approval.resolved/" + h.approval.String() + "/" + h.adult.String()
	if rows := pushRows(e); rows[want] != "approval.resolved" || len(rows) != 1 {
		t.Fatalf("after approve: %v", rows)
	}

	// A child buys a reward: it waits for a parent, and the parent hears.
	if r := e.send(child, "POST", "/api/children/"+h.child1.String()+"/redemptions", map[string]any{"rewardId": h.reward}); r.status != 201 {
		t.Fatalf("redeem: %d %s", r.status, r.raw)
	}
	pending := 0
	for k, typ := range pushRows(e) {
		if typ == "approval.pending" && strings.HasPrefix(k, "approval.pending/") && strings.HasSuffix(k, "/"+h.adult.String()) {
			pending++
		}
	}
	if pending != 1 {
		t.Errorf("pending pushes: %v", pushRows(e))
	}
}

func TestAppleAppSiteAssociation(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.AppleTeamID, c.AppleBundleID = "TEAM123456", "org.example.opencozy" })
	r := e.send(cred{}, "GET", "/.well-known/apple-app-site-association", nil)
	if r.status != 200 || !strings.Contains(string(r.raw), `"TEAM123456.org.example.opencozy"`) {
		t.Errorf("%d %s", r.status, r.raw)
	}
}

func TestTimeRequestPushes(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.APNsKeyID = "KEY1234567" })
	h := e.house("a", nil)
	clk := clock.NewFake(time.Now())
	e.srv.runner.Clock = clk
	if r := e.send(h.actors["phone"], "PUT", "/api/phone/session/push", map[string]any{"token": testPushToken, "environment": "production"}); r.status != 200 {
		t.Fatalf("register: %d %s", r.status, r.raw)
	}
	ask := "/api/children/" + h.child1.String() + "/time-requests"
	pending := func() int {
		n := 0
		for k, typ := range pushRows(e) {
			if typ == "approval.pending" && strings.HasPrefix(k, "approval.pending/") {
				n++
			}
		}
		return n
	}
	r := e.send(h.actors["device"], "POST", ask, map[string]any{})
	if r.status != 201 {
		t.Fatalf("ask: %d %s", r.status, r.raw)
	}
	if pending() != 1 {
		t.Fatalf("after asking: %v", pushRows(e))
	}
	// Asking again at once is the same request and no new push.
	if r := e.send(h.actors["device"], "POST", ask, map[string]any{}); r.status != 200 || pending() != 1 {
		t.Fatalf("again at once: %d, %v", r.status, pushRows(e))
	}
	// Two minutes later it reminds the parents (old #74): a second push.
	clk.Advance(3 * time.Minute)
	if r := e.send(h.actors["device"], "POST", ask, map[string]any{}); r.status != 200 || pending() != 2 {
		t.Fatalf("reminder: %d, %v", r.status, pushRows(e))
	}
	// A parent's answer clears it from the phones.
	var item string
	if err := e.owner.QueryRow(context.Background(), `SELECT id FROM approval WHERE kind = 'time_request' AND child_id = $1`, h.child1).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if r := e.send(h.actors["adult"], "POST", "/api/inbox/"+item+"/reject", map[string]any{}); r.status != 200 {
		t.Fatalf("no: %d %s", r.status, r.raw)
	}
	if typ := pushRows(e)["approval.resolved/"+item+"/"+h.adult.String()]; typ != "approval.resolved" {
		t.Errorf("not cleared: %v", pushRows(e))
	}
}

func TestPhoneSignInFromWeb(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	body := map[string]any{"deviceName": "Sam's iPhone"}
	// A stale web sign-in isn't enough for a 90-day phone credential.
	if r := e.send(h.actors["adult-stale"], "POST", "/api/phone/sessions/web", body, "Sec-Fetch-Site", ""); r.status != 403 {
		t.Errorf("stale: %d %s", r.status, r.raw)
	}
	// A web page can't mint one, even same-origin: the browser marks its requests, and only the
	// app's native request (no Sec-Fetch-Site) is served. Cross-origin is refused earlier.
	if r := e.send(h.actors["adult"], "POST", "/api/phone/sessions/web", body); r.status != 403 || r.code() != "use_the_app" {
		t.Errorf("same-origin page: %d %s", r.status, r.raw)
	}
	for _, site := range []string{"same-site", "cross-site"} {
		if r := e.send(h.actors["adult"], "POST", "/api/phone/sessions/web", body, "Sec-Fetch-Site", site); r.status != 403 {
			t.Errorf("Sec-Fetch-Site %s: %d %s", site, r.status, r.raw)
		}
	}
	if r := e.send(h.actors["adult"], "POST", "/api/phone/sessions/web", body, "Sec-Fetch-Site", "none"); r.status != 201 {
		t.Errorf("Sec-Fetch-Site none: %d %s", r.status, r.raw)
	}
	r := e.send(h.actors["adult"], "POST", "/api/phone/sessions/web", body, "Sec-Fetch-Site", "")
	if r.status != 201 {
		t.Fatalf("from web: %d %s", r.status, r.raw)
	}
	phone := cred{bearer: r.body["token"].(string)}
	if r := e.send(phone, "GET", "/api/phone/session", nil); r.status != 200 || r.body["session"].(map[string]any)["name"] != "Sam's iPhone" {
		t.Errorf("session: %d %s", r.status, r.raw)
	}
	// A phone can't mint another phone.
	if r := e.send(phone, "POST", "/api/phone/sessions/web", body, "Sec-Fetch-Site", ""); r.status != 403 {
		t.Errorf("phone: %d %s", r.status, r.raw)
	}
}

// A child's device that registered for push hears a parent's answer to "Ask for more time" and
// is woken to sync when a parent changes their iPad time; the device's own requests and a
// child without such a device queue nothing.
func TestKidPushes(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.APNsKeyID = "KEY1234567" })
	h := e.house("a", nil)
	if r := e.send(h.actors["device"], "PUT", "/api/device/push", map[string]any{"token": testPushToken, "environment": "production"}); r.status != 200 {
		t.Fatalf("register: %d %s", r.status, r.raw)
	}
	kid := func() map[string]string {
		out := map[string]string{}
		rows, err := e.owner.Query(context.Background(), `SELECT child_id::text, payload->>'type', coalesce(payload->>'body', '') FROM push_outbox WHERE child_id IS NOT NULL`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var c, typ, body string
			if err := rows.Scan(&c, &typ, &body); err != nil {
				t.Fatal(err)
			}
			out[c+" "+typ] += body + ";"
		}
		return out
	}
	child1 := "/api/children/" + h.child1.String()
	if r := e.send(h.actors["device"], "POST", child1+"/time-requests", map[string]any{}); r.status != 201 {
		t.Fatalf("ask: %d %s", r.status, r.raw)
	}
	if got := kid(); len(got) != 0 {
		t.Errorf("the device's own ask pushed to it: %v", got)
	}
	var item string
	if err := e.owner.QueryRow(context.Background(), `SELECT id FROM approval WHERE kind = 'time_request' AND child_id = $1`, h.child1).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if r := e.send(h.actors["phone"], "POST", "/api/inbox/"+item+"/answer", map[string]any{"choice": "30min"}); r.status != 200 {
		t.Fatalf("answer: %d %s", r.status, r.raw)
	}
	if got := kid()[h.child1.String()+" time.answered"]; got != "You have 30 more minutes.;" {
		t.Errorf("answer push: %q (%v)", got, kid())
	}
	if got := kid()[h.child1.String()+" screentime.changed"]; got != "" {
		t.Errorf("an answer also pushed silently: %q", got)
	}
	if r := e.send(h.actors["adult"], "POST", child1+"/screen-time/end", map[string]any{"scope": "now"}); r.status != 200 {
		t.Fatalf("end: %d %s", r.status, r.raw)
	}
	if got := kid()[h.child1.String()+" screentime.changed"]; got != ";" {
		t.Errorf("ending time early: %q (%v)", got, kid())
	}
	// The kid view is a page in the app's web view: the app doesn't see its responses, so the
	// child spending banked time there wakes the app. The app's own spend doesn't.
	if r := e.send(h.actors["adult"], "POST", child1+"/screen-time/grant", map[string]any{"minutes": 30}); r.status != 200 {
		t.Fatalf("grant: %d %s", r.status, r.raw)
	}
	silent := func() int { return strings.Count(kid()[h.child1.String()+" screentime.changed"], ";") }
	before := silent()
	if r := e.send(h.actors["device-web"], "POST", child1+"/screen-time/spend", map[string]any{"minutes": 10}); r.status != 200 {
		t.Fatalf("kid view spend: %d %s", r.status, r.raw)
	}
	if got := silent(); got != before+1 {
		t.Errorf("spending from the kid view didn't wake the app: %d pushes, %d before", got, before)
	}
	if r := e.send(h.actors["device"], "POST", child1+"/screen-time/spend", map[string]any{"minutes": 10}); r.status != 200 {
		t.Fatalf("app spend: %d %s", r.status, r.raw)
	}
	if got := silent(); got != before+1 {
		t.Errorf("the app's own spend pushed to it: %d pushes, %d before", got, before+1)
	}
	// Child 2 has no device with a push token: giving them time queues nothing.
	if r := e.send(h.actors["adult"], "POST", "/api/children/"+h.child2.String()+"/screen-time/grant", map[string]any{"minutes": 10}); r.status != 200 {
		t.Fatalf("grant: %d %s", r.status, r.raw)
	}
	for k := range kid() {
		if strings.HasPrefix(k, h.child2.String()) {
			t.Errorf("child 2 has no push device: %v", kid())
		}
	}
}
