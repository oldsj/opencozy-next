package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/egress"
)

const (
	oidcClientID     = "opencozy-fixture"
	oidcClientSecret = "fixture-client-secret-0123456789"
	oidcOrigin       = "https://cozy.example.test"
)

// identity is who the fake provider says signed in.
type identity struct {
	sub, email string
	verified   any // true, false, "true", or nil for no claim
	// Overrides, to make a bad token.
	nonce, aud, iss string
	exp             time.Time
	sendNoIDToken   bool
	// The email claims only from userinfo, not the ID token; and userinfo answering another sub.
	userinfoOnly bool
	userinfoSub  string
	authTime     time.Time // auth_time, when set
}

// fakeProvider is a minimal OpenID provider: discovery, keys, and a token endpoint that checks
// the client secret, the redirect URI and PKCE. The test plays the browser at the authorize
// step: authorize records the request and hands out a code for the chosen identity.
type fakeProvider struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	issuer string

	mu       sync.Mutex
	codes    map[string]grant
	tokens   map[string]identity // access token: who, for userinfo
	down     bool                // discovery answers 503
	requests int                 // every request the provider served
}

type grant struct {
	who                        identity
	nonce, challenge, redirect string
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{t: t, key: key, codes: map[string]grant{}, tokens: map[string]identity{}}
	p.srv = httptest.NewTLSServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	p.issuer = p.srv.URL
	return p
}

func (p *fakeProvider) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *fakeProvider) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requests++
	p.mu.Unlock()
	p.mu.Lock()
	down := p.down
	p.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		if down {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		p.json(w, 200, map[string]any{
			"issuer": p.issuer, "authorization_endpoint": p.issuer + "/authorize", "token_endpoint": p.issuer + "/token",
			"jwks_uri": p.issuer + "/jwks", "userinfo_endpoint": p.issuer + "/userinfo", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
		})
	case "/jwks":
		b := base64.RawURLEncoding
		p.json(w, 200, map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "fixture", "alg": "RS256", "use": "sig",
			"n": b.EncodeToString(p.key.N.Bytes()), "e": b.EncodeToString(big.NewInt(int64(p.key.E)).Bytes()),
		}}})
	case "/token":
		p.token(w, r)
	case "/userinfo":
		p.mu.Lock()
		who, ok := p.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		p.mu.Unlock()
		if !ok {
			p.json(w, 401, map[string]any{"error": "invalid_token"})
			return
		}
		claims := map[string]any{"sub": who.sub, "email": who.email}
		if who.userinfoSub != "" {
			claims["sub"] = who.userinfoSub
		}
		if who.verified != nil {
			claims["email_verified"] = who.verified
		}
		p.json(w, 200, claims)
	default:
		http.NotFound(w, r)
	}
}

func (p *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
		p.json(w, 400, map[string]any{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != oidcClientID || secret != oidcClientSecret {
		p.json(w, 401, map[string]any{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	g, found := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || r.PostForm.Get("redirect_uri") != g.redirect || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		p.json(w, 400, map[string]any{"error": "invalid_grant"})
		return
	}
	who := g.who
	access := "fixture-access-" + uuid.NewString()
	p.mu.Lock()
	p.tokens[access] = who
	p.mu.Unlock()
	out := map[string]any{"token_type": "Bearer", "expires_in": 3600, "access_token": access}
	if !who.sendNoIDToken {
		claims := map[string]any{"iss": p.issuer, "aud": oidcClientID, "sub": who.sub, "iat": time.Now().Unix(),
			"exp": time.Now().Add(5 * time.Minute).Unix(), "nonce": g.nonce, "email": who.email, "name": "Fixture person"}
		if who.verified != nil {
			claims["email_verified"] = who.verified
		}
		if !who.authTime.IsZero() {
			claims["auth_time"] = who.authTime.Unix()
		}
		if who.userinfoOnly {
			delete(claims, "email")
			delete(claims, "email_verified")
		}
		if who.nonce != "" {
			claims["nonce"] = who.nonce
		}
		if who.aud != "" {
			claims["aud"] = who.aud
		}
		if who.iss != "" {
			claims["iss"] = who.iss
		}
		if !who.exp.IsZero() {
			claims["exp"] = who.exp.Unix()
		}
		out["id_token"] = p.sign(claims)
	}
	p.json(w, 200, out)
}

func (p *fakeProvider) sign(claims map[string]any) string {
	b := base64.RawURLEncoding
	head, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "fixture"})
	body, _ := json.Marshal(claims)
	signing := b.EncodeToString(head) + "." + b.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		p.t.Fatal(err)
	}
	return signing + "." + b.EncodeToString(sig)
}

// authorize is the provider's sign-in page: it checks the request and approves who.
func (p *fakeProvider) authorize(authURL string, who identity) (code string, q url.Values) {
	p.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, p.issuer+"/authorize?") {
		p.t.Fatalf("authorization URL %q", authURL)
	}
	q = u.Query()
	code = uuid.NewString()
	p.mu.Lock()
	p.codes[code] = grant{who: who, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	p.mu.Unlock()
	return code, q
}

func withOIDC(p *fakeProvider) func(*config.Config) {
	return func(c *config.Config) {
		c.OIDCIssuer, c.OIDCClientID, c.OIDCClientSecret, c.OIDCName = p.issuer, oidcClientID, oidcClientSecret, "Example ID"
		c.PublicOrigin = oidcOrigin
	}
}

// oidcEnv serves OpenCozy with single sign-on at p. Its provider client trusts p's
// certificate; in production the client is internal/egress's (TestOIDCGoesThroughEgress).
func oidcEnv(t *testing.T, p *fakeProvider, logs *bytes.Buffer) *env {
	t.Helper()
	return newEnvWith(t, withOIDC(p), func(s *Server) {
		client := p.srv.Client()
		s.oidcClient = func(ctx context.Context) context.Context { return egress.Context(ctx, client) }
		if logs != nil {
			s.log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		}
	})
}

// started is a sign-in begun by a browser: the provider's URL and the browser's binding cookie.
type started struct {
	url     string
	binding *http.Cookie
}

func (e *env) startOIDC(c cred) started {
	e.t.Helper()
	r := e.send(c, http.MethodPost, "/api/auth/oidc/start", nil)
	if r.status != 200 {
		e.t.Fatalf("start: %d %s", r.status, r.raw)
	}
	var binding *http.Cookie
	for _, ck := range (&http.Response{Header: r.header}).Cookies() {
		if ck.Name == oidcCookie {
			binding = ck
		}
	}
	if binding == nil {
		e.t.Fatalf("start set no binding cookie: %v", r.header)
	}
	return started{url: r.body["url"].(string), binding: binding}
}

// callback is the browser arriving back from the provider. It doesn't follow the redirect.
func (e *env) callback(query url.Values, cookies ...*http.Cookie) (location *url.URL, session *http.Cookie) {
	e.t.Helper()
	req, _ := http.NewRequestWithContext(e.t.Context(), http.MethodGet, e.base+"/api/auth/callback/oidc?"+query.Encode(), nil)
	// A top-level navigation from the provider's site.
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	client := *e.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("callback: %d", res.StatusCode)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		e.t.Errorf("callback Cache-Control %q", res.Header.Get("Cache-Control"))
	}
	location, err = url.Parse(res.Header.Get("Location"))
	if err != nil {
		e.t.Fatal(err)
	}
	cleared := false
	for _, c := range res.Cookies() {
		if c.Name == actor.AdultCookie && c.Value != "" {
			session = c
		}
		if c.Name == oidcCookie && c.MaxAge < 0 && c.Path == "/" {
			cleared = true
		}
	}
	if !cleared {
		e.t.Errorf("callback didn't clear the binding cookie: %v", res.Header.Values("Set-Cookie"))
	}
	return location, session
}

// signInOIDC runs a whole sign-in as who, from a browser holding c's cookies.
func (e *env) signInOIDC(p *fakeProvider, who identity) (*url.URL, *http.Cookie) {
	e.t.Helper()
	s := e.startOIDC(cred{})
	code, q := p.authorize(s.url, who)
	return e.callback(url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {p.issuer}}, s.binding)
}

func refusedWith(t *testing.T, loc *url.URL, session *http.Cookie, code string) {
	t.Helper()
	if loc.Path != "/login" || loc.Query().Get("error") != code || session != nil {
		t.Errorf("want /login?error=%s and no session, got %s (session %v)", code, loc, session != nil)
	}
}

func TestOIDCSignIn(t *testing.T) {
	p := newFakeProvider(t)
	var logs bytes.Buffer
	e := oidcEnv(t, p, &logs)
	h := e.house("a", nil)
	other := e.house("b", nil)
	email := "sam.a@example.test"
	count := func(sql string, args ...any) int {
		var n int
		if err := e.owner.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	adults := count(`SELECT count(*) FROM adult`)

	t.Run("the sign-in page offers it", func(t *testing.T) {
		r := e.send(cred{}, http.MethodGet, "/api/auth/methods", nil)
		if r.status != 200 || r.body["passkeys"] != false || r.body["oidc"].(map[string]any)["name"] != "Example ID" {
			t.Fatalf("%d %s", r.status, r.raw)
		}
	})

	t.Run("start: PKCE S256, state, nonce, the callback URI and a callback-only cookie", func(t *testing.T) {
		s := e.startOIDC(cred{})
		_, q := p.authorize(s.url, identity{})
		for k, want := range map[string]string{
			"client_id": oidcClientID, "redirect_uri": oidcOrigin + "/api/auth/callback/oidc",
			"response_type": "code", "code_challenge_method": "S256", "scope": "openid email profile",
		} {
			if q.Get(k) != want {
				t.Errorf("%s = %q, want %q", k, q.Get(k), want)
			}
		}
		for _, k := range []string{"state", "nonce", "code_challenge"} {
			if len(q.Get(k)) < 43 {
				t.Errorf("%s %q is short", k, q.Get(k))
			}
		}
		if q.Get("state") == q.Get("nonce") || q.Get("state") == s.binding.Value {
			t.Error("state, nonce and binding must differ")
		}
		b := s.binding
		if b.Name != "__Host-oidc" || b.Path != "/" || b.Domain != "" || !b.HttpOnly || !b.Secure || b.SameSite != http.SameSiteLaxMode || b.MaxAge != 600 {
			t.Errorf("binding cookie %+v", b)
		}
	})

	t.Run("an adult signs in by verified email, any case, and is linked", func(t *testing.T) {
		loc, session := e.signInOIDC(p, identity{sub: "sam-sub", email: "Sam.A@Example.test", verified: true})
		if loc.String() != "/" || session == nil {
			t.Fatalf("got %s, session %v", loc, session != nil)
		}
		r := e.send(cred{cookie: session}, http.MethodGet, "/api/auth/me", nil)
		if r.status != 200 || r.body["adult"].(map[string]any)["email"] != email {
			t.Fatalf("me: %d %s", r.status, r.raw)
		}
		if n := count(`SELECT count(*) FROM oidc_link WHERE adult_id = $1 AND subject = 'sam-sub' AND issuer = $2 AND email = $3`, h.adult, p.issuer, email); n != 1 {
			t.Fatalf("links: %d", n)
		}
		// A fresh sign-in counts as recent, like a password.
		if r := e.send(cred{cookie: session}, http.MethodPost, "/api/auth/passkeys/options", nil); r.status == 403 {
			t.Errorf("not recent: %s", r.raw)
		}
	})

	t.Run("a linked account signs in by subject, whatever its email says now", func(t *testing.T) {
		loc, session := e.signInOIDC(p, identity{sub: "sam-sub", email: "new.address@example.test", verified: false})
		if loc.String() != "/" || session == nil {
			t.Fatalf("got %s", loc)
		}
		r := e.send(cred{cookie: session}, http.MethodGet, "/api/auth/me", nil)
		if r.body["adult"].(map[string]any)["id"] != h.adult.String() {
			t.Fatalf("me: %s", r.raw)
		}
	})

	t.Run("a silent sign-in from an old provider session isn't recent", func(t *testing.T) {
		loc, session := e.signInOIDC(p, identity{sub: "sam-sub", email: email, verified: true, authTime: time.Now().Add(-2 * time.Hour)})
		if loc.String() != "/" || session == nil {
			t.Fatalf("got %s", loc)
		}
		if r := e.send(cred{cookie: session}, http.MethodPost, "/api/auth/passkeys/options", nil); r.status != 403 || r.code() != "reauthentication_required" {
			t.Errorf("recent anyway: %d %s", r.status, r.raw)
		}
		// auth_time in the future counts as now, never later.
		_, session = e.signInOIDC(p, identity{sub: "sam-sub", email: email, verified: true, authTime: time.Now().Add(time.Hour)})
		if n := count(`SELECT count(*) FROM adult_session WHERE authenticated_at > now()`); n != 0 {
			t.Errorf("%d sessions authenticated in the future", n)
		}
		if r := e.send(cred{cookie: session}, http.MethodPost, "/api/auth/passkeys/options", nil); r.status == 403 {
			t.Errorf("not recent: %s", r.raw)
		}
	})

	t.Run("refused", func(t *testing.T) {
		for name, tc := range map[string]struct {
			who  identity
			code string
		}{
			"an email no adult uses":              {identity{sub: "stranger", email: "stranger@example.test", verified: true}, "oidc_no_adult"},
			"an unverified email":                 {identity{sub: "other-b", email: "sam.b@example.test", verified: false}, "oidc_unverified"},
			"no email_verified claim":             {identity{sub: "other-b", email: "sam.b@example.test"}, "oidc_unverified"},
			"a second account for a linked adult": {identity{sub: "sam-sub-2", email: email, verified: true}, "oidc_linked_other"},
			"a token for another nonce":           {identity{sub: "sam-sub", email: email, verified: true, nonce: "replayed-nonce-0123456789"}, "oidc_failed"},
			"a token for another client":          {identity{sub: "sam-sub", email: email, verified: true, aud: "someone-else"}, "oidc_failed"},
			"a token from another issuer":         {identity{sub: "sam-sub", email: email, verified: true, iss: "https://elsewhere.example.test"}, "oidc_failed"},
			"an expired token":                    {identity{sub: "sam-sub", email: email, verified: true, exp: time.Now().Add(-time.Hour)}, "oidc_failed"},
			"no ID token":                         {identity{sub: "sam-sub", email: email, verified: true, sendNoIDToken: true}, "oidc_failed"},
			"userinfo about another subject":      {identity{sub: "other-b", email: "sam.b@example.test", verified: true, userinfoOnly: true, userinfoSub: "someone-else"}, "oidc_failed"},
			"userinfo with an unverified email":   {identity{sub: "other-b", email: "sam.b@example.test", verified: false, userinfoOnly: true}, "oidc_unverified"},
		} {
			t.Run(name, func(t *testing.T) {
				loc, session := e.signInOIDC(p, tc.who)
				refusedWith(t, loc, session, tc.code)
			})
		}
		if n := count(`SELECT count(*) FROM adult`); n != adults {
			t.Errorf("adults %d, want %d: nobody is ever created", n, adults)
		}
		if n := count(`SELECT count(*) FROM oidc_link`); n != 1 {
			t.Errorf("links %d, want only sam-sub's", n)
		}
	})

	t.Run("an ID token without the email: the email comes from userinfo, for the same subject", func(t *testing.T) {
		loc, session := e.signInOIDC(p, identity{sub: "other-b-userinfo", email: "Sam.B@example.test", verified: "true", userinfoOnly: true})
		if loc.String() != "/" || session == nil {
			t.Fatalf("got %s", loc)
		}
		if n := count(`SELECT count(*) FROM oidc_link WHERE adult_id = $1 AND subject = 'other-b-userinfo'`, other.adult); n != 1 {
			t.Fatalf("links: %d", n)
		}
		e.exec(`DELETE FROM oidc_link WHERE adult_id = $1`, other.adult)
	})

	t.Run("the callback needs the browser that started, once, and the provider's answer", func(t *testing.T) {
		who := identity{sub: "sam-sub", email: email, verified: true}
		begin := func() (started, url.Values) {
			s := e.startOIDC(cred{})
			code, q := p.authorize(s.url, who)
			return s, url.Values{"code": {code}, "state": {q.Get("state")}}
		}
		s, q := begin()
		loc, session := e.callback(q) // no cookie: another browser
		refusedWith(t, loc, session, "oidc_failed")
		loc, session = e.callback(q, s.binding) // the state was used up by the attempt above
		refusedWith(t, loc, session, "oidc_failed")

		s, q = begin()
		other := e.startOIDC(cred{}) // a binding from another sign-in
		loc, session = e.callback(q, other.binding)
		refusedWith(t, loc, session, "oidc_failed")

		s, q = begin()
		q.Set("iss", "https://elsewhere.example.test")
		loc, session = e.callback(q, s.binding)
		refusedWith(t, loc, session, "oidc_failed")

		s, q = begin()
		q.Del("code")
		q.Set("error", "access_denied")
		loc, session = e.callback(q, s.binding)
		refusedWith(t, loc, session, "oidc_failed")

		s, q = begin()
		q.Set("state", "not-a-state-anyone-started")
		loc, session = e.callback(q, s.binding)
		refusedWith(t, loc, session, "oidc_failed")

		// An expired sign-in is refused.
		s, q = begin()
		e.exec(`UPDATE oidc_sign_in SET expires_at = now() - interval '1 second'`)
		loc, session = e.callback(q, s.binding)
		refusedWith(t, loc, session, "oidc_failed")

		// Providers add their own parameters; they are ignored.
		s, q = begin()
		q.Set("session_state", "abc")
		loc, session = e.callback(q, s.binding)
		if loc.String() != "/" || session == nil {
			t.Fatalf("got %s", loc)
		}
		// A code is good once: the provider refuses it the second time.
		s2 := e.startOIDC(cred{})
		_, q2 := p.authorize(s2.url, who)
		q2.Set("code", q.Get("code"))
		loc, session = e.callback(q2, s2.binding)
		refusedWith(t, loc, session, "oidc_failed")
	})

	t.Run("a removed adult or an archived household can't sign in", func(t *testing.T) {
		e.exec(`UPDATE household SET archived_at = now() WHERE id = $1`, h.id)
		loc, session := e.signInOIDC(p, identity{sub: "sam-sub", email: email, verified: true})
		refusedWith(t, loc, session, "oidc_no_adult")
		e.exec(`UPDATE household SET archived_at = NULL WHERE id = $1`, h.id)
		e.exec(`UPDATE adult SET removed_at = now() WHERE id = $1`, other.adult)
		loc, session = e.signInOIDC(p, identity{sub: "other-b", email: "sam.b@example.test", verified: true})
		refusedWith(t, loc, session, "oidc_no_adult")
		e.exec(`UPDATE adult SET removed_at = NULL WHERE id = $1`, other.adult)
	})

	t.Run("a kid's device can't start or finish one", func(t *testing.T) {
		kid := cred{cookie: &http.Cookie{Name: actor.DeviceWebCookie, Value: "x"}}
		if r := e.send(kid, http.MethodPost, "/api/auth/oidc/start", nil); r.status != 409 || r.code() != "kid_device" {
			t.Errorf("start: %d %s", r.status, r.raw)
		}
		s := e.startOIDC(cred{})
		code, q := p.authorize(s.url, identity{sub: "sam-sub", email: email, verified: true})
		loc, session := e.callback(url.Values{"code": {code}, "state": {q.Get("state")}}, s.binding, kid.cookie)
		refusedWith(t, loc, session, "kid_device")
	})

	t.Run("account: see the link, unlink it, link again by verified email", func(t *testing.T) {
		adult := h.actors["adult"]
		r := e.send(adult, http.MethodGet, "/api/auth/oidc/link", nil)
		link, _ := r.body["link"].(map[string]any)
		if r.status != 200 || link["email"] != email || link["linkedAt"] == nil {
			t.Fatalf("link: %d %s", r.status, r.raw)
		}
		// Another household's adult sees only their own (none).
		if r := e.send(other.actors["adult"], http.MethodGet, "/api/auth/oidc/link", nil); r.status != 200 || r.body["link"] != nil {
			t.Fatalf("other's link: %d %s", r.status, r.raw)
		}
		if r := e.send(h.actors["adult-stale"], http.MethodDelete, "/api/auth/oidc/link", nil); r.status != 403 {
			t.Fatalf("stale unlink: %d %s", r.status, r.raw)
		}
		if r := e.send(adult, http.MethodDelete, "/api/auth/oidc/link", nil); r.status != 204 {
			t.Fatalf("unlink: %d %s", r.status, r.raw)
		}
		if r := e.send(adult, http.MethodDelete, "/api/auth/oidc/link", nil); r.status != 404 {
			t.Fatalf("unlink again: %d %s", r.status, r.raw)
		}
		if r := e.send(adult, http.MethodGet, "/api/auth/oidc/link", nil); r.body["link"] != nil {
			t.Fatalf("after unlink: %s", r.raw)
		}
		// Unlinked, the old subject no longer signs in on its own: it must link afresh, by a
		// verified email.
		loc, session := e.signInOIDC(p, identity{sub: "sam-sub", email: "new.address@example.test", verified: true})
		refusedWith(t, loc, session, "oidc_no_adult")
		loc, session = e.signInOIDC(p, identity{sub: "sam-sub-2", email: email, verified: "true"})
		if loc.String() != "/" || session == nil {
			t.Fatalf("relink: %s", loc)
		}
	})

	t.Run("passwords keep working", func(t *testing.T) {
		hash, err := auth.HashPassword(password)
		if err != nil {
			t.Fatal(err)
		}
		addr := e.withPassword(h, auth.SchemeArgon2id, hash)
		if r := e.send(cred{}, http.MethodPost, "/api/auth/login", map[string]any{"email": addr, "password": password}); r.status != 200 {
			t.Fatalf("login: %d %s", r.status, r.raw)
		}
	})

	if strings.Contains(logs.String(), oidcClientSecret) || strings.Contains(logs.String(), "fixture-access-") {
		t.Error("the logs hold a secret")
	}
}

func TestOIDCOff(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", nil)
	if r := e.send(cred{}, http.MethodGet, "/api/auth/methods", nil); r.status != 200 || r.body["oidc"] != nil {
		t.Fatalf("methods: %d %s", r.status, r.raw)
	}
	if r := e.send(cred{}, http.MethodPost, "/api/auth/oidc/start", nil); r.status != 404 || r.code() != "oidc_off" {
		t.Fatalf("start: %d %s", r.status, r.raw)
	}
	if r := e.send(h.actors["adult"], http.MethodGet, "/api/auth/oidc/link", nil); r.status != 404 {
		t.Fatalf("link: %d %s", r.status, r.raw)
	}
	loc, session := e.callback(url.Values{"code": {"x"}, "state": {"y"}})
	refusedWith(t, loc, session, "oidc_off")
}

// TestOIDCGoesThroughEgress: without a test client, the provider is reached only through
// internal/egress, which refuses this loopback provider, so discovery fails and nothing reaches
// it (design §10).
func TestOIDCGoesThroughEgress(t *testing.T) {
	p := newFakeProvider(t)
	e := newEnv(t, withOIDC(p))
	r := e.send(cred{}, http.MethodPost, "/api/auth/oidc/start", nil)
	if r.status != 503 || r.code() != "oidc_unavailable" {
		t.Fatalf("start: %d %s", r.status, r.raw)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.requests != 0 {
		t.Fatalf("the provider got %d requests", p.requests)
	}
}

// A provider outage at refresh time: requests keep the last good discovery without waiting, and
// the failed refresh is retried after oidcRetry rather than on every request.
func TestOIDCDiscoveryOutage(t *testing.T) {
	p := newFakeProvider(t)
	var cfg config.Config
	withOIDC(p)(&cfg)
	client := p.srv.Client()
	f, err := newOIDCFlow(cfg, func(ctx context.Context) context.Context { return egress.Context(ctx, client) })
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.down = true
	p.mu.Unlock()
	f.mu.Lock()
	f.next = time.Now().Add(-time.Second) // due
	f.mu.Unlock()
	for range 5 {
		got, err := f.provider(t.Context())
		if err != nil || got != first {
			t.Fatalf("during the outage: %v, %v", got == first, err)
		}
	}
	// The background refresh fails and puts the next try off.
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		next := f.next
		f.mu.Unlock()
		if next.After(time.Now().Add(oidcRetry / 2)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the failed refresh didn't schedule a retry")
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	if f.cur != first {
		t.Error("the last good answer was dropped")
	}
	f.mu.Unlock()
}
