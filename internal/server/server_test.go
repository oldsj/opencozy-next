package server

import (
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/scrypt"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/config"
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
