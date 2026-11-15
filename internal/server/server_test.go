package server

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/scrypt"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/day"
)

const password = "a long enough password"

// withPassword gives household h's adult a password and returns their email.
func (e *env) withPassword(h house, scheme, hash string) string {
	e.t.Helper()
	var email string
	if err := e.owner.QueryRow(e.t.Context(), `UPDATE adult SET password_hash = $2, password_scheme = $3 WHERE id = $1 RETURNING email`,
		h.adult, hash, scheme).Scan(&email); err != nil {
		e.t.Fatal(err)
	}
	return email
}

// scryptBA makes a hash in the old app's format: hex salt (used as text), scrypt N=16384 r=16 p=1.
func scryptBA(t *testing.T, saltHex, pw string) string {
	t.Helper()
	key, err := scrypt.Key([]byte(pw), []byte(saltHex), 16384, 16, 1, 64)
	if err != nil {
		t.Fatal(err)
	}
	return saltHex + ":" + hex.EncodeToString(key)
}

func sessionFrom(t *testing.T, r resp) cred {
	t.Helper()
	for _, c := range (&http.Response{Header: r.header}).Cookies() {
		if c.Name == actor.AdultCookie && c.Value != "" {
			return cred{cookie: &http.Cookie{Name: c.Name, Value: c.Value}}
		}
	}
	t.Fatalf("no session cookie in %v", r.header)
	return cred{}
}

func TestSignInFlow(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	email := e.withPassword(h, auth.SchemeArgon2id, hash)

	if r := e.send(cred{}, "POST", "/api/auth/login", map[string]string{"email": email, "password": "wrong password"}); r.status != 401 || r.code() != "invalid_credentials" {
		t.Fatalf("wrong password: %d %s", r.status, r.raw)
	}
	if r := e.send(cred{}, "POST", "/api/auth/login", map[string]string{"email": "nobody@example.test", "password": password}); r.status != 401 || r.code() != "invalid_credentials" {
		t.Fatalf("unknown email: %d %s", r.status, r.raw)
	}
	r := e.send(cred{}, "POST", "/api/auth/login", map[string]string{"email": strings.ToUpper(email), "password": password})
	if r.status != 200 {
		t.Fatalf("login: %d %s", r.status, r.raw)
	}
	setCookie := r.header.Get("Set-Cookie")
	for _, want := range []string{"__Host-session=", "HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(setCookie, want) {
			t.Errorf("Set-Cookie %q lacks %s", setCookie, want)
		}
	}
	s := sessionFrom(t, r)
	if r := e.send(s, "GET", "/api/auth/me", nil); r.status != 200 || r.body["household"].(map[string]any)["id"] != h.id.String() {
		t.Fatalf("me: %d %s", r.status, r.raw)
	}
	if r := e.send(s, "POST", "/api/auth/confirm", map[string]string{"password": "wrong password"}); r.status != 403 || r.code() != "wrong_password" {
		t.Fatalf("confirm, wrong password: %d %s", r.status, r.raw)
	}
	if r := e.send(s, "POST", "/api/auth/confirm", map[string]string{"password": password}); r.status != 204 {
		t.Fatalf("confirm: %d %s", r.status, r.raw)
	}
	if r := e.send(s, "POST", "/api/auth/logout", nil); r.status != 204 {
		t.Fatalf("logout: %d %s", r.status, r.raw)
	}
	if r := e.send(s, "GET", "/api/auth/me", nil); r.status != 401 {
		t.Fatalf("me after logout: %d %s", r.status, r.raw)
	}
	// A dead cookie does not stop a sign-in.
	if r := e.send(s, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}); r.status != 200 {
		t.Fatalf("login with a stale cookie: %d %s", r.status, r.raw)
	}
}

func TestLoginRehashesOldScheme(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	// An scrypt hash in the old app's format, of `password`, made with a fixed fictional salt.
	email := e.withPassword(h, auth.SchemeScryptBA, scryptBA(t, "00112233445566778899aabbccddeeff", password))
	if r := e.send(cred{}, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}); r.status != 200 {
		t.Fatalf("login: %d %s", r.status, r.raw)
	}
	var scheme string
	if err := e.owner.QueryRow(t.Context(), `SELECT password_scheme FROM adult WHERE id = $1`, h.adult).Scan(&scheme); err != nil {
		t.Fatal(err)
	}
	if scheme != auth.SchemeArgon2id {
		t.Fatalf("scheme after login = %q", scheme)
	}
	if r := e.send(cred{}, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}); r.status != 200 {
		t.Fatalf("login after rehash: %d %s", r.status, r.raw)
	}
}

func TestChildrenFlow(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", e.srv.table.Scopes())
	adult := h.actors["adult"]
	r := e.send(adult, "POST", "/api/children", map[string]string{"name": "Ash"})
	if r.status != 201 || r.body["version"] != float64(1) {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	id := r.body["id"].(string)
	if r := e.send(adult, "PATCH", "/api/children/"+id, map[string]any{"name": "Ash", "archived": true, "expectedVersion": 1}); r.status != 200 || r.body["archived"] != true {
		t.Fatalf("archive: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "PATCH", "/api/children/"+id, map[string]any{"name": "Ash", "archived": false, "expectedVersion": 1}); r.status != 409 || r.code() != "version_conflict" {
		t.Fatalf("stale version: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, "GET", "/api/children", nil); len(r.body["items"].([]any)) != 2 {
		t.Fatalf("list hides archived: %s", r.raw)
	}
	if r := e.send(adult, "GET", "/api/children?includeArchived=true", nil); len(r.body["items"].([]any)) != 3 {
		t.Fatalf("list with archived: %s", r.raw)
	}
	// A credential limited to one child sees only that child.
	if r := e.send(h.actors["service-child1"], "GET", "/api/children", nil); len(r.body["items"].([]any)) != 1 || r.body["items"].([]any)[0].(map[string]any)["id"] != h.child1.String() {
		t.Fatalf("limited list: %s", r.raw)
	}
}

// Two adults adding the last allowed child at once: the household lock lets exactly one in.
func TestChildLimitUnderConcurrency(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	for i := 2; i < maxChildren-1; i++ {
		e.exec(`INSERT INTO child (household_id, name) VALUES ($1, 'Extra')`, h.id)
	}
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Go(func() {
			codes[i] = e.send(h.actors["adult"], "POST", "/api/children", map[string]string{"name": "Last"}).status
		})
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == 201 {
			created++
		} else if c != 409 {
			t.Errorf("unexpected status %d", c)
		}
	}
	if created != 1 {
		t.Fatalf("created %d children past the limit (statuses %v)", created, codes)
	}
}

func TestIdempotency(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	key := []string{"Idempotency-Key", "add-ash-1"}
	first := e.send(h.actors["adult"], "POST", "/api/children", map[string]string{"name": "Ash"}, key...)
	if first.status != 201 {
		t.Fatalf("first: %d %s", first.status, first.raw)
	}
	again := e.send(h.actors["adult"], "POST", "/api/children", map[string]string{"name": "Ash"}, key...)
	if again.status != 201 || string(again.raw) != string(first.raw) || again.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s", again.status, again.raw)
	}
	var n int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM child WHERE household_id = $1 AND name = 'Ash'`, h.id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("children named Ash: %d %v", n, err)
	}
	if r := e.send(h.actors["adult"], "POST", "/api/children", map[string]string{"name": "Other"}, key...); r.status != 422 || r.code() != "idempotency_key_reused" {
		t.Fatalf("same key, other body: %d %s", r.status, r.raw)
	}
	// The actor is part of the hash: another credential in the household cannot replay it.
	if r := e.send(h.actors["phone"], "POST", "/api/children", map[string]string{"name": "Ash"}, key...); r.status != 422 {
		t.Fatalf("same key, other actor: %d %s", r.status, r.raw)
	}
	// A 404 or 409 is recorded too, and changes nothing.
	conflict := []string{"Idempotency-Key", "stale-1"}
	body := map[string]any{"name": "Robin", "archived": true, "expectedVersion": 7}
	if r := e.send(h.actors["adult"], "PATCH", "/api/children/"+h.child1.String(), body, conflict...); r.status != 409 {
		t.Fatalf("stale: %d %s", r.status, r.raw)
	}
	if r := e.send(h.actors["adult"], "PATCH", "/api/children/"+h.child1.String(), body, conflict...); r.status != 409 || r.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("stale replay: %d %s", r.status, r.raw)
	}
}

func TestCredentials(t *testing.T) {
	e := newEnv(t, nil)
	scopes := e.srv.table.Scopes()
	a, b := e.house("a", scopes), e.house("b", scopes)
	adult := a.actors["adult"]
	r := e.send(adult, "POST", "/api/credentials", map[string]any{"kind": "service", "name": "Router", "scopes": []string{"children:read"}, "childIds": []string{a.child2.String()}})
	if r.status != 201 {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	secret := r.body["secret"].(string)
	svc := cred{bearer: secret}
	if !strings.HasPrefix(secret, auth.PrefixService) {
		t.Errorf("secret %q", secret)
	}
	if r := e.send(svc, "GET", "/api/children", nil); r.status != 200 || len(r.body["items"].([]any)) != 1 {
		t.Fatalf("new credential lists: %d %s", r.status, r.raw)
	}
	if r := e.send(svc, "GET", "/api/household", nil); r.status != 403 {
		t.Fatalf("scope not granted: %d %s", r.status, r.raw)
	}
	for name, body := range map[string]map[string]any{
		"unknown scope":       {"kind": "service", "name": "x", "scopes": []string{"everything"}},
		"other household kid": {"kind": "service", "name": "x", "scopes": []string{"children:read"}, "childIds": []string{b.child1.String()}},
		"both":                {"kind": "service", "name": "x", "scopes": []string{"children:read"}, "allChildren": true, "childIds": []string{a.child1.String()}},
	} {
		if r := e.send(adult, "POST", "/api/credentials", body); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	id := r.body["credential"].(map[string]any)["id"].(string)
	if r := e.send(adult, "DELETE", "/api/credentials/"+id, nil); r.status != 204 {
		t.Fatalf("revoke: %d %s", r.status, r.raw)
	}
	if r := e.send(svc, "GET", "/api/children", nil); r.status != 401 {
		t.Fatalf("revoked credential: %d %s", r.status, r.raw)
	}
	// A service secret presented as an assistant's is refused, like any bad credential.
	forged := cred{bearer: auth.PrefixAssistant + strings.TrimPrefix(secret, auth.PrefixService)}
	if r := e.send(forged, "GET", "/api/credential", nil); r.status != 401 {
		t.Fatalf("prefix swap: %d %s", r.status, r.raw)
	}
}

func TestValidationAndRouting(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	for name, body := range map[string]any{
		"bad email":     map[string]string{"email": "nope", "password": password},
		"unknown field": map[string]any{"email": "a@example.test", "password": password, "admin": true},
	} {
		if r := e.send(cred{}, "POST", "/api/auth/login", body); r.status != 400 || r.code() != "invalid_request" {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	if r := e.send(h.actors["adult"], "POST", "/api/children", map[string]string{"name": "   "}); r.status != 400 {
		t.Errorf("blank name: %d %s", r.status, r.raw)
	}
	if r := e.send(cred{}, "GET", "/api/nope", nil); r.status != 404 || r.code() != "not_found" {
		t.Errorf("unknown endpoint: %d %s", r.status, r.raw)
	}
	if r := e.send(cred{}, "PUT", "/api/household", nil); r.status != 405 {
		t.Errorf("unknown method: %d %s", r.status, r.raw)
	}
}

// The body cap applies before authentication: an anonymous oversized body is a 413, never read.
func TestBodyCapBeforeAuth(t *testing.T) {
	e := newEnv(t, nil)
	big := []byte(`{"name":"` + strings.Repeat("x", 2000) + `"}`)
	if r := e.send(cred{}, "POST", "/api/children", big); r.status != 413 {
		t.Fatalf("oversized, signed out: %d %s", r.status, r.raw)
	}
	if r := e.send(cred{bearer: "ocz_bad"}, "POST", "/api/children", big); r.status != 413 {
		t.Fatalf("oversized, bad credential: %d %s", r.status, r.raw)
	}
}

func TestLoginRateLimited(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.AuthRatePerMinute = 2 })
	bad := map[string]string{"email": "x@example.test", "password": "wrong password!!"}
	e.send(cred{}, "POST", "/api/auth/login", bad)
	e.send(cred{}, "POST", "/api/auth/login", bad)
	if r := e.send(cred{}, "POST", "/api/auth/login", bad); r.status != 429 || r.code() != "rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatalf("third attempt: %d %s", r.status, r.raw)
	}
}

func TestCrossOriginRefused(t *testing.T) {
	e := newEnv(t, nil)
	if r := e.send(cred{}, "POST", "/api/auth/login", []byte(`{}`), "Sec-Fetch-Site", "cross-site"); r.status != 403 {
		t.Fatalf("cross-site POST: %d", r.status)
	}
}

func TestProbesAndHeaders(t *testing.T) {
	e := newEnv(t, nil)
	for _, p := range []string{"/healthz", "/readyz"} {
		res, err := e.http.Get(e.base + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != 204 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
	r := e.send(cred{}, "GET", "/api/auth/me", nil)
	if r.header.Get("Content-Security-Policy") == "" || r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("API headers: CSP=%q Cache-Control=%q", r.header.Get("Content-Security-Policy"), r.header.Get("Cache-Control"))
	}
}

// The runtime role reads and writes rows; it cannot touch the schema or goose's bookkeeping.
func TestRuntimeRoleIsRowsOnly(t *testing.T) {
	e := newEnv(t, nil)
	for _, sql := range []string{
		`CREATE TABLE intruder (id int)`,
		`DROP TABLE child`,
		`DELETE FROM goose_db_version`,
		`SELECT * FROM goose_db_version`,
	} {
		if _, err := e.srv.pool.Exec(t.Context(), sql); err == nil {
			t.Errorf("runtime role ran %q", sql)
		}
	}
}

func TestCommandsNeedAnIdempotencyKey(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	if r := e.send(h.actors["adult"], "POST", "/api/children", map[string]string{"name": "Ash"}, "Idempotency-Key", ""); r.status != 400 || r.code() != "idempotency_key_required" {
		t.Fatalf("no key: %d %s", r.status, r.raw)
	}
}

// A new credential's secret is shown once and never stored: not in the command log either.
func TestCredentialSecretNeverStored(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", e.srv.table.Scopes())
	key := []string{"Idempotency-Key", "issue-router-1"}
	body := map[string]any{"kind": "service", "name": "Router", "scopes": []string{"children:read"}, "allChildren": true}
	r := e.send(h.actors["adult"], "POST", "/api/credentials", body, key...)
	if r.status != 201 {
		t.Fatalf("create: %d %s", r.status, r.raw)
	}
	secret := r.body["secret"].(string)
	var hits int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM command_log x
		WHERE position(convert_to($1, 'UTF8') in x.body) > 0 OR x::text LIKE '%' || $1 || '%'`, secret).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatal("the secret is in command_log")
	}
	again := e.send(h.actors["adult"], "POST", "/api/credentials", body, key...)
	if again.status != 409 || again.code() != "already_done" || strings.Contains(string(again.raw), secret) {
		t.Fatalf("replay: %d %s", again.status, again.raw)
	}
	var n int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM credential WHERE household_id = $1 AND name = 'Router'`, h.id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("credentials named Router: %d %v", n, err)
	}
}

// A credential revoked while its command waits for the household lock stops the command: the
// re-check after the lock sees the revocation (design rule 4).
func TestRevokedWhileWaiting(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	ctx := t.Context()
	var role string
	if err := e.srv.pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	tx, err := e.owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	if _, err := tx.Exec(ctx, `SELECT id FROM household WHERE id = $1 FOR UPDATE`, h.id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE phone_session SET revoked_at = now() WHERE household_id = $1`, h.id); err != nil {
		t.Fatal(err)
	}
	done := make(chan resp, 1)
	go func() {
		done <- e.send(h.actors["phone"], "POST", "/api/children", map[string]string{"name": "Waiting"})
	}()
	waiting := false
	for range 200 {
		var n int
		if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE usename = $1 AND wait_event_type = 'Lock'`, role).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("the command never waited for the household lock")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.status != 401 {
		t.Fatalf("revoked while waiting: %d %s", r.status, r.raw)
	}
	var n int
	if err := e.owner.QueryRow(ctx, `SELECT count(*) FROM child WHERE household_id = $1 AND name = 'Waiting'`, h.id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("children named Waiting: %d %v", n, err)
	}
}

// A wall display asks for a code, an adult enters it, and the display collects its session.
func TestDisplayPairing(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	none := cred{}

	start := e.send(none, "POST", "/api/display/pairings", nil)
	if start.status != 201 {
		t.Fatalf("start: %d %s", start.status, start.raw)
	}
	code := start.body["code"].(string)
	var pairing *http.Cookie
	for _, c := range (&http.Response{Header: start.header}).Cookies() {
		if c.Name == PairingCookie {
			pairing = c
		}
	}
	if pairing == nil || !pairing.HttpOnly || !pairing.Secure || pairing.MaxAge != 600 {
		t.Fatalf("pairing cookie: %+v", pairing)
	}
	claimer := cred{cookie: &http.Cookie{Name: PairingCookie, Value: pairing.Value}}

	if r := e.send(claimer, "POST", "/api/display/pairings/claim", nil); r.status != 202 {
		t.Fatalf("claim before approval: %d %s", r.status, r.raw)
	}
	if r := e.send(none, "POST", "/api/display/pairings/claim", nil); r.status != 404 || r.code() != "pairing_expired" {
		t.Fatalf("claim without the cookie: %d %s", r.status, r.raw)
	}
	pair := func(c cred, code string) resp {
		return e.send(c, "POST", "/api/displays", map[string]any{"code": code, "name": "Kitchen", "scopes": []string{"today"}})
	}
	if r := pair(h.actors["adult"], "ZZZZZZ"); r.status != 404 || r.code() != "no_such_code" {
		t.Fatalf("wrong code: %d %s", r.status, r.raw)
	}
	if r := pair(h.actors["adult-stale"], code); r.status != 403 || r.code() != "reauthentication_required" {
		t.Fatalf("stale sign-in: %d %s", r.status, r.raw)
	}
	// Typed loosely: lower case, with spaces.
	created := pair(h.actors["adult"], strings.ToLower(code[:3]+" "+code[3:]))
	if created.status != 201 {
		t.Fatalf("pair: %d %s", created.status, created.raw)
	}
	if r := pair(h.actors["adult"], code); r.status != 404 {
		t.Fatalf("the code works twice: %d %s", r.status, r.raw)
	}

	claim := e.send(claimer, "POST", "/api/display/pairings/claim", nil)
	if claim.status != 200 || claim.body["name"] != "Kitchen" {
		t.Fatalf("claim: %d %s", claim.status, claim.raw)
	}
	var session *http.Cookie
	for _, c := range (&http.Response{Header: claim.header}).Cookies() {
		if c.Name == actor.DisplayCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.MaxAge < 300*24*3600 {
		t.Fatalf("display cookie: %+v", session)
	}
	if r := e.send(claimer, "POST", "/api/display/pairings/claim", nil); r.status != 404 {
		t.Fatalf("second claim: %d %s", r.status, r.raw)
	}
	display := cred{cookie: &http.Cookie{Name: actor.DisplayCookie, Value: session.Value}}
	if r := e.send(display, "GET", "/api/household", nil); r.status != 200 || r.body["id"] != h.id.String() {
		t.Fatalf("the display reads its household: %d %s", r.status, r.raw)
	}

	list := e.send(h.actors["adult"], "GET", "/api/displays", nil)
	if !strings.Contains(string(list.raw), "Kitchen") {
		t.Fatalf("list: %s", list.raw)
	}
	id := created.body["id"].(string)
	if r := e.send(h.actors["adult"], "DELETE", "/api/displays/"+id, nil); r.status != 204 {
		t.Fatalf("unpair: %d %s", r.status, r.raw)
	}
	if r := e.send(display, "GET", "/api/household", nil); r.status != 401 {
		t.Fatalf("after unpairing: %d %s", r.status, r.raw)
	}
}

// An expired code cannot be entered, and its display cannot claim.
func TestDisplayPairingExpires(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	start := e.send(cred{}, "POST", "/api/display/pairings", nil)
	e.exec(`UPDATE display_pairing SET expires_at = now() - interval '1 second'`)
	if r := e.send(h.actors["adult"], "POST", "/api/displays", map[string]any{"code": start.body["code"], "name": "Hall", "scopes": []string{"today"}}); r.status != 404 {
		t.Fatalf("expired code: %d %s", r.status, r.raw)
	}
}

// A display polling for its pairing does not use up sign-in attempts from the same address.
func TestPairingDoesNotStarveSignIn(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.AuthRatePerMinute = 2 })
	start := e.send(cred{}, "POST", "/api/display/pairings", nil)
	var claim cred
	for _, c := range (&http.Response{Header: start.header}).Cookies() {
		if c.Name == PairingCookie {
			claim = cred{cookie: c}
		}
	}
	for i := range 40 { // two minutes of polling at once
		if r := e.send(claim, "POST", "/api/display/pairings/claim", nil); r.status != 202 {
			t.Fatalf("poll %d: %d %s", i, r.status, r.raw)
		}
	}
	bad := map[string]string{"email": "x@example.test", "password": "wrong password!!"}
	if r := e.send(cred{}, "POST", "/api/auth/login", bad); r.status != 401 {
		t.Fatalf("sign-in after polling: %d %s", r.status, r.raw)
	}
}

// The wall display shows today's chores and routines; a child marks them done, and each waits
// for an adult's approval.
func TestToday(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult, wall := h.actors["adult"], h.actors["display-today"]

	var zone string
	if err := e.owner.QueryRow(t.Context(), `SELECT time_zone FROM household WHERE id = $1`, h.id).Scan(&zone); err != nil {
		t.Fatal(err)
	}
	loc, err := day.Zone(zone)
	if err != nil {
		t.Fatal(err)
	}
	today := day.Of(time.Now(), loc).Weekday()
	notToday := []int{}
	for d := range 7 {
		if d != int(today) {
			notToday = append(notToday, d)
		}
	}
	later := e.send(adult, "POST", "/api/chores", map[string]any{"childId": h.child2, "title": "Water plants", "points": 2, "days": notToday})
	if later.status != 201 {
		t.Fatalf("create chore: %d %s", later.status, later.raw)
	}
	routine := e.send(adult, "POST", "/api/routines", map[string]any{"childId": h.child2, "title": "Bedtime", "points": 5, "steps": []string{"Pyjamas", "Brush teeth"}})
	if routine.status != 201 {
		t.Fatalf("create routine: %d %s", routine.status, routine.raw)
	}
	rid := routine.body["id"].(string)
	steps := routine.body["steps"].([]any)
	step := func(i int) string { return steps[i].(map[string]any)["id"].(string) }

	if r := e.send(h.actors["display-none"], "GET", "/api/today", nil); r.status != 403 {
		t.Fatalf("display without today: %d", r.status)
	}
	if r := e.send(wall, "POST", "/api/today/chores/"+later.body["id"].(string)+"/complete", nil); r.status != 409 || r.code() != "not_today" {
		t.Fatalf("a chore not on today: %d %s", r.status, r.raw)
	}

	done := e.send(wall, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil)
	if done.status != 200 || done.body["state"] != "pending" {
		t.Fatalf("complete: %d %s", done.status, done.raw)
	}
	if r := e.send(wall, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil); r.status != 409 || r.code() != "already_done" {
		t.Fatalf("complete twice: %d %s", r.status, r.raw)
	}

	check := func(i int) resp {
		return e.send(wall, "POST", "/api/today/routines/"+rid+"/steps/"+step(i)+"/check", nil)
	}
	if r := check(0); r.status != 200 || r.body["pending"] != false {
		t.Fatalf("first step: %d %s", r.status, r.raw)
	}
	if r := check(0); r.status != 200 {
		t.Fatalf("the same step again: %d %s", r.status, r.raw)
	}
	if r := check(1); r.status != 200 || r.body["pending"] != true {
		t.Fatalf("last step: %d %s", r.status, r.raw)
	}
	if r := check(1); r.status != 409 || r.code() != "already_done" {
		t.Fatalf("after the routine is done: %d %s", r.status, r.raw)
	}

	var approvals int
	if err := e.owner.QueryRow(t.Context(), `SELECT count(*) FROM approval WHERE household_id = $1 AND state = 'pending' AND id <> $2`, h.id, h.approval).Scan(&approvals); err != nil {
		t.Fatal(err)
	}
	if approvals != 2 {
		t.Fatalf("pending approvals: %d", approvals)
	}

	got := e.send(wall, "GET", "/api/today", nil)
	if got.status != 200 {
		t.Fatalf("today: %d %s", got.status, got.raw)
	}
	byChild := map[string]map[string]any{}
	for _, c := range got.body["children"].([]any) {
		c := c.(map[string]any)["child"].(map[string]any)
		byChild[c["id"].(string)] = c
	}
	c2 := byChild[h.child2.String()]
	if c2 == nil || c2["completed"] != float64(2) || c2["total"] != float64(2) || c2["color"] == "" {
		t.Fatalf("child 2 today: %v", c2)
	}

	if r := e.send(adult, "DELETE", "/api/chores/"+h.chore.String(), nil); r.status != 204 {
		t.Fatalf("archive: %d %s", r.status, r.raw)
	}
	if r := e.send(wall, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil); r.status != 404 {
		t.Fatalf("an archived chore: %d %s", r.status, r.raw)
	}
}

// A child's completion waits in the inbox; an adult approves it (points land) or sends it back
// (the child sees why and can do it again). An adult's own completion is approved at once.
func TestInbox(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult, wall := h.actors["adult"], h.actors["display-today"]
	points := func(child uuid.UUID) float64 {
		r := e.send(wall, "GET", "/api/today", nil)
		for _, c := range r.body["children"].([]any) {
			c := c.(map[string]any)
			if ch := c["child"].(map[string]any); ch["id"] == child.String() {
				return ch["points"].(float64)
			}
		}
		t.Fatalf("child %s not on today", child)
		return 0
	}
	inbox := func() []any {
		r := e.send(adult, "GET", "/api/inbox", nil)
		if r.status != 200 {
			t.Fatalf("inbox: %d %s", r.status, r.raw)
		}
		return r.body["items"].([]any)
	}
	decide := func(id any, how string, body any) resp {
		return e.send(adult, "POST", fmt.Sprintf("/api/inbox/%v/%s", id, how), body)
	}
	find := func(title string) map[string]any {
		for _, it := range inbox() {
			if it := it.(map[string]any); it["title"] == title {
				return it
			}
		}
		return nil
	}

	// The fixture's earlier chore is already waiting; decide it out of the way.
	if r := decide(h.approval, "reject", map[string]any{}); r.status != 200 {
		t.Fatalf("reject the fixture's: %d %s", r.status, r.raw)
	}

	if r := e.send(wall, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil); r.status != 200 {
		t.Fatalf("complete: %d %s", r.status, r.raw)
	}
	if r := e.send(wall, "POST", "/api/today/routines/"+h.routine.String()+"/steps/"+h.step.String()+"/check", nil); r.status != 200 {
		t.Fatalf("routine: %d %s", r.status, r.raw)
	}
	items := inbox()
	if len(items) != 2 {
		t.Fatalf("inbox: %v", items)
	}
	chore, routine := find("Feed the cat"), find("Morning")
	if chore == nil || chore["kind"] != "completion" || chore["points"] != float64(4) ||
		chore["child"].(map[string]any)["id"] != h.child1.String() || routine == nil || routine["kind"] != "routine" {
		t.Fatalf("inbox items: %v", items)
	}

	before := points(h.child1)
	if r := decide(chore["id"], "approve", map[string]any{}); r.status != 200 || r.body["state"] != "approved" {
		t.Fatalf("approve: %d %s", r.status, r.raw)
	}
	if got := points(h.child1); got != before+4 {
		t.Fatalf("points after approval: %v, want %v", got, before+4)
	}
	if r := decide(chore["id"], "approve", map[string]any{}); r.status != 409 || r.code() != "already_decided" {
		t.Fatalf("approve twice: %d %s", r.status, r.raw)
	}
	if r := decide(chore["id"], "reject", map[string]any{}); r.status != 409 {
		t.Fatalf("reject after approval: %d %s", r.status, r.raw)
	}

	reason := "The bed still needs making."
	if r := decide(routine["id"], "reject", map[string]any{"reason": reason}); r.status != 200 || r.body["state"] != "rejected" {
		t.Fatalf("reject: %d %s", r.status, r.raw)
	}
	if got := points(h.child1); got != before+4 {
		t.Fatalf("points after rejection: %v", got)
	}
	again := e.send(wall, "POST", "/api/today/routines/"+h.routine.String()+"/steps/"+h.step.String()+"/check", nil)
	if again.status != 200 || again.body["pending"] != true {
		t.Fatalf("try again after rejection: %d %s", again.status, again.raw)
	}
	if n := len(inbox()); n != 1 {
		t.Fatalf("inbox after trying again: %d items", n)
	}

	if r := decide(uuid.New(), "approve", map[string]any{}); r.status != 404 {
		t.Fatalf("unknown item: %d", r.status)
	}
	if r := decide(chore["id"], "reject", map[string]any{"reason": strings.Repeat("x", 201)}); r.status != 400 && r.status != 422 {
		t.Fatalf("a long reason: %d", r.status)
	}

	// An adult marking a chore done approves it on the spot.
	extra := e.send(adult, "POST", "/api/chores", map[string]any{"childId": h.child2, "title": "Tidy up", "points": 3})
	before2 := points(h.child2)
	if r := e.send(adult, "POST", "/api/today/chores/"+extra.body["id"].(string)+"/complete", nil); r.status != 200 || r.body["state"] != "complete" {
		t.Fatalf("an adult's completion: %d %s", r.status, r.raw)
	}
	if got := points(h.child2); got != before2+3 {
		t.Fatalf("points after an adult's completion: %v, want %v", got, before2+3)
	}
	if n := len(inbox()); n != 1 {
		t.Fatalf("an adult's completion went to the inbox: %d items", n)
	}
}

// Points history and taking points back: once per award, not after they are spent, and the
// chore is open again afterwards.
func TestPointsHistory(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	adult := h.actors["adult"]
	history := func(child uuid.UUID) resp {
		r := e.send(adult, "GET", "/api/children/"+child.String()+"/history", nil)
		if r.status != 200 {
			t.Fatalf("history: %d %s", r.status, r.raw)
		}
		return r
	}
	entries := func(r resp) []map[string]any {
		var out []map[string]any
		for _, x := range r.body["entries"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	reverse := func(id any) resp {
		return e.send(adult, "POST", fmt.Sprintf("/api/points/%v/reverse", id), map[string]any{"reason": "Marked done by mistake."})
	}
	complete := func() {
		if r := e.send(adult, "POST", "/api/today/chores/"+h.chore.String()+"/complete", nil); r.status != 200 || r.body["state"] != "complete" {
			t.Fatalf("complete: %d %s", r.status, r.raw)
		}
	}

	start := history(h.child1).body["balance"].(float64) // the fixture's 5
	complete()
	got := history(h.child1)
	first := entries(got)[0]
	if got.body["balance"] != start+4 || first["kind"] != "earned" || first["points"] != float64(4) || first["title"] != "Feed the cat" || first["by"] == nil {
		t.Fatalf("after earning: %s", got.raw)
	}

	r := reverse(first["id"])
	if r.status != 200 || r.body["kind"] != "reversed" || r.body["points"] != float64(-4) || r.body["reverses"] != first["id"] || r.body["reason"] != "Marked done by mistake." {
		t.Fatalf("reverse: %d %s", r.status, r.raw)
	}
	got = history(h.child1)
	if got.body["balance"] != start || entries(got)[1]["reversedBy"] != r.body["id"] {
		t.Fatalf("after reversing: %s", got.raw)
	}
	if r := reverse(first["id"]); r.status != 409 || r.code() != "already_reversed" {
		t.Fatalf("reverse twice: %d %s", r.status, r.raw)
	}

	// The chore is open again; done again, it pays again, and the old award stays reversed.
	complete()
	second := entries(history(h.child1))[0]
	if r := reverse(first["id"]); r.status != 409 || r.code() != "already_reversed" {
		t.Fatalf("reverse the old award after a new one: %d %s", r.status, r.raw)
	}
	if r := reverse(r.body["id"]); r.status != 409 || r.code() != "not_an_award" {
		t.Fatalf("reverse a reversal: %d %s", r.status, r.raw)
	}

	// Spent: the child redeemed everything, so the award can't be taken back.
	e.exec(`INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id)
		VALUES ($1, $2, $3, 'redemption', 'redemption', gen_random_uuid())`, h.id, h.child1, -int(start+4))
	if r := reverse(second["id"]); r.status != 409 || r.code() != "points_spent" {
		t.Fatalf("reverse spent points: %d %s", r.status, r.raw)
	}
	if b := history(h.child1).body["balance"]; b != float64(0) {
		t.Fatalf("balance after a refused reversal: %v", b)
	}

	if r := e.send(adult, "GET", "/api/children/"+uuid.NewString()+"/history", nil); r.status != 404 {
		t.Fatalf("unknown child: %d", r.status)
	}
	if r := reverse(uuid.New()); r.status != 404 {
		t.Fatalf("unknown entry: %d", r.status)
	}
}

// An archived child's waiting items leave the inbox, and deciding one is a clear refusal.
func TestInboxArchivedChild(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	e.exec(`UPDATE child SET archived_at = now() WHERE id = $1`, h.child1)
	r := e.send(h.actors["adult"], "GET", "/api/inbox", nil)
	if r.status != 200 || len(r.body["items"].([]any)) != 0 {
		t.Fatalf("inbox with an archived child: %d %s", r.status, r.raw)
	}
	if r := e.send(h.actors["adult"], "POST", "/api/inbox/"+h.approval.String()+"/approve", map[string]any{}); r.status != 409 || r.code() != "child_archived" {
		t.Fatalf("approve for an archived child: %d %s", r.status, r.raw)
	}
	if r := e.send(h.actors["adult"], "POST", "/api/points/"+h.entry.String()+"/reverse", map[string]any{}); r.status != 409 || r.code() != "child_archived" {
		t.Fatalf("reverse for an archived child: %d %s", r.status, r.raw)
	}
}
