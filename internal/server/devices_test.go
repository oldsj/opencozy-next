package server

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
)

// pairBody is what the kid app sends to redeem an offer: its key and the proof it holds it.
func pairBody(d *deviceKey, field, value string) map[string]any {
	pub := base64.StdEncoding.EncodeToString(d.publicKey())
	return map[string]any{field: value, "publicKey": pub, "proof": d.signRaw(actor.PairProof(value, pub)), "platform": "ios", "name": "Kit's iPad"}
}

// freshSession signs the adult in again, for a test that signed them out.
func freshSession(e *env, adult uuid.UUID) *http.Cookie {
	tok, hash, _ := auth.NewSessionToken()
	e.exec(`INSERT INTO adult_session (token_hash, adult_id, authenticated_at, expires_at) VALUES ($1, $2, now(), now() + interval '1 hour')`, hash, adult)
	return &http.Cookie{Name: actor.AdultCookie, Value: tok}
}

func TestDevices(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	none := cred{}
	offer := func(child uuid.UUID) map[string]any {
		t.Helper()
		r := e.send(adult, "POST", "/api/devices/pairings", map[string]any{"childId": child})
		if r.status != 201 {
			t.Fatalf("offer: %d %s", r.status, r.raw)
		}
		return r.body
	}

	// An offer by code: any case, with or without the dash.
	o := offer(h.child2)
	code := o["code"].(string)
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("code %q", code)
	}
	if qr := o["qrPayload"].(string); !strings.HasPrefix(qr, "opencozy-pair:v1?origin=https%3A%2F%2F127.0.0.1") || !strings.HasSuffix(qr, "&token="+o["token"].(string)) {
		t.Errorf("qrPayload %q", qr)
	}
	kit := newDeviceKey()
	// A proof by another key is refused, and leaves the offer usable.
	bad := pairBody(kit, "code", strings.ToLower(strings.ReplaceAll(code, "-", "")))
	bad["proof"] = newDeviceKey().signRaw(actor.PairProof(bad["code"].(string), bad["publicKey"].(string)))
	if r := e.send(none, "POST", "/api/device/pair", bad); r.status != 409 || r.code() != "pairing_unavailable" {
		t.Fatalf("wrong proof: %d %s", r.status, r.raw)
	}
	r := e.send(none, "POST", "/api/device/pair", pairBody(kit, "code", strings.ToLower(strings.ReplaceAll(code, "-", ""))))
	if r.status != 201 {
		t.Fatalf("pair: %d %s", r.status, r.raw)
	}
	dev := r.body["device"].(map[string]any)
	kit.id = uuid.MustParse(dev["id"].(string))
	if dev["child"].(map[string]any)["name"] != "Kit" || r.body["household"].(map[string]any)["lockMessage"] != DefaultLockMessage {
		t.Errorf("paired: %s", r.raw)
	}
	// Once only, and the same key can't pair twice.
	if r := e.send(none, "POST", "/api/device/pair", pairBody(newDeviceKey(), "code", code)); r.status != 409 {
		t.Errorf("used offer: %d %s", r.status, r.raw)
	}
	o2 := offer(h.child2)
	if r := e.send(none, "POST", "/api/device/pair", pairBody(kit, "token", o2["token"].(string))); r.status != 409 {
		t.Errorf("same key again: %d %s", r.status, r.raw)
	}

	// Signed requests: the device sees itself.
	device := cred{device: kit}
	if r := e.send(device, "GET", "/api/device", nil); r.status != 200 || r.body["device"].(map[string]any)["id"] != kit.id.String() {
		t.Fatalf("GET /device: %d %s", r.status, r.raw)
	}
	// A replayed nonce, a stale timestamp, a tampered body or another key: the same 401.
	signed := func(mutate func(req *http.Request)) int {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), "GET", e.base+"/api/device", nil)
		kit.sign(req, nil)
		mutate(req)
		res, err := e.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode == 401 && res.Header.Get("Date") == "" {
			t.Error("a 401 needs a Date header for the app's clock")
		}
		return res.StatusCode
	}
	var replay http.Header
	if s := signed(func(req *http.Request) { replay = req.Header.Clone() }); s != 200 {
		t.Fatalf("signed: %d", s)
	}
	if s := signed(func(req *http.Request) { req.Header = replay }); s != 401 {
		t.Errorf("replayed nonce: %d", s)
	}
	if s := signed(func(req *http.Request) {
		ts := strconv.FormatInt(time.Now().Add(-2*time.Minute).Unix(), 10)
		nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
		req.Header.Set(actor.TimestampHeader, ts)
		req.Header.Set(actor.NonceHeader, nonce)
		req.Header.Set(actor.SignatureHeader, kit.signRaw(actor.CanonicalRequest("GET", "/api/device", ts, nonce, nil, kit.id.String())))
	}); s != 401 {
		t.Errorf("stale timestamp: %d", s)
	}
	if s := signed(func(req *http.Request) { req.URL.RawQuery = "x=1" }); s != 401 {
		t.Errorf("changed query: %d", s)
	}
	other := &deviceKey{id: kit.id, key: newDeviceKey().key}
	if s := signed(func(req *http.Request) { other.sign(req, nil) }); s != 401 {
		t.Errorf("another key: %d", s)
	}

	// The web view: a one-time link, exchanged for the device cookie; adult cookies go.
	r = e.send(device, "POST", "/api/device/web-session", map[string]any{})
	if r.status != 201 {
		t.Fatalf("web-session: %d %s", r.status, r.raw)
	}
	link := r.body["url"].(string)
	noFollow := *e.http
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	type opened struct {
		status   int
		location string
		cookies  []*http.Cookie
	}
	open := func(path string, cookies ...*http.Cookie) opened {
		t.Helper()
		req, _ := http.NewRequestWithContext(t.Context(), "GET", e.base+path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		res, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return opened{res.StatusCode, res.Header.Get("Location"), res.Cookies()}
	}
	res := open(link, adult.cookie)
	if res.status != 303 || res.location != "/kid" {
		t.Fatalf("exchange: %d %s", res.status, res.location)
	}
	var web *http.Cookie
	cleared := map[string]bool{}
	for _, c := range res.cookies {
		switch {
		case c.Name == actor.DeviceWebCookie:
			web = c
		case c.MaxAge < 0:
			cleared[c.Name] = true
		}
	}
	if web == nil || !web.HttpOnly || !web.Secure || !cleared[actor.AdultCookie] || !cleared[actor.DisplayCookie] {
		t.Fatalf("cookies: %v", res.cookies)
	}
	if r := e.send(adult, "GET", "/api/household", nil); r.status != 401 {
		t.Errorf("the adult session in the web view should be gone: %d", r.status)
	}
	adult = e.house("b", nil).actors["adult"] // household B's adult can't see A
	if res := open(link); res.status != 303 || res.location != "/device/expired" {
		t.Errorf("reused link: %d %s", res.status, res.location)
	}
	kidView := cred{cookie: &http.Cookie{Name: actor.DeviceWebCookie, Value: web.Value}}
	if r := e.send(kidView, "GET", "/api/device/session", nil); r.status != 200 || r.body["child"].(map[string]any)["name"] != "Kit" {
		t.Fatalf("GET /device/session: %d %s", r.status, r.raw)
	}
	// The kid view acts only for its own child.
	if r := e.send(kidView, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil); r.status != 404 {
		t.Errorf("child1's chore from Kit's iPad: %d %s", r.status, r.raw)
	}
	if r := e.send(kidView, "POST", "/api/children/"+h.child1.String()+"/redemptions", map[string]any{"rewardId": h.reward}); r.status != 404 {
		t.Errorf("child1's redemption from Kit's iPad: %d %s", r.status, r.raw)
	}

	// Household A's adult manages it (a fresh session: the old one was signed out above).
	a := cred{cookie: freshSession(e, h.adult)}
	r = e.send(a, "GET", "/api/devices", nil)
	if r.status != 200 || len(r.body["devices"].([]any)) != 3 || r.body["lockMessage"] != nil || r.body["defaultLockMessage"] != DefaultLockMessage {
		t.Fatalf("list: %d %s", r.status, r.raw)
	}
	if r := e.send(a, "PUT", "/api/devices/lock-message", map[string]any{"lockMessage": "  Time for a break.  "}); r.status != 204 {
		t.Fatalf("lock message: %d %s", r.status, r.raw)
	}
	if r := e.send(device, "GET", "/api/device", nil); r.body["household"].(map[string]any)["lockMessage"] != "Time for a break." {
		t.Errorf("lock message on the device: %s", r.raw)
	}
	if r := e.send(a, "PUT", "/api/devices/lock-message", map[string]any{"lockMessage": "   "}); r.status != 422 {
		t.Errorf("blank lock message: %d %s", r.status, r.raw)
	}
	if r := e.send(a, "PUT", "/api/devices/lock-message", map[string]any{"lockMessage": nil}); r.status != 204 {
		t.Errorf("default again: %d %s", r.status, r.raw)
	}
	// Reassigning ends the web session; the device then acts for its new child.
	r = e.send(a, "PATCH", "/api/devices/"+kit.id.String(), map[string]any{"childId": h.child1, "name": " Robin's iPad "})
	if r.status != 200 || r.body["name"] != "Robin's iPad" || r.body["child"].(map[string]any)["name"] != "Robin" {
		t.Fatalf("reassign: %d %s", r.status, r.raw)
	}
	if r := e.send(kidView, "GET", "/api/device/session", nil); r.status != 401 {
		t.Errorf("web session after reassigning: %d", r.status)
	}
	// Archiving its child: listed without a child, and its requests get 401.
	e.exec(`UPDATE child SET archived_at = now() WHERE id = $1`, h.child1)
	if r := e.send(device, "GET", "/api/device", nil); r.status != 401 {
		t.Errorf("archived child: %d", r.status)
	}
	r = e.send(a, "GET", "/api/devices", nil)
	for _, d := range r.body["devices"].([]any) {
		if d := d.(map[string]any); d["id"] == kit.id.String() && d["child"] != nil {
			t.Errorf("archived child still shown: %v", d)
		}
	}
	if r := e.send(a, "PATCH", "/api/devices/"+kit.id.String(), map[string]any{"childId": h.child1}); r.status != 422 {
		t.Errorf("give it to an archived child: %d %s", r.status, r.raw)
	}
	if r := e.send(a, "PATCH", "/api/devices/"+kit.id.String(), map[string]any{"childId": h.child2}); r.status != 200 {
		t.Fatalf("give it to Kit again: %d %s", r.status, r.raw)
	}
	if r := e.send(device, "GET", "/api/device", nil); r.status != 200 {
		t.Errorf("reassigned: %d", r.status)
	}
	// Unpairing: the next request gets 401.
	if r := e.send(a, "DELETE", "/api/devices/"+kit.id.String(), nil); r.status != 204 {
		t.Fatalf("revoke: %d %s", r.status, r.raw)
	}
	if r := e.send(device, "GET", "/api/device", nil); r.status != 401 {
		t.Errorf("revoked: %d", r.status)
	}
	// An expired link goes to /device/expired too.
	if res := open("/device/session?token=" + url.QueryEscape("not-a-token")); res.location != "/device/expired" {
		t.Errorf("bad link: %s", res.location)
	}
	if r := e.send(adult, "GET", "/api/devices", nil); r.status != 200 || len(r.body["devices"].([]any)) != 2 {
		t.Errorf("household B sees only its own: %d %s", r.status, r.raw)
	}
}
