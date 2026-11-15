package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/listen"
)

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		host, origin string
		cookie, want bool
	}{
		{"app.example.test", "https://app.example.test", true, true},
		{"app.example.test", "HTTPS://APP.example.test", true, true},
		{"app.example.test:8443", "https://app.example.test:8443", true, true},
		// A sibling subdomain shares SameSite=Lax cookies, so it must be refused.
		{"app.example.test", "https://evil.example.test", true, false},
		{"app.example.test", "https://example.test", true, false},
		{"app.example.test", "https://app.example.test.evil.test", true, false},
		{"app.example.test", "http://app.example.test", true, false},
		{"app.example.test", "https://app.example.test:444", true, false},
		{"app.example.test", "null", true, false},
		// A cookie needs an Origin; a bearer client may send none.
		{"app.example.test", "", true, false},
		{"app.example.test", "", false, true},
		{"app.example.test", "https://evil.example.test", false, false},
		// Plain http only on a loopback host, for development.
		{"localhost:5173", "http://localhost:5173", true, true},
		{"127.0.0.1:8080", "http://127.0.0.1:8080", true, true},
		{"[::1]:8080", "http://[::1]:8080", true, true},
		{"localhost:5173", "http://localhost:5174", true, false},
		{"", "https://", true, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/live", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := OriginAllowed(r, c.cookie); got != c.want {
			t.Errorf("host %q origin %q cookie %v: got %v", c.host, c.origin, c.cookie, got)
		}
	}
}

type hubEnv struct {
	hub   *Hub
	notes chan listen.Note
	url   string
	// recheck answers each socket's recheck.
	recheck func(actor.Actor) (bool, error)
}

func newHub(t *testing.T, a func(r *http.Request) actor.Actor, recheck ...func(actor.Actor) (bool, error)) *hubEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e := &hubEnv{hub: NewHub(slog.New(slog.NewTextHandler(io.Discard, nil))), notes: make(chan listen.Note, 8),
		recheck: func(actor.Actor) (bool, error) { return true, nil }}
	if len(recheck) > 0 {
		e.hub.recheck = 50 * time.Millisecond
		e.recheck = recheck[0]
	}
	go e.hub.Run(ctx, e.notes)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hub.Serve(w, r, a(r), func(_ context.Context, a actor.Actor) (actor.Actor, bool, error) {
			ok, err := e.recheck(a)
			return a, ok, err
		})
	}))
	t.Cleanup(func() { cancel(); ts.Close() })
	e.url = ts.URL
	return e
}

func (e *hubEnv) dial(t *testing.T, origin string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, e.url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func (e *hubEnv) notify(h uuid.UUID, topic Topic, child *uuid.UUID) {
	b, _ := json.Marshal(note{Household: h, Topic: topic, ChildID: child})
	e.notes <- listen.Note{Payload: string(b)}
}

func read(t *testing.T, c *websocket.Conn) (message, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		return message{}, err
	}
	var m message
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("bad message %s", b)
	}
	return m, nil
}

// waitSockets waits until the hub has registered n sockets.
func (e *hubEnv) waitSockets(t *testing.T, n int) {
	t.Helper()
	for range 200 {
		e.hub.mu.Lock()
		got := 0
		for _, set := range e.hub.socks {
			got += len(set)
		}
		e.hub.mu.Unlock()
		if got == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub never reached %d sockets", n)
}

func TestHubDeliversToItsHouseholdOnly(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	e := newHub(t, func(r *http.Request) actor.Actor {
		if r.URL.Query().Get("h") == "b" {
			return actor.Actor{Kind: actor.Phone, HouseholdID: b}
		}
		return actor.Actor{Kind: actor.Phone, HouseholdID: a}
	})
	ca := e.dial(t, "")
	url := e.url
	e.url = url + "?h=b"
	cb := e.dial(t, "")
	e.waitSockets(t, 2)

	child := uuid.New()
	e.notify(a, Today, &child)
	m, err := read(t, ca)
	if err != nil || m.Topic != Today || m.ChildID == nil || *m.ChildID != child {
		t.Fatalf("household A got %+v, %v", m, err)
	}
	// B gets nothing for A's event: the next thing B sees is its own.
	e.notify(b, Children, nil)
	if m, err := read(t, cb); err != nil || m.Topic != Children {
		t.Fatalf("household B got %+v, %v", m, err)
	}
	// A resync goes to everyone.
	e.notes <- listen.Note{Resync: true}
	for _, c := range []*websocket.Conn{ca, cb} {
		if m, err := read(t, c); err != nil || m.Topic != Resync {
			t.Fatalf("resync: %+v, %v", m, err)
		}
	}
}

func TestHubDisplaySeesOnlyItsScopes(t *testing.T) {
	h := uuid.New()
	e := newHub(t, func(*http.Request) actor.Actor {
		return actor.Actor{Kind: actor.Display, HouseholdID: h, Scopes: []string{"today"}}
	})
	c := e.dial(t, strings.Replace(e.url, "http://", "http://", 1))
	e.waitSockets(t, 1)
	e.notify(h, Credentials, nil)
	e.notify(h, TV, nil)
	e.notify(h, Today, nil)
	if m, err := read(t, c); err != nil || m.Topic != Today {
		t.Fatalf("display got %+v, %v; want only today", m, err)
	}
}

func TestHubRefusals(t *testing.T) {
	e := newHub(t, func(*http.Request) actor.Actor { return actor.Actor{Kind: actor.Adult, HouseholdID: uuid.New()} })

	// Not an upgrade.
	resp, err := http.Get(e.url)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("plain GET: %d", resp.StatusCode)
	}
	// A cookie actor from a sibling origin, or with no Origin at all.
	for _, origin := range []string{"http://evil.example.test", ""} {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, resp, err := websocket.Dial(ctx, e.url, &websocket.DialOptions{HTTPHeader: h})
		cancel()
		if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Errorf("origin %q: err %v, resp %v", origin, err, resp)
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	// The same origin (loopback http) is accepted.
	e.dial(t, e.url)
}

func TestHubClosesOnRecheck(t *testing.T) {
	for name, answer := range map[string]struct {
		ok   bool
		err  error
		code websocket.StatusCode
	}{
		"revoked":     {false, nil, closeRevoked},
		"unavailable": {false, errors.New("db down"), closeUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			e := newHub(t, func(*http.Request) actor.Actor { return actor.Actor{Kind: actor.Phone, HouseholdID: uuid.New()} },
				func(actor.Actor) (bool, error) { return answer.ok, answer.err })
			c := e.dial(t, "")
			_, err := read(t, c)
			if got := websocket.CloseStatus(err); got != answer.code {
				t.Fatalf("close status %v (%v), want %v", got, err, answer.code)
			}
		})
	}
}

func TestHubKicksSlowSockets(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	// A registered socket whose writer has stalled: nothing drains its queue.
	s := &socket{actor: actor.Actor{Kind: actor.Phone, HouseholdID: uuid.New()}, out: make(chan message, queueLen), kicked: make(chan struct{})}
	if !hub.add(s) {
		t.Fatal("add")
	}
	for range queueLen {
		hub.broadcast(func(uuid.UUID) bool { return true }, message{Topic: Today})
	}
	select {
	case <-s.kicked:
		t.Fatal("kicked with room left in the queue")
	default:
	}
	hub.broadcast(func(uuid.UUID) bool { return true }, message{Topic: Today})
	select {
	case <-s.kicked:
	default:
		t.Fatal("a socket that fell behind was not kicked")
	}
}

func TestHubCapsSocketsPerHousehold(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := uuid.New()
	for i := range maxPerHousehold + 1 {
		ok := hub.add(&socket{actor: actor.Actor{HouseholdID: h}})
		if ok != (i < maxPerHousehold) {
			t.Fatalf("socket %d: add = %v", i, ok)
		}
	}
	if !hub.add(&socket{actor: actor.Actor{HouseholdID: uuid.New()}}) {
		t.Fatal("another household was refused")
	}
}

func TestNotifyRefusesUnknownTopics(t *testing.T) {
	for _, topic := range []Topic{Resync, "secret", ""} {
		if err := Notify(context.Background(), nil, uuid.New(), Event{Topic: topic}); err == nil {
			t.Errorf("topic %q accepted", topic)
		}
	}
}
