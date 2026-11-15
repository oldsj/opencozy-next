package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/oldsj/opencozy-next/internal/calendar"
)

// fakeGoogle stands in for Google's consent, tokens and calendar list. Fictional values only.
type fakeGoogle struct {
	mu        sync.Mutex
	verifiers map[string]string // state → verifier, from the consent URLs handed out
	acct      calendar.GoogleAccount
	exchange  error
	access    error
	revoked   []string
}

func (f *fakeGoogle) AuthURL(state, verifier string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifiers[state] = verifier
	return "https://accounts.example.test/auth?" + url.Values{"state": {state}}.Encode()
}

func (f *fakeGoogle) Exchange(_ context.Context, code, verifier string) (calendar.GoogleAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := false
	for _, v := range f.verifiers {
		ok = ok || v == verifier
	}
	if code != "fixture-code" || !ok {
		return calendar.GoogleAccount{}, errors.New("bad code or verifier")
	}
	return f.acct, f.exchange
}

func (f *fakeGoogle) Access(context.Context, string) (calendar.Access, error) {
	return calendar.Access{Token: "fixture-access"}, f.access
}

func (f *fakeGoogle) Calendars(context.Context, string) ([]calendar.GoogleCalendar, error) {
	return []calendar.GoogleCalendar{{ID: "parent@example.test", Name: "Parent", Primary: true},
		{ID: "family@group.calendar.example.test", Name: "Family"}}, nil
}

func (f *fakeGoogle) Revoke(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, token)
	return nil
}

// callback is Google sending the browser back: a GET with the session cookie (SameSite=Lax
// lets it through on a top-level navigation), not followed past the redirect.
func (e *env) callback(c cred, query url.Values) string {
	e.t.Helper()
	req, _ := http.NewRequestWithContext(e.t.Context(), http.MethodGet, e.base+"/api/calendar/google/callback?"+query.Encode(), nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	c.apply(req)
	client := *e.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		return res.Status
	}
	return res.Header.Get("Location")
}

func TestGoogleConnectFlow(t *testing.T) {
	// Without a Google client the household is told so.
	bare := newEnv(t, withKeys(t))
	bh := bare.house("off", nil)
	if r := bare.send(bh.actors["adult"], "POST", "/api/calendar/google/connect", nil); r.status != 422 || r.code() != "not_configured" {
		t.Fatalf("off: %d %s", r.status, r.raw)
	}
	if r := bare.send(bh.actors["adult"], "GET", "/api/calendar/google/connections", nil); r.status != 200 || r.body["enabled"] != false {
		t.Fatalf("off list: %d %s", r.status, r.raw)
	}

	e := newEnv(t, withKeys(t))
	g := &fakeGoogle{verifiers: map[string]string{}, acct: calendar.GoogleAccount{Subject: "1001", Email: "Parent@Example.test",
		Scope: calendar.GoogleScope, RefreshToken: "fixture-google-refresh"}}
	e.srv.google = g
	h, other := e.house("a", nil), e.house("b", nil)
	adult := h.actors["adult"]
	start := func() string {
		t.Helper()
		r := e.send(adult, "POST", "/api/calendar/google/connect", nil)
		if r.status != 200 {
			t.Fatalf("connect: %d %s", r.status, r.raw)
		}
		u, _ := url.Parse(r.body["authorizeUrl"].(string))
		return u.Query().Get("state")
	}
	if r := e.send(h.actors["phone"], "POST", "/api/calendar/google/connect", nil); r.status != 403 {
		t.Errorf("phone connect: %d", r.status)
	}

	state := start()
	ok := url.Values{"state": {state}, "code": {"fixture-code"}}
	if loc := e.callback(cred{}, ok); !strings.Contains(loc, "401") {
		t.Errorf("signed out: %s", loc)
	}
	if loc := e.callback(other.actors["adult"], ok); loc != "/calendars?google=expired" {
		t.Errorf("another household's adult: %s", loc)
	}
	loc := e.callback(adult, ok)
	if !strings.HasPrefix(loc, "/calendars?connection=") || !strings.HasSuffix(loc, "&google=connected") {
		t.Fatalf("connect: %s", loc)
	}
	u, _ := url.Parse(loc)
	conn := u.Query().Get("connection")
	if loc := e.callback(adult, ok); loc != "/calendars?google=expired" {
		t.Errorf("the state used twice: %s", loc)
	}
	var stored string
	if err := e.owner.QueryRow(t.Context(), `SELECT row_to_json(c)::text FROM calendar_google_connection c WHERE id = $1`, conn).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "fixture-google-refresh") || !strings.Contains(stored, `"refresh_token":"v1.k1.`) {
		t.Fatalf("stored: %s", stored)
	}
	var states int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM google_oauth_state`).Scan(&states); err != nil || states != 0 {
		t.Errorf("connects in progress left: %d %v", states, err)
	}

	if loc := e.callback(adult, url.Values{"state": {start()}, "error": {"access_denied"}}); loc != "/calendars?google=denied" {
		t.Errorf("declined: %s", loc)
	}
	g.exchange = calendar.ErrScopeMissing
	if loc := e.callback(adult, url.Values{"state": {start()}, "code": {"fixture-code"}}); loc != "/calendars?google=scope_missing" {
		t.Errorf("no calendar permission: %s", loc)
	}
	g.exchange = nil
	// The same account again replaces the token on the same connection.
	if loc := e.callback(adult, url.Values{"state": {start()}, "code": {"fixture-code"}}); !strings.Contains(loc, conn) {
		t.Errorf("reconnect: %s", loc)
	}

	list := e.send(h.actors["phone"], "GET", "/api/calendar/google/connections", nil)
	cs := list.body["connections"].([]any)
	if list.body["enabled"] != true || len(cs) != 1 || cs[0].(map[string]any)["email"] != "parent@example.test" ||
		cs[0].(map[string]any)["version"] != float64(2) || strings.Contains(string(list.raw), "fixture") {
		t.Fatalf("connections: %s", list.raw)
	}
	if r := e.send(other.actors["adult"], "GET", "/api/calendar/google/connections/"+conn+"/calendars", nil); r.status != 404 {
		t.Errorf("another household's account: %d", r.status)
	}
	cals := e.send(adult, "GET", "/api/calendar/google/connections/"+conn+"/calendars", nil)
	if cals.status != 200 || len(cals.body["calendars"].([]any)) != 2 {
		t.Fatalf("calendars: %d %s", cals.status, cals.raw)
	}
	follow := map[string]any{"googleId": "family@group.calendar.example.test", "name": "Family", "color": "sky", "showOnDisplay": true}
	r := e.send(adult, "POST", "/api/calendar/google/connections/"+conn+"/calendars", follow)
	if r.status != 201 || r.body["provider"] != "google" || r.body["googleConnectionId"] != conn {
		t.Fatalf("follow: %d %s", r.status, r.raw)
	}
	source := r.body["id"].(string)
	if r := e.send(adult, "POST", "/api/calendar/google/connections/"+conn+"/calendars", follow); r.status != 409 || r.code() != "calendar_exists" {
		t.Errorf("follow twice: %d %s", r.status, r.raw)
	}
	cals = e.send(adult, "GET", "/api/calendar/google/connections/"+conn+"/calendars", nil)
	if !strings.Contains(string(cals.raw), `"calendarId":"`+source+`"`) {
		t.Errorf("followed calendar not marked: %s", cals.raw)
	}

	// Google refuses the token: reconnect needed, and nothing new can be followed.
	g.access = calendar.ErrReconnect
	if r := e.send(adult, "GET", "/api/calendar/google/connections/"+conn+"/calendars", nil); r.status != 422 || r.code() != "reconnect_needed" {
		t.Errorf("refused token: %d %s", r.status, r.raw)
	}
	follow["googleId"] = "parent@example.test"
	if r := e.send(adult, "POST", "/api/calendar/google/connections/"+conn+"/calendars", follow); r.status != 422 || r.code() != "reconnect_needed" {
		t.Errorf("follow while reconnect needed: %d %s", r.status, r.raw)
	}

	if r := e.send(adult, "DELETE", "/api/calendar/google/connections/"+conn+"?expectedVersion=1", nil); r.status != 409 {
		t.Errorf("stale disconnect: %d", r.status)
	}
	if r := e.send(adult, "DELETE", "/api/calendar/google/connections/"+conn+"?expectedVersion=2", nil); r.status != 204 {
		t.Fatalf("disconnect: %d %s", r.status, r.raw)
	}
	if len(g.revoked) != 1 || g.revoked[0] != "fixture-google-refresh" {
		t.Errorf("revoked: %v", g.revoked)
	}
	var left int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM calendar_source WHERE id = $1`, source).Scan(&left); err != nil || left != 0 {
		t.Errorf("the account's calendars outlived it: %d %v", left, err)
	}
}
