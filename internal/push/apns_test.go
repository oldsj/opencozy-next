package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

func testKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// fakeAPNs is an HTTP/2 TLS server that answers each device token as told.
type fakeAPNs struct {
	mu       sync.Mutex
	srv      *httptest.Server
	answer   map[string]func(w http.ResponseWriter)
	requests []*http.Request
	bodies   []map[string]any
}

func newFakeAPNs(t *testing.T) *fakeAPNs {
	f := &fakeAPNs{answer: map[string]func(http.ResponseWriter){}}
	f.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("APNs needs HTTP/2, got %s", r.Proto)
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.bodies = append(f.bodies, body)
		answer := f.answer[strings.TrimPrefix(r.URL.Path, "/3/device/")]
		f.mu.Unlock()
		if answer != nil {
			answer(w)
		}
	}))
	f.srv.EnableHTTP2 = true
	f.srv.StartTLS()
	t.Cleanup(f.srv.Close)
	return f
}

func reason(status int, r string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"reason":"` + r + `"}`))
	}
}

func testAPNs(t *testing.T, f *fakeAPNs) (*APNs, *ecdsa.PrivateKey) {
	t.Helper()
	k, p := testKey(t)
	a, err := newAPNs(APNsConfig{TeamID: "TEAM123456", Topic: "org.example.opencozy", KeyID: "KEY1234567", Key: p}, f.srv.Client(),
		map[string]string{"development": f.srv.URL, "production": f.srv.URL}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	return a, k
}

func TestNewAPNsChecksConfig(t *testing.T) {
	_, p := testKey(t)
	rsaish := "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"
	for name, c := range map[string]APNsConfig{
		"team":  {TeamID: "team", Topic: "t", KeyID: "KEY1234567", Key: p},
		"key":   {TeamID: "TEAM123456", Topic: "t", KeyID: "KEY1234567", Key: rsaish},
		"topic": {TeamID: "TEAM123456", KeyID: "KEY1234567", Key: p},
	} {
		if _, err := newAPNs(c, http.DefaultClient, Origins, quiet); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestProviderToken(t *testing.T) {
	f := newFakeAPNs(t)
	a, k := testAPNs(t, f)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	tok, err := a.providerToken(false)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q", tok)
	}
	var head, claims map[string]any
	for i, v := range []*map[string]any{&head, &claims} {
		raw, _ := base64.RawURLEncoding.DecodeString(parts[i])
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatal(err)
		}
	}
	if head["alg"] != "ES256" || head["kid"] != "KEY1234567" || claims["iss"] != "TEAM123456" || claims["iat"] != float64(now.Unix()) {
		t.Errorf("header %v claims %v", head, claims)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&k.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("the signature does not verify")
	}
	// Reused for 50 minutes, then renewed.
	now = now.Add(49 * time.Minute)
	if again, _ := a.providerToken(false); again != tok {
		t.Error("renewed too soon")
	}
	now = now.Add(2 * time.Minute)
	if again, _ := a.providerToken(false); again == tok {
		t.Error("not renewed after 50 minutes")
	}
}

func TestPost(t *testing.T) {
	f := newFakeAPNs(t)
	a, _ := testAPNs(t, f)
	ctx := context.Background()
	tok := func(c byte) string { return strings.Repeat(string(c), 64) }
	f.answer[tok('a')] = func(w http.ResponseWriter) {}
	f.answer[tok('b')] = reason(410, "Unregistered")
	f.answer[tok('c')] = reason(400, "BadDeviceToken")
	f.answer[tok('d')] = reason(503, "ServiceUnavailable")
	f.answer[tok('e')] = reason(413, "PayloadTooLarge")
	f.answer[tok('f')] = reason(429, "TooManyRequests")
	alert := Notification{Type: "approval.pending", Collapse: "c1", Title: "Robin finished Feed the cat", Body: "3 points", Badge: true}

	for c, want := range map[byte]error{'a': nil, 'b': errInvalidToken, 'c': errInvalidToken, 'd': errTransient, 'f': errTransient} {
		if err := a.post(ctx, target{token: tok(c), env: "production"}, alert, []byte(`{}`)); (want == nil) != (err == nil) || (want != nil && !errors.Is(err, want)) {
			t.Errorf("%c: got %v, want %v", c, err, want)
		}
	}
	if err := a.post(ctx, target{token: tok('e'), env: "production"}, alert, []byte(`{}`)); err == nil || errors.Is(err, errTransient) || errors.Is(err, errInvalidToken) {
		t.Errorf("a 413 is refused for good, got %v", err)
	}
	r := f.requests[0]
	for h, want := range map[string]string{"apns-topic": "org.example.opencozy", "apns-collapse-id": "c1", "apns-push-type": "alert", "apns-priority": "10"} {
		if got := r.Header.Get(h); got != want {
			t.Errorf("%s: %q, want %q", h, got, want)
		}
	}
	if !strings.HasPrefix(r.Header.Get("authorization"), "bearer ") {
		t.Error("no provider token")
	}

	// A silent update is a background push at priority 5.
	silent := Notification{Type: "approval.resolved", Collapse: "c1", Badge: true}
	if err := a.post(ctx, target{token: tok('a'), env: "development"}, silent, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if r := f.requests[len(f.requests)-1]; r.Header.Get("apns-push-type") != "background" || r.Header.Get("apns-priority") != "5" {
		t.Errorf("silent: %v", r.Header)
	}

	// An expired provider token is renewed once.
	calls := 0
	f.answer[tok('g')] = func(w http.ResponseWriter) {
		calls++
		if calls == 1 {
			reason(403, "ExpiredProviderToken")(w)
		}
	}
	if err := a.post(ctx, target{token: tok('g'), env: "production"}, alert, []byte(`{}`)); err != nil || calls != 2 {
		t.Errorf("expired token: %v after %d calls", err, calls)
	}
	if err := a.post(ctx, target{token: tok('a'), env: "staging"}, alert, []byte(`{}`)); err == nil {
		t.Error("an unknown environment was accepted")
	}
}

func TestNotificationPayload(t *testing.T) {
	ap := dbq.Approval{ID: uuid.MustParse("0195f2d4-0000-7000-8000-000000000001"), Kind: "completion", Title: "Feed the cat", Points: 1}
	asked := time.Unix(1_800_000_000, 0)
	key, n := ApprovalPending(ap, "Robin", asked)
	got := n.APNs(4)
	aps := got["aps"].(map[string]any)
	app := got["opencozy"].(map[string]any)
	if aps["alert"].(map[string]any)["title"] != "Robin finished Feed the cat" || aps["alert"].(map[string]any)["body"] != "1 point · waiting for your approval" {
		t.Errorf("alert: %v", aps["alert"])
	}
	if aps["badge"] != 4 || aps["category"] != "APPROVAL" || aps["sound"] != "default" || app["pendingCount"] != 4 || app["itemId"] != ap.ID.String() || app["type"] != "approval.pending" {
		t.Errorf("payload: %v", got)
	}
	if n.Collapse != ap.ID.String() {
		t.Errorf("collapse %q", n.Collapse)
	}

	// Asking again later (old #74) is a new push, replacing the first: a new key, the same collapse ID.
	again, n2 := ApprovalPending(ap, "Robin", asked.Add(3*time.Minute))
	if again == key || n2.Collapse != n.Collapse {
		t.Errorf("reminder: key %q vs %q, collapse %q vs %q", again, key, n2.Collapse, n.Collapse)
	}
	if same, _ := ApprovalPending(ap, "Robin", asked); same != key {
		t.Error("the same ask must dedupe")
	}

	ap.Kind, ap.Title, ap.Points = "redemption", "Movie night", 20
	if _, n := ApprovalPending(ap, "Robin", asked); n.Title != "Robin wants Movie night" || n.Body != "20 points · waiting for your approval" {
		t.Errorf("redemption: %q / %q", n.Title, n.Body)
	}
	ap.Kind = "time_request"
	if _, n := ApprovalPending(ap, "Robin", asked); n.Title != "Robin is asking for more iPad time" || n.Category != "TIME_REQUEST" {
		t.Errorf("time request: %q", n.Title)
	}

	// Silent: content-available only, no alert, sound or badge.
	_, r := ApprovalResolved(ap.ID, "approved")
	s := r.APNs(2)
	if len(s["aps"].(map[string]any)) != 1 || s["aps"].(map[string]any)["content-available"] != 1 || s["opencozy"].(map[string]any)["state"] != "approved" {
		t.Errorf("silent: %v", s)
	}

	passive := Notification{Type: "tv.pending", Collapse: "p", Title: "Living room: who's watching?", Passive: true, Wake: true}.APNs(0)["aps"].(map[string]any)
	if passive["sound"] != nil || passive["interruption-level"] != "passive" || passive["content-available"] != 1 || passive["badge"] != nil {
		t.Errorf("passive: %v", passive)
	}
}

func TestSendToAdult(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h, adult := household(t, db.Owner)
	phone := func(name, token string) {
		t.Helper()
		if _, err := db.Owner.Exec(ctx, `INSERT INTO phone_session (household_id, adult_id, name, expires_at, push_token, push_environment, push_registered_at)
			VALUES ($1, $2, $3, now() + interval '1 day', $4, 'production', now())`, h, adult, name, token); err != nil {
			t.Fatal(err)
		}
	}
	good, gone := strings.Repeat("a", 64), strings.Repeat("b", 64)
	phone("Phone", good)
	phone("Old phone", gone)
	f := newFakeAPNs(t)
	f.answer[good] = func(http.ResponseWriter) {}
	f.answer[gone] = reason(410, "Unregistered")
	a, _ := testAPNs(t, f)
	q := dbq.New(db.Runtime)
	send := func(n Notification) {
		t.Helper()
		payload, _ := json.Marshal(n)
		if err := a.Send(ctx, q, dbq.PushOutbox{HouseholdID: h, AdultID: &adult, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}

	// Both phones are tried; the one APNs no longer knows loses its token.
	send(Notification{Type: "approval.pending", Collapse: "c", Title: "Robin finished Feed the cat", Badge: true})
	if len(f.requests) != 2 {
		t.Fatalf("%d requests", len(f.requests))
	}
	if f.bodies[0]["aps"].(map[string]any)["badge"] != float64(0) {
		t.Errorf("badge: %v", f.bodies[0])
	}
	var left int
	if err := db.Owner.QueryRow(ctx, `SELECT count(*) FROM phone_session WHERE push_token IS NOT NULL`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("tokens left: %d %v", left, err)
	}

	// Muted: no alerts, but silent updates still arrive.
	if _, err := db.Owner.Exec(ctx, `UPDATE adult SET push_muted_until = now() + interval '1 hour' WHERE id = $1`, adult); err != nil {
		t.Fatal(err)
	}
	send(Notification{Type: "approval.pending", Collapse: "c", Title: "Robin wants Movie night", Badge: true})
	if len(f.requests) != 2 {
		t.Errorf("a muted adult got an alert: %d requests", len(f.requests))
	}
	send(Notification{Type: "approval.resolved", Collapse: "c", Badge: true})
	if len(f.requests) != 3 {
		t.Errorf("a muted adult missed a silent update: %d requests", len(f.requests))
	}
}
