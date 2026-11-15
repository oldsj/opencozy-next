// Package oldsecret decrypts the old app's integration secrets (Google tokens, iCloud app
// passwords, the Home Assistant token) during import, so they can be sealed again under the new
// keyring. The old app encrypted each with AES-256-GCM under a key derived by HKDF-SHA256 from
// its BETTER_AUTH_SECRET (empty salt, one info string per use) and stored hex "iv:tag:data".
//
// The old secret is read once from a file and never stored or logged; neither is anything
// decrypted with it. Without it, those integrations import as "reconnect needed".
package oldsecret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// The old app's HKDF info strings, one per use; a ciphertext of one use never opens under
// another. They are labels, not secrets.
//
//nolint:gosec // G101: info strings, not credentials
const (
	GoogleToken        = "opencozy-calendar-google-token-v1"
	ICloudPassword     = "opencozy-icloud-credential-v1"
	HomeAssistantToken = "opencozy-home-assistant-token-v1"
)

// ErrUnreadable means a stored value does not open under the old secret: the wrong secret, the
// wrong use, or a damaged value. Its message never includes the value.
var ErrUnreadable = errors.New("oldsecret: the value does not decrypt with this old secret")

// Secret is the old app's BETTER_AUTH_SECRET.
type Secret struct {
	ikm  []byte // the secret: read without its trailing newline
	read []byte // everything read, cleared by Forget
}

// Load reads the old secret from a file (its own line; a trailing newline is ignored). Like the
// old app, it must be at least 32 characters. The secret is kept only in the bytes read, so
// Forget can clear it.
func Load(path string) (*Secret, error) {
	b, err := os.ReadFile(path) //nolint:gosec // an operator-given path, read once
	if err != nil {
		return nil, fmt.Errorf("oldsecret: read the old secret file: %w", err)
	}
	ikm := bytes.TrimRight(b, "\r\n")
	if len(ikm) < 32 {
		clear(b)
		return nil, errors.New("oldsecret: the old secret is shorter than 32 characters; is it the right file?")
	}
	return &Secret{ikm: ikm, read: b}, nil
}

// Loose reports a file others can read: group or world permissions. The import warns.
func Loose(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().Perm()&0o077 != 0
}

// Decrypt opens one stored value for one use (GoogleToken, ICloudPassword, HomeAssistantToken).
func (s *Secret) Decrypt(info, stored string) ([]byte, error) {
	parts := strings.Split(stored, ":")
	if len(parts) != 3 {
		return nil, ErrUnreadable
	}
	var raw [3][]byte
	for i, p := range parts {
		b, err := hex.DecodeString(p)
		if err != nil || len(b) == 0 {
			return nil, ErrUnreadable
		}
		raw[i] = b
	}
	iv, tag, data := raw[0], raw[1], raw[2]
	if len(iv) != 12 || len(tag) != 16 {
		return nil, ErrUnreadable
	}
	key, err := hkdf.Key(sha256.New, s.ikm, nil, info, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, iv, append(data, tag...), nil)
	if err != nil {
		return nil, ErrUnreadable
	}
	return plain, nil
}

// Forget overwrites the secret in memory once the import no longer needs it.
func (s *Secret) Forget() {
	if s == nil {
		return
	}
	clear(s.read)
	s.ikm, s.read = nil, nil
}
