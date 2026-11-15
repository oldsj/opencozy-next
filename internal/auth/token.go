package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// Bearer credential prefixes. The prefix says what kind of credential a secret is, so a leaked
// one is easy to recognise and route to the right revocation.
const (
	PrefixService   = "ocz_"
	PrefixAssistant = "oca_"
	PrefixPhone     = "ocp_"
)

// tokenLen is the length of base64url(32 bytes) without padding.
const tokenLen = 43

// NewToken returns prefix + 32 random bytes in base64url, and the hash to store.
func NewToken(prefix string) (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// ParseBearer splits a bearer secret into its prefix and reports whether it is well formed: a
// known prefix followed by exactly 43 base64url characters. A malformed secret is never looked up.
func ParseBearer(secret string) (prefix string, ok bool) {
	for _, p := range []string{PrefixService, PrefixAssistant, PrefixPhone} {
		if rest, found := strings.CutPrefix(secret, p); found {
			return p, wellFormed(rest)
		}
	}
	return "", false
}

// WellFormedCookie reports whether a cookie value can be a token from NewSessionToken.
func WellFormedCookie(v string) bool { return wellFormed(v) }

func wellFormed(s string) bool {
	if len(s) != tokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil
}

// NewUserHandle makes a WebAuthn user handle: 32 random bytes that identify an adult to their
// passkeys without revealing anything about them.
func NewUserHandle() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
