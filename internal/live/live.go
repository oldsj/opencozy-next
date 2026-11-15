// Package live is /api/live, the same-origin WebSocket that tells clients what to refetch
// (design §8). A writing transaction calls Notify, which is pg_notify('opencozy_live', …):
// PostgreSQL delivers it only if that transaction commits. Each replica LISTENs (internal/listen)
// and fans events out to its own sockets in the event's household. Messages carry a topic and at
// most a child ID, never names or values.
package live

import (
	"context"
	"encoding/json"

	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/httpx"
	"github.com/oldsj/opencozy-next/internal/listen"
)

// Channel is the NOTIFY channel for live events.
const Channel = "opencozy_live"

// Topic says what changed. The display topics are display scopes: a display receives a topic
// only when it holds that scope.
type Topic string

// Topics.
const (
	Resync      Topic = "resync" // refetch everything; sent by the server, not by Notify
	Household   Topic = "household"
	Children    Topic = "children"
	Credentials Topic = "credentials"
	Today       Topic = "today"
	TV          Topic = "tv"
	Calendar    Topic = "calendar"
	Meals       Topic = "meals"
	Dashboard   Topic = "dashboard"
)

var topics = []Topic{Household, Children, Credentials, Today, TV, Calendar, Meals, Dashboard}

// Event is one change.
type Event struct {
	Topic   Topic
	ChildID *uuid.UUID
}

// note is the NOTIFY payload.
type note struct {
	Household uuid.UUID  `json:"h"`
	Topic     Topic      `json:"t"`
	ChildID   *uuid.UUID `json:"c,omitempty"`
}

// message is what a socket receives.
type message struct {
	Topic   Topic      `json:"topic"`
	ChildID *uuid.UUID `json:"childId,omitempty"`
}

// Notify sends e to the household's sockets when q's transaction commits.
func Notify(ctx context.Context, q *dbq.Queries, householdID uuid.UUID, e Event) error {
	if !slices.Contains(topics, e.Topic) {
		return fmt.Errorf("live: unknown topic %q", e.Topic)
	}
	b, err := json.Marshal(note{Household: householdID, Topic: e.Topic, ChildID: e.ChildID})
	if err != nil {
		return err
	}
	return q.NotifyLive(ctx, string(b))
}

// Recheck re-validates a socket's actor: the credential and the operation's policy. It returns
// false when the socket must close.
type Recheck func(ctx context.Context, a actor.Actor) (actor.Actor, bool, error)

// Limits.
const (
	// recheckEvery is how often a socket's credential is checked again (design §3, rule 11).
	recheckEvery     = 60 * time.Second
	pingEvery        = 30 * time.Second
	writeTimeout     = 10 * time.Second
	queueLen         = 32
	maxPerHousehold  = 100
	clientReadLimit  = 512
	closeRevoked     = websocket.StatusPolicyViolation
	closeTooSlow     = websocket.StatusPolicyViolation
	closeUnavailable = websocket.StatusTryAgainLater
)

// Hub holds this replica's sockets.
type Hub struct {
	log     *slog.Logger
	recheck time.Duration
	mu      sync.Mutex
	socks   map[uuid.UUID]map[*socket]struct{}
	done    chan struct{}
}

type socket struct {
	mu     sync.Mutex
	actor  actor.Actor
	out    chan message
	kicked chan struct{}
	once   sync.Once
}

func (s *socket) kick() { s.once.Do(func() { close(s.kicked) }) }

func (s *socket) allows(t Topic) bool {
	if t == Resync {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.actor.Kind == actor.Display {
		return slices.Contains(s.actor.Scopes, string(t))
	}
	return true
}

// NewHub makes an empty hub.
func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log, recheck: recheckEvery, socks: map[uuid.UUID]map[*socket]struct{}{}, done: make(chan struct{})}
}

// Run delivers notes until ctx ends, then closes every socket.
func (h *Hub) Run(ctx context.Context, notes <-chan listen.Note) {
	defer close(h.done)
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-notes:
			if n.Resync {
				h.broadcast(func(uuid.UUID) bool { return true }, message{Topic: Resync})
				continue
			}
			var e note
			if err := json.Unmarshal([]byte(n.Payload), &e); err != nil || !slices.Contains(topics, e.Topic) {
				h.log.WarnContext(ctx, "live: bad notification")
				continue
			}
			h.broadcast(func(id uuid.UUID) bool { return id == e.Household }, message{Topic: e.Topic, ChildID: e.ChildID})
		}
	}
}

func (h *Hub) broadcast(match func(uuid.UUID) bool, m message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, set := range h.socks {
		if !match(id) {
			continue
		}
		for s := range set {
			if !s.allows(m.Topic) {
				continue
			}
			select {
			case s.out <- m:
			default:
				s.kick() // too far behind: the client reconnects and refetches
			}
		}
	}
}

func (h *Hub) stopped() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

func (h *Hub) add(s *socket) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := s.actor.HouseholdID
	if len(h.socks[id]) >= maxPerHousehold {
		return false
	}
	if h.socks[id] == nil {
		h.socks[id] = map[*socket]struct{}{}
	}
	h.socks[id][s] = struct{}{}
	return true
}

func (h *Hub) remove(s *socket) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := s.actor.HouseholdID
	delete(h.socks[id], s)
	if len(h.socks[id]) == 0 {
		delete(h.socks, id)
	}
}

// cookieAuth reports whether the actor kind authenticates by cookie, which a browser attaches
// to a cross-site WebSocket handshake.
func cookieAuth(k actor.Kind) bool {
	return k == actor.Adult || k == actor.Display || k == actor.DeviceWeb
}

// OriginAllowed is the exact-origin check. The handshake is a GET, which
// http.CrossOriginProtection does not check, and SameSite=Lax does not stop it from a sibling
// subdomain. So the Origin must be exactly this app's origin: https and the request's Host, or
// http on a loopback host for development. An absent Origin (a native client) is accepted only
// when no cookie authenticated the request.
func OriginAllowed(r *http.Request, cookie bool) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return !cookie
	}
	if r.Host == "" {
		return false
	}
	if strings.EqualFold(o, "https://"+r.Host) {
		return true
	}
	return loopback(r.Host) && strings.EqualFold(o, "http://"+r.Host)
}

func loopback(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func upgradeRequest(r *http.Request) bool {
	return headerHas(r.Header, "Connection", "upgrade") && headerHas(r.Header, "Upgrade", "websocket")
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// Serve upgrades r for actor a, which the authorization middleware has already allowed, and
// sends events until the socket closes. It returns when the socket is done. A nil Hub (live
// updates not started) refuses upgrades with 503.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, a actor.Actor, recheck Recheck) {
	if !upgradeRequest(r) {
		w.Header().Set("Upgrade", "websocket")
		httpx.WriteError(w, http.StatusUpgradeRequired, "upgrade_required", "Open this as a WebSocket.")
		return
	}
	if !OriginAllowed(r, cookieAuth(a.Kind)) {
		httpx.WriteError(w, http.StatusForbidden, "cross_origin", "Cross-origin request refused.")
		return
	}
	if h == nil || h.stopped() {
		httpx.WriteError(w, http.StatusServiceUnavailable, "unavailable", "Try again shortly.")
		return
	}
	s := &socket{actor: a, out: make(chan message, queueLen), kicked: make(chan struct{})}
	if !h.add(s) {
		httpx.WriteError(w, http.StatusTooManyRequests, "too_many_sockets", "Too many live connections for this household.")
		return
	}
	defer h.remove(s)

	// The server's read and write timeouts would otherwise cut the socket off.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	// OriginPatterns stays empty, so the library's own check (Origin host equals Host) runs too.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return // Accept has written the refusal
	}
	c.SetReadLimit(clientReadLimit)
	// Clients only listen. CloseRead answers pings and closes the socket on any data message.
	ctx := c.CloseRead(r.Context())

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	re := time.NewTicker(h.recheck)
	defer re.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = c.CloseNow()
			return
		case <-h.done:
			_ = c.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case <-s.kicked:
			_ = c.Close(closeTooSlow, "too slow; reconnect")
			return
		case m := <-s.out:
			if err := write(ctx, c, m); err != nil {
				_ = c.CloseNow()
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				_ = c.CloseNow()
				return
			}
		case <-re.C:
			s.mu.Lock()
			cur := s.actor
			s.mu.Unlock()
			fresh, ok, err := recheck(ctx, cur)
			switch {
			case err != nil:
				h.log.WarnContext(ctx, "live: recheck", "err", err)
				_ = c.Close(closeUnavailable, "try again")
				return
			case !ok:
				_ = c.Close(closeRevoked, "credential no longer valid")
				return
			}
			s.mu.Lock()
			s.actor = fresh
			s.mu.Unlock()
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b)
}
