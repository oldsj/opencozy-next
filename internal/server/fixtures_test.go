package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

// These are integration tests: the real handler stack against a real, migrated PostgreSQL, with
// the server on the runtime role and fixtures written through the schema owner.

type env struct {
	t     *testing.T
	base  string
	http  *http.Client
	srv   *Server
	owner *pgxpool.Pool
}

func newEnv(t *testing.T, mutate func(*config.Config)) *env {
	t.Helper()
	return newEnvWith(t, mutate, nil)
}

// newEnvWith is newEnv with prepare run on the Server before its handler is built.
func newEnvWith(t *testing.T, mutate func(*config.Config), prepare func(*Server)) *env {
	t.Helper()
	db := testdb.Open(t)
	cfg := config.Config{SessionTTL: time.Hour, AuthRatePerMinute: 1000}
	if mutate != nil {
		mutate(&cfg)
	}
	srv := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db.Runtime)
	if prepare != nil {
		prepare(srv)
	}
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(h) // TLS, because the cookies are Secure
	t.Cleanup(ts.Close)
	return &env{t: t, base: ts.URL, http: ts.Client(), srv: srv, owner: db.Owner}
}

// cred is how a request authenticates: a header, a cookie, a device's signature or nothing.
type cred struct {
	bearer string
	cookie *http.Cookie
	device *deviceKey
	// cookies are more cookies in the same jar (a web view holding several).
	cookies []*http.Cookie
}

// deviceKey is a paired kid device's id and private key, to sign requests as the app does.
type deviceKey struct {
	id  uuid.UUID
	key *ecdsa.PrivateKey
}

func newDeviceKey() *deviceKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return &deviceKey{key: k}
}

// publicKey is the uncompressed point, as the app sends it.
func (d *deviceKey) publicKey() []byte {
	b, err := d.key.PublicKey.Bytes()
	if err != nil {
		panic(err)
	}
	return b
}

// signRaw signs msg as the app does: SHA-256, raw r||s, base64url.
func (d *deviceKey) signRaw(msg string) string {
	sum := sha256.Sum256([]byte(msg))
	r, s, err := ecdsa.Sign(rand.Reader, d.key, sum[:])
	if err != nil {
		panic(err)
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return base64.RawURLEncoding.EncodeToString(out)
}

// sign adds the four signature headers for this request.
func (d *deviceKey) sign(req *http.Request, body []byte) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
	req.Header.Set(actor.DeviceHeader, d.id.String())
	req.Header.Set(actor.TimestampHeader, ts)
	req.Header.Set(actor.NonceHeader, nonce)
	req.Header.Set(actor.SignatureHeader, d.signRaw(actor.CanonicalRequest(req.Method, req.URL.RequestURI(), ts, nonce, body, d.id.String())))
}

func (c cred) apply(r *http.Request) {
	if c.bearer != "" {
		r.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	for _, more := range c.cookies {
		r.AddCookie(more)
	}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
	header http.Header
}

func (r resp) code() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

// send makes one request. body is JSON-encoded unless it is already []byte. Changes get a fresh
// Idempotency-Key unless headers set one; setting it to "" sends none.
func (e *env) send(c cred, method, path string, body any, headers ...string) resp {
	e.t.Helper()
	var raw []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		raw = b
	default:
		raw, _ = json.Marshal(b)
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequestWithContext(e.t.Context(), method, e.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "Host" {
			req.Host = headers[i+1]
			continue
		}
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
			continue
		}
		req.Header.Set(headers[i], headers[i+1])
	}
	c.apply(req)
	if c.device != nil {
		c.device.sign(req, raw)
	}
	res, err := e.http.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	got, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(got, &out)
	return resp{status: res.StatusCode, raw: got, body: out, header: res.Header}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.owner.Exec(e.t.Context(), sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func (e *env) id(sql string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.owner.QueryRow(e.t.Context(), sql, args...).Scan(&id); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
	return id
}

// house is one fictional household with an adult, two children and one actor of every kind.
type house struct {
	id, adult, child1, child2, target uuid.UUID // target: a credential to revoke
	screen                            uuid.UUID // a display to unpair (not one of the actors)
	chore, routine, step              uuid.UUID // child1's, on every day
	approval                          uuid.UUID // child1's chore on an earlier day, waiting
	recipe                            uuid.UUID
	entry                             uuid.UUID // an award of 5 points to child1
	reward                            uuid.UUID // costs 3
	redemption                        uuid.UUID // child1 bought the reward; approved, waiting to be given
	calendar, event                   uuid.UUID // an ICS calendar shown on displays, with one event
	google                            uuid.UUID // a Google account that needs connecting again (no token)
	passkey                           uuid.UUID // the adult's only passkey
	device                            uuid.UUID // child2's tablet, to rename or unpair (not one of the actors)
	rule, link                        uuid.UUID // build_house (5 points, 20 a day); child1 linked as Builder_42
	tv, prompt                        uuid.UUID // a TV with one MAC, asking "Who's watching?"
	credit                            uuid.UUID // child1's iPad grant, all used (so no banked time)
	actors                            map[string]cred
}

// actorNames are the actors every house has. Kinds are what Policy.Check compares.
var actorNames = []string{
	"adult", "adult-stale", "phone", "display-today", "display-meals", "display-calendar", "display-tv", "display-none",
	"service-all", "service-none", "service-child1", "assistant", "device", "device-web",
	"revoked", "malformed", "expired", "none",
}

func actorKind(name string) (actor.Kind, bool) {
	switch name {
	case "adult", "adult-stale":
		return actor.Adult, true
	case "phone":
		return actor.Phone, true
	case "display-today", "display-meals", "display-calendar", "display-tv", "display-none":
		return actor.Display, true
	case "service-all", "service-none", "service-child1":
		return actor.Service, true
	case "assistant":
		return actor.Assistant, true
	case "device":
		return actor.Device, true
	case "device-web":
		return actor.DeviceWeb, true
	}
	return "", false // no live credential
}

func (e *env) house(label string, scopes []string) house {
	e.t.Helper()
	h := house{actors: map[string]cred{}}
	if len(scopes) == 0 {
		scopes = []string{"household:read"}
	}
	h.id = e.id(`INSERT INTO household (name, time_zone) VALUES ($1, 'Europe/Amsterdam') RETURNING id`, "Household "+label)
	handle, _ := auth.NewUserHandle()
	h.adult = e.id(`INSERT INTO adult (household_id, email, name, webauthn_user_handle) VALUES ($1, $2, 'Sam', $3) RETURNING id`,
		h.id, "sam."+label+"@example.test", handle)
	h.child1 = e.id(`INSERT INTO child (household_id, name) VALUES ($1, 'Robin') RETURNING id`, h.id)
	h.child2 = e.id(`INSERT INTO child (household_id, name) VALUES ($1, 'Kit') RETURNING id`, h.id)

	session := func(authAgo time.Duration) *http.Cookie {
		tok, hash, _ := auth.NewSessionToken()
		e.exec(`INSERT INTO adult_session (token_hash, adult_id, authenticated_at, expires_at)
			VALUES ($1, $2, now() - $3::interval, now() + interval '1 hour')`, hash, h.adult, authAgo.String())
		return &http.Cookie{Name: actor.AdultCookie, Value: tok}
	}
	h.actors["adult"] = cred{cookie: session(0)}
	h.actors["adult-stale"] = cred{cookie: session(time.Hour)}

	tok, hash, _ := auth.NewToken(auth.PrefixPhone)
	e.exec(`INSERT INTO phone_session (household_id, adult_id, name, token_hash, expires_at)
		VALUES ($1, $2, 'Phone', $3, now() + interval '90 days')`, h.id, h.adult, hash)
	h.actors["phone"] = cred{bearer: tok}

	display := func(scopes []string) cred {
		tok, hash, _ := auth.NewSessionToken()
		e.exec(`INSERT INTO display (household_id, name, scopes, token_hash, expires_at)
			VALUES ($1, 'Hall', $2, $3, now() + interval '1 day')`, h.id, scopes, hash)
		return cred{cookie: &http.Cookie{Name: actor.DisplayCookie, Value: tok}}
	}
	h.actors["display-today"] = display([]string{"today"})
	h.actors["display-meals"] = display([]string{"meals"})
	h.actors["display-calendar"] = display([]string{"calendar"})
	h.actors["display-tv"] = display([]string{"tv"})
	h.actors["display-none"] = display([]string{})
	h.calendar = e.id(`INSERT INTO calendar_source (household_id, provider, name, color, feed_url, feed_url_mac)
		VALUES ($1, 'ics', 'School', 'butter', 'v1.k1.fixture.fixture', 'h1.k1.' || gen_random_uuid()::text) RETURNING id`, h.id)
	series := e.id(`INSERT INTO calendar_series (household_id, source_id, uid, recurring) VALUES ($1, $2, 'picture-day@example.test', false) RETURNING id`, h.id, h.calendar)
	h.event = e.id(`INSERT INTO calendar_event (household_id, source_id, series_id, recurrence_key, all_day, start_date, end_date, title, status)
		VALUES ($1, $2, $3, 'single', true, '2026-09-16', '2026-09-17', 'Picture day', 'confirmed') RETURNING id`, h.id, h.calendar, series)
	h.google = e.id(`INSERT INTO calendar_google_connection (household_id, subject, email, scope, needs_reconnect)
		VALUES ($1, gen_random_uuid()::text, 'parent@example.test', 'openid', true) RETURNING id`, h.id)
	h.rule = e.id(`INSERT INTO award_rule (household_id, activity_key, points, daily_limit) VALUES ($1, 'build_house', 5, 20) RETURNING id`, h.id)
	h.link = e.id(`INSERT INTO learning_link (household_id, child_id, provider, external_id) VALUES ($1, $2, 'minecraft', 'Builder_42') RETURNING id`, h.id, h.child1)
	h.recipe = e.id(`INSERT INTO recipe (household_id, title, servings) VALUES ($1, 'Soup', 2) RETURNING id`, h.id)
	h.screen = e.id(`INSERT INTO display (household_id, name, scopes, expires_at)
		VALUES ($1, 'Spare', '{today}', now() + interval '1 day') RETURNING id`, h.id)
	h.chore = e.id(`INSERT INTO chore (household_id, child_id, title, points) VALUES ($1, $2, 'Feed the cat', 4) RETURNING id`, h.id, h.child1)
	h.routine = e.id(`INSERT INTO routine (household_id, child_id, title, points) VALUES ($1, $2, 'Morning', 3) RETURNING id`, h.id, h.child1)
	occ := e.id(`INSERT INTO occurrence (id, household_id, child_id, kind, definition_id, day, state, points)
		VALUES (gen_random_uuid(), $1, $2, 'chore', $3, '2000-01-03', 'pending', 4) RETURNING id`, h.id, h.child1, h.chore)
	h.approval = e.id(`INSERT INTO approval (household_id, child_id, kind, occurrence_id, title, points)
		VALUES ($1, $2, 'completion', $3, 'Feed the cat', 4) RETURNING id`, h.id, h.child1, occ)
	h.entry = e.id(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id)
		VALUES ($1, $2, 5, 'award', 'fixture', gen_random_uuid()) RETURNING id`, h.id, h.child1)
	e.exec(`INSERT INTO active_award (source_kind, source_id, household_id, ledger_id)
		SELECT source_kind, source_id, household_id, id FROM point_ledger WHERE id = $1`, h.entry)
	h.reward = e.id(`INSERT INTO reward (household_id, title, cost, kind) VALUES ($1, 'Choose a game', 3, 'item') RETURNING id`, h.id)
	// Paid for by an award of its own, days ago, so child1's balance and today's earnings stay as
	// the other tests expect.
	e.exec(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id, created_at)
		VALUES ($1, $2, 3, 'award', 'fixture', gen_random_uuid(), clock_timestamp() - interval '2 days')`, h.id, h.child1)
	h.redemption = e.id(`INSERT INTO redemption (household_id, child_id, reward_id, title, cost, state) VALUES ($1, $2, $3, 'Choose a game', 3, 'approved') RETURNING id`, h.id, h.child1, h.reward)
	spent := e.id(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id)
		VALUES ($1, $2, -3, 'redemption', 'redemption', $3) RETURNING id`, h.id, h.child1, h.redemption)
	e.exec(`UPDATE redemption SET ledger_id = $2 WHERE id = $1`, h.redemption, spent)
	h.passkey = e.id(`INSERT INTO passkey (household_id, adult_id, credential_id, public_key, backup_eligible, backed_up, name) VALUES ($1, $2, decode(md5(random()::text) || md5(random()::text), 'hex'), '\\x00', true, true, 'Laptop') RETURNING id`, h.id, h.adult)
	h.tv = e.id(`INSERT INTO tv (household_id, name) VALUES ($1, 'Den TV') RETURNING id`, h.id)
	e.exec(`INSERT INTO tv_mac (household_id, tv_id, mac) VALUES ($1, $2, '02:00:5e:10:00:01')`, h.id, h.tv)
	h.prompt = e.id(`INSERT INTO tv_prompt (household_id, tv_id, source, created_at) VALUES ($1, $2, 'display', clock_timestamp()) RETURNING id`, h.id, h.tv)
	h.credit = e.id(`INSERT INTO screen_time_credit (household_id, child_id, source, granted_by, minutes, seconds, used_seconds, created_at)
		VALUES ($1, $2, 'grant', $3, 1, 60, 60, now() - interval '2 days') RETURNING id`, h.id, h.child1, h.adult)
	h.step = e.id(`INSERT INTO routine_step (household_id, routine_id, title, position) VALUES ($1, $2, 'Brush teeth', 0) RETURNING id`, h.id, h.routine)

	credential := func(kind string, scopes []string, all bool, children []uuid.UUID, extra string) (cred, uuid.UUID) {
		prefix := auth.PrefixService
		if kind == "assistant" {
			prefix = auth.PrefixAssistant
		}
		tok, hash, _ := auth.NewToken(prefix)
		id := e.id(`INSERT INTO credential (household_id, kind, name, token_hash, scopes, all_children, adult_id)
			VALUES ($1, $2, 'Credential', $3, $4, $5, $6) RETURNING id`, h.id, kind, hash, scopes, all, h.adult)
		for _, c := range children {
			e.exec(`INSERT INTO credential_child (household_id, credential_id, child_id) VALUES ($1, $2, $3)`, h.id, id, c)
		}
		if extra != "" {
			e.exec(`UPDATE credential SET `+extra+` WHERE id = $1`, id)
		}
		return cred{bearer: tok}, id
	}
	var withRule [2]uuid.UUID
	h.actors["service-all"], withRule[0] = credential("service", scopes, true, nil, "")
	h.actors["service-none"], _ = credential("service", []string{"nothing:needed"}, true, nil, "")
	h.actors["service-child1"], withRule[1] = credential("service", scopes, false, []uuid.UUID{h.child1}, "")
	for _, id := range withRule {
		e.exec(`INSERT INTO credential_award_rule (household_id, credential_id, rule_id) VALUES ($1, $2, $3)`, h.id, id, h.rule)
	}
	h.actors["assistant"], _ = credential("assistant", scopes, true, nil, "")
	h.actors["revoked"], _ = credential("service", scopes, true, nil, "revoked_at = now()")
	h.actors["expired"], _ = credential("service", scopes, true, nil, "expires_at = now() - interval '1 minute'")
	_, h.target = credential("service", scopes, true, nil, "")
	// child1's iPad signs requests; its web view has a device web session.
	ipad := newDeviceKey()
	ipad.id = e.id(`INSERT INTO device (household_id, child_id, name, platform, public_key) VALUES ($1, $2, 'iPad', 'ios', $3) RETURNING id`, h.id, h.child1, ipad.publicKey())
	h.actors["device"] = cred{device: ipad}
	webTok, webHash, _ := auth.NewSessionToken()
	e.exec(`INSERT INTO device_web_session (token_hash, device_id, expires_at) VALUES ($1, $2, now() + interval '1 day')`, webHash, ipad.id)
	h.actors["device-web"] = cred{cookie: &http.Cookie{Name: actor.DeviceWebCookie, Value: webTok}}
	h.device = e.id(`INSERT INTO device (household_id, child_id, name, platform, public_key) VALUES ($1, $2, 'Tablet', 'ios', $3) RETURNING id`, h.id, h.child2, newDeviceKey().publicKey())
	h.actors["malformed"] = cred{bearer: "ocz_not-a-real-token"}
	h.actors["none"] = cred{}
	return h
}

// fingerprint is a digest of every row that belongs to a household, to show a request changed
// nothing there.
func (e *env) fingerprint(household uuid.UUID) string {
	e.t.Helper()
	ctx := e.t.Context()
	rows, err := e.owner.Query(ctx, `SELECT table_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'household_id' ORDER BY table_name`)
	if err != nil {
		e.t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		e.t.Fatal(err)
	}
	var out bytes.Buffer
	digest := func(sql string) {
		var s *string
		if err := e.owner.QueryRow(ctx, sql, household).Scan(&s); err != nil {
			e.t.Fatalf("%s: %v", sql, err)
		}
		if s != nil {
			out.WriteString(*s)
		}
		out.WriteByte('|')
	}
	for _, t := range tables {
		name := pgx.Identifier{t}.Sanitize()
		digest(`SELECT md5(string_agg(x::text, ',' ORDER BY x::text)) FROM ` + name + ` x WHERE household_id = $1`)
	}
	digest(`SELECT md5(string_agg(x::text, ',' ORDER BY x::text)) FROM household x WHERE id = $1`)
	digest(`SELECT md5(string_agg(x::text, ',' ORDER BY x::text)) FROM adult_session x
		WHERE adult_id IN (SELECT id FROM adult WHERE household_id = $1)`)
	return out.String()
}
