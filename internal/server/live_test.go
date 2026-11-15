package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/live"
)

// dialLive opens /api/live as c. It returns the handshake's status and, for a refusal, the
// error code.
func (e *env) dialLive(c cred, origin string) (conn *websocket.Conn, status int, code string, err error) {
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	c.apply(r)
	for k, v := range r.Header {
		h[k] = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, strings.Replace(e.base, "https://", "wss://", 1)+"/api/live",
		&websocket.DialOptions{HTTPClient: e.http, HTTPHeader: h})
	if conn != nil {
		e.t.Cleanup(func() { _ = conn.CloseNow() })
	}
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			var body struct{ Error struct{ Code string } }
			_ = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			code = body.Error.Code
		}
	}
	return conn, status, code, err
}

// An event written in a committed transaction reaches the household's sockets through
// PostgreSQL; the upgrade goes through the whole middleware chain.
func TestLiveEndToEnd(t *testing.T) {
	e := newEnv(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.srv.StartLive(ctx)
	a := e.house("a", nil)

	adult, status, _, err := e.dialLive(a.actors["adult"], e.base)
	if err != nil {
		t.Fatalf("adult, same origin: %v %d", err, status)
	}
	phone, status, _, err := e.dialLive(a.actors["phone"], "")
	if err != nil {
		t.Fatalf("phone, no Origin: %v %d", err, status)
	}

	child := a.child1
	got := func(c *websocket.Conn) bool {
		// The listener may not be LISTENing yet, so notify until the event arrives.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if err := live.Notify(context.Background(), dbq.New(e.owner), a.id, live.Event{Topic: live.Today, ChildID: &child}); err != nil {
				t.Fatal(err)
			}
			rctx, rcancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			_, b, err := c.Read(rctx)
			rcancel()
			if err != nil {
				if websocket.CloseStatus(err) != -1 {
					t.Fatalf("socket closed: %v", err)
				}
				continue
			}
			var m struct {
				Topic   string `json:"topic"`
				ChildID string `json:"childId"`
			}
			_ = json.Unmarshal(b, &m)
			if m.Topic == "today" && m.ChildID == child.String() {
				return true
			}
		}
		return false
	}
	if !got(adult) || !got(phone) {
		t.Fatal("an event never arrived")
	}
}

func TestLiveRefusesOtherOrigins(t *testing.T) {
	e := newEnv(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.srv.StartLive(ctx)
	a := e.house("a", nil)
	sibling := strings.Replace(e.base, "127.0.0.1", "evil.127.0.0.1.nip.test", 1)
	for name, c := range map[string]struct {
		who, origin string
	}{
		"cookie, sibling origin": {"adult", sibling},
		"cookie, no origin":      {"adult", ""},
		"display, other port":    {"display-today", "https://127.0.0.1:1"},
		"bearer, sibling origin": {"phone", sibling},
	} {
		_, status, code, err := e.dialLive(a.actors[c.who], c.origin)
		if err == nil || status != http.StatusForbidden || code != "cross_origin" {
			t.Errorf("%s: err %v, status %d, code %q", name, err, status, code)
		}
	}
	// Who may open it at all is the matrix test's job; a display without scopes is one case.
	if _, status, code, err := e.dialLive(a.actors["display-none"], e.base); err == nil || status != http.StatusForbidden || code != "forbidden" {
		t.Errorf("display-none: %v %d %q", err, status, code)
	}
}
