package auth

import (
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"
)

// Password schemes stored next to each hash.
const (
	SchemeArgon2id = "argon2id"
	// SchemeScryptBA is the old app's (Better Auth) format, kept for imported adults until their
	// next successful sign-in rehashes the password to argon2id.
	SchemeScryptBA = "scrypt-ba"
)

// Better Auth's scrypt parameters.
const (
	baN      = 16384
	baR      = 16
	baP      = 1
	baKeyLen = 64
)

// checkScryptBA verifies hex(salt):hex(key), where key = scrypt(NFKC(password), the salt's hex
// text as bytes, N=16384, r=16, p=1, 64 bytes).
func checkScryptBA(hash, password string) (bool, error) {
	saltHex, keyHex, ok := strings.Cut(hash, ":")
	if !ok || len(saltHex) != 32 || len(keyHex) != 2*baKeyLen {
		return false, errBadHash
	}
	if _, err := hex.DecodeString(saltHex); err != nil {
		return false, errBadHash
	}
	want, err := hex.DecodeString(keyHex)
	if err != nil {
		return false, errBadHash
	}
	got, err := scrypt.Key([]byte(norm.NFKC.String(password)), []byte(saltHex), baN, baR, baP, baKeyLen)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Verify checks password against a stored hash of the given scheme. rehash is true when the
// password matched but is stored in an old scheme and should be replaced with HashPassword's.
func Verify(scheme, hash, password string) (ok, rehash bool, err error) {
	switch scheme {
	case SchemeArgon2id:
		ok, err = CheckPassword(hash, password)
		return ok, false, err
	case SchemeScryptBA:
		ok, err = checkScryptBA(hash, password)
		return ok, ok, err
	default:
		return false, false, errBadHash
	}
}
