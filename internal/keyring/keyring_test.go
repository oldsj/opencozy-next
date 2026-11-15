package keyring

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Fictional test keys, never used anywhere else.
var (
	keyA = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xa1}, 32))
	keyB = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xb2}, 32))
	aad  = AAD{HouseholdID: "6f9c1d2e-0000-4000-8000-000000000001", Table: "ha_connection", RowID: "7", Purpose: "token"}
)

func mustParse(t *testing.T, s string) *Keyring {
	t.Helper()
	kr, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestParse(t *testing.T) {
	kr := mustParse(t, "k2:"+keyB+", k1:"+keyA)
	if got := strings.Join(kr.IDs(), ","); got != "k2,k1" {
		t.Fatalf("ids %s", got)
	}
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	for _, bad := range []string{
		"", "k1", ":" + keyA, "K1:" + keyA, "k.1:" + keyA, "-k:" + keyA, "abcdefghijklmnopq:" + keyA,
		"k1:" + short, "k1:not-base64", "k1:" + keyA + ",k1:" + keyB, "k1:" + keyA + ",",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		} else if strings.Contains(err.Error(), keyA) || strings.Contains(err.Error(), keyB) {
			t.Errorf("Parse error shows a key: %v", err)
		}
	}
}

func TestNeverShowsKeys(t *testing.T) {
	kr := mustParse(t, "k1:"+keyA)
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "keyring", kr)
	for _, s := range []string{kr.String(), fmt.Sprint(kr), fmt.Sprintf("%v %s", kr, kr), logged.String()} {
		if strings.Contains(s, keyA) || !strings.Contains(s, "k1") {
			t.Errorf("shown as %q", s)
		}
	}
}

func TestEncryptDecrypt(t *testing.T) {
	kr := mustParse(t, "k1:"+keyA)
	v, err := kr.Encrypt(aad, []byte("fictional-token"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "v1.k1.") || strings.Count(v, ".") != 3 || strings.Contains(v, "fictional") {
		t.Fatalf("value %q", v)
	}
	if again, _ := kr.Encrypt(aad, []byte("fictional-token")); again == v {
		t.Fatal("nonce reused")
	}
	if got, err := kr.Decrypt(aad, v); err != nil || string(got) != "fictional-token" {
		t.Fatalf("got %q, %v", got, err)
	}
	// Any other row, table, household or purpose refuses the value.
	for _, other := range []AAD{
		{aad.HouseholdID[:35] + "2", aad.Table, aad.RowID, aad.Purpose},
		{aad.HouseholdID, "ics_feed", aad.RowID, aad.Purpose},
		{aad.HouseholdID, aad.Table, "8", aad.Purpose},
		{aad.HouseholdID, aad.Table, aad.RowID, "url"},
		// Shifting a boundary between fields is a different AAD.
		{aad.HouseholdID, "ha_connectio", "n7", aad.Purpose},
	} {
		if _, err := kr.Decrypt(other, v); !errors.Is(err, ErrDecrypt) {
			t.Errorf("Decrypt with %+v: %v, want ErrDecrypt", other, err)
		}
	}
	parts := strings.Split(v, ".")
	for _, bad := range []string{
		"", "v2" + v[2:], "v1.k9." + parts[2] + "." + parts[3], "v1.k1." + parts[2] + "." + parts[3] + "A",
		"v1.k1." + parts[2], "v1.k1.!!." + parts[3],
	} {
		if _, err := kr.Decrypt(aad, bad); !errors.Is(err, ErrDecrypt) {
			t.Errorf("Decrypt(%q): %v, want ErrDecrypt", bad, err)
		}
	}
	if _, err := kr.Encrypt(AAD{HouseholdID: "h"}, nil); err == nil {
		t.Error("Encrypt accepted a partial AAD")
	}
}

func TestNoKeys(t *testing.T) {
	var kr *Keyring
	if _, err := kr.Encrypt(aad, nil); err == nil || !strings.Contains(err.Error(), "OPENCOZY_SECRET_KEYS") {
		t.Errorf("Encrypt: %v", err)
	}
	if _, err := kr.Decrypt(aad, "v1.a.b.c"); err == nil {
		t.Error("Decrypt with no keys")
	}
	if _, err := kr.FeedMAC("h", nil); err == nil {
		t.Error("FeedMAC with no keys")
	}
}

func TestRotation(t *testing.T) {
	old := mustParse(t, "k1:"+keyA)
	v1, _ := old.Encrypt(aad, []byte("fictional-token"))

	rotated := mustParse(t, "k2:"+keyB+",k1:"+keyA)
	if got, err := rotated.Decrypt(aad, v1); err != nil || string(got) != "fictional-token" {
		t.Fatalf("old value under the rotated keyring: %q, %v", got, err)
	}
	v2, changed, err := rotated.Rekey(aad, v1)
	if err != nil || !changed || !strings.HasPrefix(v2, "v1.k2.") {
		t.Fatalf("Rekey: %q, %v, %v", v2, changed, err)
	}
	if same, changed, err := rotated.Rekey(aad, v2); err != nil || changed || same != v2 {
		t.Fatalf("Rekey again: %v, %v", changed, err)
	}
	if _, _, err := rotated.Rekey(AAD{aad.HouseholdID, aad.Table, "8", aad.Purpose}, v2); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("Rekey with the wrong AAD: %v", err)
	}
	// Once k1 is dropped, the rekeyed value still decrypts and the old one does not.
	after := mustParse(t, "k2:"+keyB)
	if _, err := after.Decrypt(aad, v2); err != nil {
		t.Fatal(err)
	}
	if _, err := after.Decrypt(aad, v1); !errors.Is(err, ErrDecrypt) {
		t.Fatal("dropped key still decrypts")
	}
}

func TestFeedMAC(t *testing.T) {
	kr := mustParse(t, "k1:"+keyA)
	url := []byte("https://calendar.example.test/fictional.ics")
	m, err := kr.FeedMAC("household-1", url)
	if err != nil || !strings.HasPrefix(m, "h1.k1.") {
		t.Fatalf("%q %v", m, err)
	}
	if again, _ := kr.FeedMAC("household-1", url); again != m {
		t.Fatal("not deterministic")
	}
	if other, _ := kr.FeedMAC("household-2", url); other == m {
		t.Fatal("households share a MAC")
	}
	// The MAC key is derived, not the encryption key itself, and differs per keyring key.
	rotated := mustParse(t, "k2:"+keyB+",k1:"+keyA)
	all, _ := rotated.FeedMACs("household-1", url)
	if len(all) != 2 || !strings.HasPrefix(all[0], "h1.k2.") || all[1] != m {
		t.Fatalf("FeedMACs %v", all)
	}
	if k, _ := kr.find("k1"); bytes.Equal(k.mac, bytes.Repeat([]byte{0xa1}, 32)) {
		t.Fatal("HMAC key equals the encryption key")
	}
}

func TestRekeyAll(t *testing.T) {
	saved := columns
	t.Cleanup(func() { columns = saved })
	columns = nil
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	kr := mustParse(t, "k1:"+keyA)

	if err := RekeyAll(t.Context(), nil, nil, log); err == nil {
		t.Fatal("ran with no keys")
	}
	if err := RekeyAll(t.Context(), nil, kr, log); err != nil {
		t.Fatalf("no columns: %v", err)
	}
	var ran []string
	Register("a.secret", func(context.Context, *pgxpool.Pool, *Keyring) (int, error) { ran = append(ran, "a"); return 1, nil })
	Register("b.secret", func(context.Context, *pgxpool.Pool, *Keyring) (int, error) {
		ran = append(ran, "b")
		return 0, errors.New("boom")
	})
	Register("c.secret", func(context.Context, *pgxpool.Pool, *Keyring) (int, error) { ran = append(ran, "c"); return 0, nil })
	if err := RekeyAll(t.Context(), nil, kr, log); err == nil || !strings.Contains(err.Error(), "b.secret") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(ran, "") != "ab" {
		t.Fatalf("ran %v, want a then b, stopping there", ran)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a second registration of a.secret did not panic")
		}
	}()
	Register("a.secret", nil)
}
