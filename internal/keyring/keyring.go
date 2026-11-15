// Package keyring encrypts integration secrets at rest and keys the feed-URL HMAC (design §11).
//
// Keys come from OPENCOZY_SECRET_KEYS, "id:base64key,…": each key is 32 random bytes in
// standard base64, each id is 1 to 16 of [a-z0-9-], starting with a letter or digit. The first
// key encrypts; every key decrypts. To rotate, put a new key first, deploy, run `app rekey`,
// then drop the old key.
//
// Ciphertext is AES-256-GCM with a random 96-bit nonce, stored as
// "v1.<key-id>.<nonce>.<ciphertext>" (unpadded base64url). The additional data binds a value to
// its household, table, row and purpose, so a value copied to another row does not decrypt.
//
// The feed-URL HMAC key is not configured separately: for each keyring key, HKDF-SHA256 derives
// a distinct HMAC-SHA256 key (info "opencozy feed-url hmac v1"), so it rotates with the
// keyring. MACs are stored as "h1.<key-id>.<mac>". `app rekey` re-encrypts each feed URL under
// the first key and recomputes its MAC from the decrypted URL under the first key's derived
// key. During a rotation, uniqueness lookups compare against FeedMACs, one MAC per key.
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

const (
	keySize  = 32
	hmacInfo = "opencozy feed-url hmac v1"
	aadLabel = "opencozy secret v1"
	macLabel = "opencozy feed-url v1"
)

var (
	idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)
	b64  = base64.RawURLEncoding

	// ErrDecrypt means a value did not decrypt: wrong row, wrong purpose, unknown key or
	// tampered ciphertext. It never says which.
	ErrDecrypt = errors.New("keyring: value does not decrypt")
)

// AAD names where a value lives. Every field is required.
type AAD struct {
	HouseholdID string
	Table       string
	RowID       string
	Purpose     string
}

// bytes encodes the fields length-prefixed, so no choice of field values can collide.
func (a AAD) bytes() ([]byte, error) {
	out := []byte(aadLabel)
	for _, f := range []string{a.HouseholdID, a.Table, a.RowID, a.Purpose} {
		if f == "" {
			return nil, errors.New("keyring: every AAD field is required")
		}
		out = binary.AppendUvarint(out, uint64(len(f)))
		out = append(out, f...)
	}
	return out, nil
}

type key struct {
	id   string
	aead cipher.AEAD
	mac  []byte
}

// Keyring holds the parsed keys. The zero value has no keys; use Parse.
type Keyring struct{ keys []key }

// Parse reads "id:base64key,…". It refuses an empty list, a malformed or repeated id, and a
// key that is not exactly 32 bytes.
func Parse(s string) (*Keyring, error) {
	kr := &Keyring{}
	seen := map[string]bool{}
	for i, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		id, encoded, ok := strings.Cut(field, ":")
		if !ok || !idRE.MatchString(id) {
			return nil, fmt.Errorf("keyring: entry %d: want id:base64key with id of [a-z0-9-], at most 16", i+1)
		}
		if seen[id] {
			return nil, fmt.Errorf("keyring: key id %q appears twice", id)
		}
		seen[id] = true
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != keySize {
			return nil, fmt.Errorf("keyring: key %q must be %d bytes in standard base64", id, keySize)
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return nil, fmt.Errorf("keyring: key %q: %w", id, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("keyring: key %q: %w", id, err)
		}
		mac, err := hkdf.Key(sha256.New, raw, nil, hmacInfo, sha256.Size)
		if err != nil {
			return nil, fmt.Errorf("keyring: key %q: %w", id, err)
		}
		kr.keys = append(kr.keys, key{id: id, aead: aead, mac: mac})
	}
	return kr, nil
}

// UnmarshalText lets internal/config parse OPENCOZY_SECRET_KEYS straight into a Keyring.
func (kr *Keyring) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*kr = *parsed
	return nil
}

// IDs lists the key ids, the encrypting key first. Ids are not secret.
func (kr *Keyring) IDs() []string {
	ids := make([]string, len(kr.keys))
	for i, k := range kr.keys {
		ids[i] = k.id
	}
	return ids
}

// String and LogValue show key ids only, so printing or logging a Keyring never shows a key.
func (kr *Keyring) String() string { return "keyring(" + strings.Join(kr.IDs(), ",") + ")" }

// LogValue implements slog.LogValuer.
func (kr *Keyring) LogValue() slog.Value { return slog.StringValue(kr.String()) }

func (kr *Keyring) find(id string) (key, bool) {
	for _, k := range kr.keys {
		if k.id == id {
			return k, true
		}
	}
	return key{}, false
}

func (kr *Keyring) current() (key, error) {
	if kr == nil || len(kr.keys) == 0 {
		return key{}, errors.New("keyring: OPENCOZY_SECRET_KEYS is not set")
	}
	return kr.keys[0], nil
}

// Encrypt seals plaintext under the first key.
func (kr *Keyring) Encrypt(aad AAD, plaintext []byte) (string, error) {
	k, err := kr.current()
	if err != nil {
		return "", err
	}
	ad, err := aad.bytes()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := k.aead.Seal(nil, nonce, plaintext, ad)
	return "v1." + k.id + "." + b64.EncodeToString(nonce) + "." + b64.EncodeToString(sealed), nil
}

// Decrypt opens a value from Encrypt with the key its id names.
func (kr *Keyring) Decrypt(aad AAD, value string) ([]byte, error) {
	if kr == nil {
		return nil, errors.New("keyring: OPENCOZY_SECRET_KEYS is not set")
	}
	ad, err := aad.bytes()
	if err != nil {
		return nil, err
	}
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return nil, ErrDecrypt
	}
	k, ok := kr.find(parts[1])
	if !ok {
		return nil, ErrDecrypt
	}
	nonce, err1 := b64.DecodeString(parts[2])
	sealed, err2 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(nonce) != k.aead.NonceSize() {
		return nil, ErrDecrypt
	}
	plain, err := k.aead.Open(nil, nonce, sealed, ad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

// Rekey re-encrypts value under the first key, and reports whether it changed. A value
// already under the first key is returned as is.
func (kr *Keyring) Rekey(aad AAD, value string) (string, bool, error) {
	k, err := kr.current()
	if err != nil {
		return "", false, err
	}
	if strings.HasPrefix(value, "v1."+k.id+".") {
		if _, err := kr.Decrypt(aad, value); err != nil {
			return "", false, err
		}
		return value, false, nil
	}
	plain, err := kr.Decrypt(aad, value)
	if err != nil {
		return "", false, err
	}
	out, err := kr.Encrypt(aad, plain)
	return out, err == nil, err
}

func feedMAC(k key, householdID string, feedURL []byte) string {
	m := hmac.New(sha256.New, k.mac)
	m.Write([]byte(macLabel))
	var n [binary.MaxVarintLen64]byte
	m.Write(n[:binary.PutUvarint(n[:], uint64(len(householdID)))])
	m.Write([]byte(householdID))
	m.Write(feedURL)
	return "h1." + k.id + "." + b64.EncodeToString(m.Sum(nil))
}

// FeedMAC is the feed URL's MAC within its household under the first key, for the uniqueness
// column. The feed slice decides how a URL is normalised before it gets here.
func (kr *Keyring) FeedMAC(householdID string, feedURL []byte) (string, error) {
	k, err := kr.current()
	if err != nil {
		return "", err
	}
	return feedMAC(k, householdID, feedURL), nil
}

// FeedMACs is the MAC under every key, so a uniqueness lookup during a rotation also finds rows
// not yet rekeyed.
func (kr *Keyring) FeedMACs(householdID string, feedURL []byte) ([]string, error) {
	if _, err := kr.current(); err != nil {
		return nil, err
	}
	out := make([]string, len(kr.keys))
	for i, k := range kr.keys {
		out[i] = feedMAC(k, householdID, feedURL)
	}
	return out, nil
}
