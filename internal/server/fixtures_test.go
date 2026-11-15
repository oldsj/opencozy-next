package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	db := testdb.Open(t)
	cfg := config.Config{SessionTTL: time.Hour, AuthRatePerMinute: 1000}
	if mutate != nil {
		mutate(&cfg)
	}
	srv := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db.Runtime)
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(h) // TLS, because the cookies are Secure
	t.Cleanup(ts.Close)
	return &env{t: t, base: ts.URL, http: ts.Client(), srv: srv, owner: db.Owner}
}

// cred is how a request authenticates: a header, a cookie or nothing.
type cred struct {
	bearer string
	cookie *http.Cookie
}

func (c cred) apply(r *http.Request) {
	if c.bearer != "" {
		r.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
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

// send makes one request. body is JSON-encoded unless it is already []byte.
func (e *env) send(c cred, method, path string, body any, headers ...string) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequestWithContext(e.t.Context(), method, e.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	c.apply(req)
	res, err := e.http.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp{status: res.StatusCode, raw: raw, body: out, header: res.Header}
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
	actors                            map[string]cred
}

// actorNames are the actors every house has. Kinds are what Policy.Check compares.
var actorNames = []string{
	"adult", "adult-stale", "phone", "display-today", "display-none",
	"service-all", "service-none", "service-child1", "assistant",
	"revoked", "malformed", "expired", "none",
}

func actorKind(name string) (actor.Kind, bool) {
	switch name {
	case "adult", "adult-stale":
		return actor.Adult, true
	case "phone":
		return actor.Phone, true
	case "display-today", "display-none":
		return actor.Display, true
	case "service-all", "service-none", "service-child1":
		return actor.Service, true
	case "assistant":
		return actor.Assistant, true
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
	h.actors["display-none"] = display([]string{})

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
	h.actors["service-all"], _ = credential("service", scopes, true, nil, "")
	h.actors["service-none"], _ = credential("service", []string{"nothing:needed"}, true, nil, "")
	h.actors["service-child1"], _ = credential("service", scopes, false, []uuid.UUID{h.child1}, "")
	h.actors["assistant"], _ = credential("assistant", scopes, true, nil, "")
	h.actors["revoked"], _ = credential("service", scopes, true, nil, "revoked_at = now()")
	h.actors["expired"], _ = credential("service", scopes, true, nil, "expires_at = now() - interval '1 minute'")
	_, h.target = credential("service", scopes, true, nil, "")
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
