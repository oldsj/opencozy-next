package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/oldsj/opencozy-next/internal/config"
)

const (
	rpID     = "cozy.example.test"
	rpOrigin = "https://cozy.example.test"
)

func withPasskeys(c *config.Config) {
	c.WebAuthnRPID = rpID
	c.WebAuthnOrigins = []string{rpOrigin}
}

var b64 = base64.RawURLEncoding

// authenticator is a software passkey: a P-256 key with "none" attestation, discoverable and
// user-verifying, that signs the way a platform authenticator does.
type authenticator struct {
	t          *testing.T
	key        *ecdsa.PrivateKey
	id, handle []byte
	count      uint32
	origin     string
}

func newAuthenticator(t *testing.T) *authenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &authenticator{t: t, key: key, id: id, origin: rpOrigin}
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagBE = 0x08
	flagBS = 0x10
	flagAT = 0x40
)

func (a *authenticator) authData(flags byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append(h[:], flags|flagUP|flagUV|flagBE|flagBS)
	return binary.BigEndian.AppendUint32(out, a.count)
}

func (a *authenticator) clientData(kind string, options map[string]any) []byte {
	j, _ := json.Marshal(map[string]any{"type": kind, "challenge": options["challenge"], "origin": a.origin})
	return j
}

// create answers navigator.credentials.create.
func (a *authenticator) create(options map[string]any) map[string]any {
	a.t.Helper()
	user, _ := options["user"].(map[string]any)
	handle, err := b64.DecodeString(user["id"].(string))
	if err != nil {
		a.t.Fatal(err)
	}
	a.handle = handle
	pub, err := a.key.PublicKey.ECDH()
	if err != nil {
		a.t.Fatal(err)
	}
	raw := pub.Bytes() // 0x04 || x || y
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: raw[1:33], -3: raw[33:]})
	ad := a.authData(flagAT)
	ad = append(ad, make([]byte, 16)...) // AAGUID
	ad = binary.BigEndian.AppendUint16(ad, uint16(len(a.id)))
	ad = append(append(ad, a.id...), cose...)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	return map[string]any{
		"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", options)),
			"attestationObject": b64.EncodeToString(att),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults": map[string]any{},
	}
}

// get answers navigator.credentials.get, counting up first.
func (a *authenticator) get(options map[string]any) map[string]any {
	a.t.Helper()
	a.count++
	ad := a.authData(0)
	cd := a.clientData("webauthn.get", options)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return map[string]any{
		"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(a.handle),
		},
		"clientExtensionResults": map[string]any{},
	}
}

func options(t *testing.T, r resp) (string, map[string]any) {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("options: %d %s", r.status, r.raw)
	}
	opts, _ := r.body["options"].(map[string]any)
	return r.body["challengeId"].(string), opts
}

func TestPasskeys(t *testing.T) {
	e := newEnv(t, withPasskeys)
	h := e.house("a", e.srv.table.Scopes())
	adult, none := h.actors["adult"], h.actors["none"]
	e.exec(`DELETE FROM passkey WHERE adult_id = $1`, h.adult) // start without the fixture's

	// Add one. The options exclude nothing yet and require a discoverable, verified passkey.
	key := newAuthenticator(t)
	ch, opts := options(t, e.send(adult, http.MethodPost, "/api/auth/passkeys/options", nil))
	if sel, _ := opts["authenticatorSelection"].(map[string]any); sel["residentKey"] != "required" || sel["userVerification"] != "required" {
		t.Fatalf("authenticatorSelection: %v", opts["authenticatorSelection"])
	}
	created := key.create(opts)
	if r := e.send(adult, http.MethodPost, "/api/auth/passkeys", map[string]any{"challengeId": ch, "name": "Laptop", "credential": created}); r.status != 201 || r.body["backedUp"] != true {
		t.Fatalf("add: %d %s", r.status, r.raw)
	}
	// The challenge worked once.
	if r := e.send(adult, http.MethodPost, "/api/auth/passkeys", map[string]any{"challengeId": ch, "name": "Again", "credential": created}); r.status != 422 || r.code() != "challenge_expired" {
		t.Fatalf("replayed add: %d %s", r.status, r.raw)
	}
	if r := e.send(adult, http.MethodGet, "/api/auth/passkeys", nil); len(r.body["passkeys"].([]any)) != 1 {
		t.Fatalf("list: %s", r.raw)
	}

	// Sign in with it, signed out.
	ch, opts = options(t, e.send(none, http.MethodPost, "/api/auth/passkey/options", nil))
	assertion := key.get(opts)
	r := e.send(none, http.MethodPost, "/api/auth/passkey", map[string]any{"challengeId": ch, "credential": assertion})
	if r.status != 200 {
		t.Fatalf("sign in: %d %s", r.status, r.raw)
	}
	session := sessionFrom(t, r)
	if r := e.send(session, http.MethodGet, "/api/auth/me", nil); r.status != 200 {
		t.Fatalf("me: %d %s", r.status, r.raw)
	}
	// Replaying the same response fails: the challenge is gone.
	if r := e.send(none, http.MethodPost, "/api/auth/passkey", map[string]any{"challengeId": ch, "credential": assertion}); r.status != 401 || r.code() != "passkey_not_accepted" {
		t.Fatalf("replayed sign-in: %d %s", r.status, r.raw)
	}
	// Another site's response is refused.
	ch, opts = options(t, e.send(none, http.MethodPost, "/api/auth/passkey/options", nil))
	key.origin = "https://elsewhere.example.test"
	if r := e.send(none, http.MethodPost, "/api/auth/passkey", map[string]any{"challengeId": ch, "credential": key.get(opts)}); r.status != 401 {
		t.Fatalf("wrong origin: %d %s", r.status, r.raw)
	}
	key.origin = rpOrigin
	// A sign counter that went backwards looks like a clone, and is refused.
	e.exec(`UPDATE passkey SET sign_count = 100 WHERE adult_id = $1`, h.adult)
	ch, opts = options(t, e.send(none, http.MethodPost, "/api/auth/passkey/options", nil))
	if r := e.send(none, http.MethodPost, "/api/auth/passkey", map[string]any{"challengeId": ch, "credential": key.get(opts)}); r.status != 401 {
		t.Fatalf("cloned counter: %d %s", r.status, r.raw)
	}
	key.count = 200

	// A stale sign-in can't add a passkey until it is confirmed with one.
	stale := h.actors["adult-stale"]
	if r := e.send(stale, http.MethodPost, "/api/auth/passkeys/options", nil); r.status != 403 {
		t.Fatalf("stale: %d %s", r.status, r.raw)
	}
	ch, opts = options(t, e.send(stale, http.MethodPost, "/api/auth/passkey/confirm/options", nil))
	if r := e.send(stale, http.MethodPost, "/api/auth/passkey/confirm", map[string]any{"challengeId": ch, "credential": key.get(opts)}); r.status != 204 {
		t.Fatalf("confirm: %d %s", r.status, r.raw)
	}
	if r := e.send(stale, http.MethodPost, "/api/auth/passkeys/options", nil); r.status != 200 {
		t.Fatalf("after confirm: %d %s", r.status, r.raw)
	}

	// The only way to sign in can't be removed.
	r = e.send(adult, http.MethodGet, "/api/auth/passkeys", nil)
	id := r.body["passkeys"].([]any)[0].(map[string]any)["id"].(string)
	if r := e.send(adult, http.MethodDelete, "/api/auth/passkeys/"+id, nil); r.status != 409 || r.code() != "last_sign_in" {
		t.Fatalf("remove the last: %d %s", r.status, r.raw)
	}
	e.withPassword(h, "argon2id", "$argon2id$v=19$m=65536,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA")
	if r := e.send(adult, http.MethodDelete, "/api/auth/passkeys/"+id, nil); r.status != 204 {
		t.Fatalf("remove: %d %s", r.status, r.raw)
	}
	ch, opts = options(t, e.send(none, http.MethodPost, "/api/auth/passkey/options", nil))
	if r := e.send(none, http.MethodPost, "/api/auth/passkey", map[string]any{"challengeId": ch, "credential": key.get(opts)}); r.status != 401 {
		t.Fatalf("removed passkey signed in: %d %s", r.status, r.raw)
	}
}

func TestPasskeysOff(t *testing.T) {
	e := newEnv(t, nil)
	if r := e.send(cred{}, http.MethodPost, "/api/auth/passkey/options", nil); r.status != 404 || r.code() != "passkeys_off" {
		t.Fatalf("%d %s", r.status, r.raw)
	}
}

// addPasskey adds key for h's adult and returns the passkey's id.
func addPasskey(t *testing.T, e *env, h house, key *authenticator, name string) string {
	t.Helper()
	ch, opts := options(t, e.send(h.actors["adult"], http.MethodPost, "/api/auth/passkeys/options", nil))
	r := e.send(h.actors["adult"], http.MethodPost, "/api/auth/passkeys", map[string]any{"challengeId": ch, "name": name, "credential": key.create(opts)})
	if r.status != 201 {
		t.Fatalf("add: %d %s", r.status, r.raw)
	}
	return r.body["id"].(string)
}

func signInWith(e *env, key *authenticator) resp {
	ch, opts := options(e.t, e.send(cred{}, http.MethodPost, "/api/auth/passkey/options", nil))
	return e.send(cred{}, http.MethodPost, "/api/auth/passkey", map[string]any{"challengeId": ch, "credential": key.get(opts)})
}

func TestPasskeyFollowUps(t *testing.T) {
	e := newEnv(t, withPasskeys)
	h := e.house("a", e.srv.table.Scopes())
	e.exec(`DELETE FROM passkey WHERE adult_id = $1`, h.adult) // start without the fixture's
	key := newAuthenticator(t)
	addPasskey(t, e, h, key, "Tablet")

	// An imported passkey's backup eligibility is unknown until its first sign-in records it.
	e.exec(`UPDATE passkey SET backup_eligible = NULL WHERE adult_id = $1 AND name = 'Tablet'`, h.adult)
	if r := signInWith(e, key); r.status != 200 {
		t.Fatalf("unknown eligibility: %d %s", r.status, r.raw)
	}
	var eligible *bool
	if err := e.owner.QueryRow(t.Context(), `SELECT backup_eligible FROM passkey WHERE adult_id = $1 AND name = 'Tablet'`, h.adult).Scan(&eligible); err != nil || eligible == nil || !*eligible {
		t.Fatalf("eligibility not recorded: %v %v", eligible, err)
	}

	// Two removals at once can't leave a password-less adult with no passkey.
	e.exec(`DELETE FROM passkey WHERE adult_id = $1 AND name <> 'Tablet'`, h.adult)
	second := addPasskey(t, e, h, newAuthenticator(t), "Phone")
	var first string
	if err := e.owner.QueryRow(t.Context(), `SELECT id::text FROM passkey WHERE adult_id = $1 AND name = 'Tablet'`, h.adult).Scan(&first); err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, id := range []string{first, second} {
		wg.Go(func() { codes <- e.send(h.actors["adult"], http.MethodDelete, "/api/auth/passkeys/"+id, nil).status })
	}
	wg.Wait()
	close(codes)
	got := map[int]int{}
	for c := range codes {
		got[c]++
	}
	if got[204] != 1 || got[409] != 1 {
		t.Fatalf("concurrent removals: %v", got)
	}

	// An archived household can't sign in with a passkey, as with a password.
	e.exec(`UPDATE household SET archived_at = now() WHERE id = $1`, h.id)
	if r := signInWith(e, key); r.status != 401 {
		t.Fatalf("archived household: %d %s", r.status, r.raw)
	}
}
