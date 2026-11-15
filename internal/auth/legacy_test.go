package auth

import (
	"encoding/hex"
	"strings"
	"testing"

	"golang.org/x/crypto/scrypt"
)

// betterAuthHash builds a hash the way the old app (Better Auth) does.
func betterAuthHash(t *testing.T, saltHex, password string) string {
	t.Helper()
	key, err := scrypt.Key([]byte(password), []byte(saltHex), 16384, 16, 1, 64)
	if err != nil {
		t.Fatal(err)
	}
	return saltHex + ":" + hex.EncodeToString(key)
}

func TestVerifyScryptBA(t *testing.T) {
	const salt = "00112233445566778899aabbccddeeff"
	hash := betterAuthHash(t, salt, "correct horse battery")

	ok, rehash, err := Verify(SchemeScryptBA, hash, "correct horse battery")
	if err != nil || !ok || !rehash {
		t.Fatalf("right password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	ok, rehash, err = Verify(SchemeScryptBA, hash, "wrong")
	if err != nil || ok || rehash {
		t.Fatalf("wrong password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	// NFKC: a full-width letter normalises to the ASCII one before hashing.
	ok, _, _ = Verify(SchemeScryptBA, betterAuthHash(t, salt, "pass A"), "pass Ａ")
	if !ok {
		t.Fatal("NFKC normalisation not applied")
	}
	for _, bad := range []string{"", "nocolon", salt + ":zz", strings.Repeat("0", 31) + ":" + strings.Repeat("0", 128)} {
		if _, _, err := Verify(SchemeScryptBA, bad, "x"); err == nil {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
	if _, _, err := Verify("md5", hash, "x"); err == nil {
		t.Error("unknown scheme accepted")
	}
}

func TestVerifyArgon2idNoRehash(t *testing.T) {
	h, _ := HashPassword("a long enough password")
	ok, rehash, err := Verify(SchemeArgon2id, h, "a long enough password")
	if err != nil || !ok || rehash {
		t.Fatalf("ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestParseBearer(t *testing.T) {
	tok, _, _ := NewToken(PrefixService)
	if p, ok := ParseBearer(tok); !ok || p != PrefixService {
		t.Fatalf("own token: %q %v", p, ok)
	}
	for _, bad := range []string{"", "ocz_", tok + "A", tok[:len(tok)-1], "ocx_" + tok[4:], strings.Replace(tok, tok[5:6], "+", 1)} {
		if _, ok := ParseBearer(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}
