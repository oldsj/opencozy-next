package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/egress"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

// Fictional values only: the stand-in issues these, and nothing here is a real credential.
const (
	fixtureClientID     = "fixture-client.apps.example.test"
	fixtureClientSecret = "fixture-client-secret" //nolint:gosec // G101: a stand-in's value
	fixtureRedirect     = "https://cozy.example.test/api/calendar/google/callback"
	fixtureRefresh      = "fixture-refresh-token" //nolint:gosec // G101: a stand-in's value
)

// standIn plays Google: token, userinfo, revoke, calendarList and events, all in memory.
type standIn struct {
	mu         sync.Mutex
	challenge  string // the PKCE challenge the consent URL carried
	scope      string
	refreshErr string // "invalid_grant" to refuse refresh tokens
	rotate     string // a new refresh token to hand out on the next refresh
	events     []gEvent
	changed    bool // the next sync-token call reports a change
	gone       bool // the next sync-token call answers 410
	syncCalls  int
	fullCalls  int
	revoked    []string
}

func (s *standIn) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	bearer := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer fixture-access" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = r.ParseForm()
		if r.Form.Get("client_id") != fixtureClientID || r.Form.Get("client_secret") != fixtureClientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "fixture-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != s.challenge ||
				r.Form.Get("redirect_uri") != fixtureRedirect {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			writeJSON(w, map[string]any{"access_token": "fixture-access", "refresh_token": fixtureRefresh, "expires_in": 3600,
				"token_type": "Bearer", "scope": s.scope})
		case "refresh_token":
			if s.refreshErr != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": s.refreshErr})
				return
			}
			out := map[string]any{"access_token": "fixture-access", "expires_in": 3600, "token_type": "Bearer"}
			if s.rotate != "" {
				out["refresh_token"], s.rotate = s.rotate, ""
			}
			writeJSON(w, out)
		}
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		if bearer(w, r) {
			writeJSON(w, map[string]string{"sub": "1001", "email": "Parent@Example.test"})
		}
	})
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = r.ParseForm()
		s.revoked = append(s.revoked, r.Form.Get("token"))
	})
	mux.HandleFunc("GET /calendar/v3/users/me/calendarList", func(w http.ResponseWriter, r *http.Request) {
		if bearer(w, r) {
			writeJSON(w, map[string]any{"items": []map[string]any{
				{"id": "family@group.calendar.example.test", "summary": "Family"},
				{"id": "parent@example.test", "summary": "Parent", "primary": true},
				{"id": "old@group.calendar.example.test", "summary": "Old", "deleted": true},
			}})
		}
	})
	mux.HandleFunc("GET /calendar/v3/calendars/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		q := r.URL.Query()
		if r.PathValue("id") != "family@group.calendar.example.test" || q.Get("singleEvents") != "true" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if q.Get("syncToken") != "" {
			s.syncCalls++
			switch {
			case s.gone:
				s.gone = false
				w.WriteHeader(http.StatusGone)
			case s.changed:
				s.changed = false
				writeJSON(w, eventsPage{Items: []gEvent{{ID: "fair", Status: "confirmed"}}, NextSyncToken: "sync-x"})
			default:
				writeJSON(w, eventsPage{NextSyncToken: "sync-same"})
			}
			return
		}
		if q.Get("timeMin") == "" || q.Get("timeMax") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.fullCalls++
		writeJSON(w, eventsPage{Items: s.events, NextSyncToken: "sync-full"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (s *standIn) google(srv *httptest.Server) *Google {
	c := srv.Client()
	ep := endpoints{Auth: "https://accounts.example.test/auth", Token: srv.URL + "/token", Revoke: srv.URL + "/revoke",
		UserInfo: srv.URL + "/userinfo", API: srv.URL + "/calendar/v3"}
	return newGoogle(fixtureClientID, fixtureClientSecret, fixtureRedirect, c, func(ctx context.Context) context.Context {
		return egress.Context(ctx, c)
	}, ep)
}

// The consent URL asks for offline access with PKCE, and the code is exchanged with the
// verifier; a consent without the calendar permission is refused.
func TestGoogleConnect(t *testing.T) {
	s := &standIn{scope: "openid " + GoogleScope + " https://www.googleapis.com/auth/userinfo.email"}
	g := s.google(s.serve(t))
	verifier := "fixture-verifier-0123456789abcdefghijklmnopqrstuvwxyz0123456"
	u, err := url.Parse(g.AuthURL("fixture-state", verifier))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, want := range map[string]string{"state": "fixture-state", "access_type": "offline", "prompt": "consent", "code_challenge_method": "S256",
		"client_id": fixtureClientID, "redirect_uri": fixtureRedirect, "include_granted_scopes": "false", "response_type": "code"} {
		if q.Get(k) != want {
			t.Errorf("consent URL %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if !strings.Contains(q.Get("scope"), GoogleScope) {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	s.challenge = q.Get("code_challenge")
	if _, err := g.Exchange(t.Context(), "fixture-code", "another-verifier-0123456789abcdefghijklmnopqrstuvwxyz012"); err == nil {
		t.Fatal("a code exchanged with the wrong verifier")
	}
	acct, err := g.Exchange(t.Context(), "fixture-code", verifier)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Subject != "1001" || acct.Email != "Parent@Example.test" || acct.RefreshToken != fixtureRefresh {
		t.Errorf("account = %+v", acct)
	}
	s.scope = "openid email"
	if _, err := g.Exchange(t.Context(), "fixture-code", verifier); !errors.Is(err, ErrScopeMissing) {
		t.Errorf("without the calendar scope: err = %v", err)
	}
	cals, err := g.Calendars(t.Context(), "fixture-access")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 || !cals[0].Primary || cals[1].Name != "Family" {
		t.Errorf("calendars = %+v", cals)
	}
	if err := g.Revoke(t.Context(), fixtureRefresh); err != nil || len(s.revoked) != 1 {
		t.Errorf("revoke: %v %v", err, s.revoked)
	}
}

// The real client reaches Google's hosts only.
func TestGoogleClientReachesOnlyGoogle(t *testing.T) {
	g, err := NewGoogle(fixtureClientID, fixtureClientSecret, fixtureRedirect)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	err = g.get(t.Context(), "fixture-access", "https://calendar.example.test/x", &v)
	if !errors.Is(err, egress.ErrRefused) {
		t.Errorf("another host: err = %v, want ErrRefused", err)
	}
}

func timed(id, recurring, start, end, original string) gEvent {
	e := gEvent{ID: id, Status: "confirmed", Summary: "Swimming", RecurringEventID: recurring,
		Start: gTime{DateTime: start}, End: gTime{DateTime: end}}
	if original != "" {
		e.OriginalStartTime = gTime{DateTime: original}
	}
	return e
}

// Google's expanded events become series and keys like an ICS feed's.
func TestGoogleFeed(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	items := []gEvent{
		timed("swim_1", "swim", "2026-09-14T15:30:00-04:00", "2026-09-14T16:30:00-04:00", "2026-09-14T15:30:00-04:00"),
		// moved an hour later: keyed by where it was
		timed("swim_2", "swim", "2026-09-21T16:30:00-04:00", "2026-09-21T17:30:00-04:00", "2026-09-21T15:30:00-04:00"),
		{ID: "swim_3", Status: "cancelled", RecurringEventID: "swim", OriginalStartTime: gTime{DateTime: "2026-09-28T15:30:00-04:00"}},
		{ID: "fair", Status: "tentative", Summary: " Fair\x07 ", Start: gTime{Date: "2026-09-18"}, End: gTime{Date: "2026-09-19"}},
		{ID: "camp_1", Status: "confirmed", RecurringEventID: "camp", Start: gTime{Date: "2026-09-19"}, End: gTime{Date: "2026-09-19"},
			OriginalStartTime: gTime{Date: "2026-09-19"}},
		{ID: "broken", Status: "confirmed", Start: gTime{DateTime: "yesterday"}},
		timed("swim_1", "swim", "2026-09-14T15:30:00-04:00", "2026-09-14T16:30:00-04:00", "2026-09-14T15:30:00-04:00"),
	}
	feed := googleFeed(items, ny)
	got := map[string][]string{}
	for _, s := range feed.Series {
		for _, e := range s.Events {
			got[s.UID] = append(got[s.UID], e.Key+" "+e.String())
		}
	}
	want := map[string][]string{
		"swim": {"20260914T193000Z " + Event{Key: "20260914T193000Z", Start: time.Date(2026, 9, 14, 15, 30, 0, 0, ny), End: time.Date(2026, 9, 14, 16, 30, 0, 0, ny), Title: "Swimming"}.String(),
			"20260921T193000Z " + Event{Key: "20260921T193000Z", Start: time.Date(2026, 9, 21, 16, 30, 0, 0, ny), End: time.Date(2026, 9, 21, 17, 30, 0, 0, ny), Title: "Swimming"}.String()},
		"fair": {"single " + Event{Key: "single", AllDay: true, StartDate: mustDate("2026-09-18"), EndDate: mustDate("2026-09-19"), Title: "Fair", Tentative: true}.String()},
		"camp": {"20260919 " + Event{Key: "20260919", AllDay: true, StartDate: mustDate("2026-09-19"), EndDate: mustDate("2026-09-20")}.String()},
	}
	for uid, w := range want {
		if strings.Join(got[uid], "\n") != strings.Join(w, "\n") {
			t.Errorf("%s:\n got %q\nwant %q", uid, got[uid], w)
		}
	}
	if len(got) != len(want) || feed.Skipped != 1 {
		t.Errorf("series %d, skipped %d", len(got), feed.Skipped)
	}
	for _, s := range feed.Series {
		if s.Recurring != (s.UID != "fair") {
			t.Errorf("%s recurring = %v", s.UID, s.Recurring)
		}
	}
}

// A Google calendar is fetched in full, then only asked whether it changed: unchanged keeps
// the stored events, a change or an expired sync token refetches (keeping event ids and
// assignments), and a refused refresh token marks the account "reconnect needed".
func TestGoogleRefresh(t *testing.T) {
	db := testdb.Open(t)
	ctx := t.Context()
	kr := testKeyring(t)
	s := &standIn{events: []gEvent{
		timed("swim_1", "swim", "2026-09-14T15:30:00-04:00", "2026-09-14T16:30:00-04:00", "2026-09-14T15:30:00-04:00"),
		{ID: "fair", Status: "confirmed", Summary: "Fair", Start: gTime{Date: "2026-09-18"}, End: gTime{Date: "2026-09-19"}},
	}}
	f := Fetcher{google: s.google(s.serve(t))}
	clock := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	log := slog.New(slog.DiscardHandler)

	var h uuid.UUID
	if err := db.Owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Fictional', 'America/New_York') RETURNING id`).Scan(&h); err != nil {
		t.Fatal(err)
	}
	q := dbq.New(db.Owner)
	conn := uuid.Must(uuid.NewV7())
	sealed, err := SealGoogleToken(kr, h, conn, fixtureRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateGoogleConnection(ctx, dbq.CreateGoogleConnectionParams{ID: conn, HouseholdID: h, Subject: "1001",
		Email: "parent@example.test", Scope: GoogleScope, RefreshToken: sealed}); err != nil {
		t.Fatal(err)
	}
	calID := "family@group.calendar.example.test"
	src, err := q.CreateGoogleCalendarSource(ctx, dbq.CreateGoogleCalendarSourceParams{ID: uuid.Must(uuid.NewV7()), HouseholdID: h,
		Name: "Family", Color: "sky", ShowOnDisplay: true, GoogleConnectionID: &conn, GoogleCalendarID: &calID})
	if err != nil {
		t.Fatal(err)
	}
	run := func() {
		t.Helper()
		if _, err := db.Owner.Exec(ctx, `UPDATE calendar_source SET next_sync_at = now() - interval '1 second'`); err != nil {
			t.Fatal(err)
		}
		if err := Refresh(ctx, poolTx{db.Runtime}, kr, f, log, now); err != nil {
			t.Fatal(err)
		}
	}
	titles := func() map[string]string {
		t.Helper()
		rows, err := db.Owner.Query(ctx, `SELECT e.id::text, coalesce(e.title, '') FROM calendar_event e WHERE e.source_id = $1`, src.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for rows.Next() {
			var id, title string
			if err := rows.Scan(&id, &title); err != nil {
				t.Fatal(err)
			}
			out[id] = title
		}
		return out
	}
	state := func() (code string, token string) {
		t.Helper()
		var c, tok *string
		if err := db.Owner.QueryRow(ctx, `SELECT error_code, google_sync_token FROM calendar_source WHERE id = $1`, src.ID).Scan(&c, &tok); err != nil {
			t.Fatal(err)
		}
		return deref(c), deref(tok)
	}

	run()
	first := titles()
	if len(first) != 2 || s.fullCalls != 1 {
		t.Fatalf("after the first refresh: events %v, full fetches %d", first, s.fullCalls)
	}
	if code, tok := state(); code != "" || tok != "sync-full" {
		t.Fatalf("error %q, sync token %q", code, tok)
	}
	var series uuid.UUID
	if err := db.Owner.QueryRow(ctx, `SELECT id FROM calendar_series WHERE source_id = $1 AND uid = 'swim'`, src.ID).Scan(&series); err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertAssignment(ctx, dbq.UpsertAssignmentParams{HouseholdID: h, SeriesID: series, RecurrenceKey: "*"}); err != nil {
		t.Fatal(err)
	}

	run() // unchanged: no full fetch
	if s.syncCalls != 1 || s.fullCalls != 1 {
		t.Errorf("unchanged: sync calls %d, full fetches %d", s.syncCalls, s.fullCalls)
	}
	if _, tok := state(); tok != "sync-same" {
		t.Errorf("sync token %q after an unchanged check", tok)
	}

	s.mu.Lock()
	s.changed = true
	s.events[0].Summary = "Swimming lessons"
	s.mu.Unlock()
	run()
	after := titles()
	if s.fullCalls != 2 || len(after) != 2 {
		t.Fatalf("changed: full fetches %d, events %v", s.fullCalls, after)
	}
	for id := range first {
		if _, ok := after[id]; !ok {
			t.Errorf("event %s lost its id across a refetch", id)
		}
	}
	var assigned int
	if err := db.Owner.QueryRow(ctx, `SELECT count(*) FROM calendar_assignment WHERE series_id = $1`, series).Scan(&assigned); err != nil || assigned != 1 {
		t.Errorf("assignment after a refetch: %d %v", assigned, err)
	}

	s.mu.Lock()
	s.gone = true
	s.rotate = "fixture-rotated-refresh-token"
	s.mu.Unlock()
	run() // 410: refetch, and the rotated refresh token is kept sealed
	if s.fullCalls != 3 {
		t.Errorf("after a 410: full fetches %d", s.fullCalls)
	}
	c, err := q.GetGoogleConnection(ctx, dbq.GetGoogleConnectionParams{HouseholdID: h, ID: conn})
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := OpenGoogleToken(kr, h, conn, c.RefreshToken); err != nil || tok != "fixture-rotated-refresh-token" {
		t.Errorf("rotated token: %q %v", tok, err)
	}
	if strings.Contains(c.RefreshToken, "fixture") {
		t.Error("the refresh token is stored in the clear")
	}

	s.mu.Lock()
	s.refreshErr = "invalid_grant"
	s.mu.Unlock()
	run()
	if code, _ := state(); code != CodeTokenExpired {
		t.Errorf("refused refresh token: error %q", code)
	}
	if c, _ := q.GetGoogleConnection(ctx, dbq.GetGoogleConnectionParams{HouseholdID: h, ID: conn}); !c.NeedsReconnect {
		t.Error("the connection is not marked reconnect needed")
	}
	if len(titles()) != 2 {
		t.Error("the stored events went with the refused token")
	}
	calls := s.fullCalls + s.syncCalls
	run() // reconnect needed: Google is not asked at all
	if s.fullCalls+s.syncCalls != calls {
		t.Error("a connection that needs reconnecting still called Google")
	}
}

func mustDate(s string) day.Date {
	d, err := day.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}
