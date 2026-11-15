package oldsecret

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Values the old app's own code (Node's crypto, as in its src/lib/server/crypto.ts) encrypted
// under a fictional secret, so this pins the real format rather than a Go round trip.
const fixtureSecret = "fixture-old-auth-secret-0123456789abcdefghij"

var vectors = []struct{ info, stored, plain string }{
	{GoogleToken, "872dc466ff84ba59ca64297f:b995293500d7e16c130cef443823d573:d5582b7fa0096d63824808adf3a34e0422fe027ae99fda0f6574e28a", "fixture-google-refresh-token"},
	{ICloudPassword, "0f2972d89dcda0e5bb8c28b5:f90764f9634ccfdd13a2996be53bf5c1:37584b02d690a91e31e59a827319a67173e523a6", "fixture-app-password"},
	{HomeAssistantToken, "28af2f2b321a2654f6a0418e:082b6f65cf25691e8db62f1259ca81f0:a466ac1366150b6e1249293cf530eb64", "fixture-ha-token"},
}

func load(t *testing.T, content string) (*Secret, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old-secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestDecryptsTheOldAppsValues(t *testing.T) {
	s, err := load(t, fixtureSecret+"\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		got, err := s.Decrypt(v.info, v.stored)
		if err != nil || string(got) != v.plain {
			t.Errorf("%s: %q, %v", v.info, got, err)
		}
	}
}

func TestRefusesWhatIsNotItsOwn(t *testing.T) {
	s, err := load(t, fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	other, err := load(t, strings.Replace(fixtureSecret, "fixture", "another", 1))
	if err != nil {
		t.Fatal(err)
	}
	g := vectors[0]
	parts := strings.Split(g.stored, ":")
	data, _ := hex.DecodeString(parts[2])
	data[0] ^= 1
	tampered := parts[0] + ":" + parts[1] + ":" + hex.EncodeToString(data)
	for name, try := range map[string]func() ([]byte, error){
		"another use":     func() ([]byte, error) { return s.Decrypt(ICloudPassword, g.stored) },
		"another secret":  func() ([]byte, error) { return other.Decrypt(g.info, g.stored) },
		"tampered":        func() ([]byte, error) { return s.Decrypt(g.info, tampered) },
		"not an envelope": func() ([]byte, error) { return s.Decrypt(g.info, "plaintext-token") },
		"short iv":        func() ([]byte, error) { return s.Decrypt(g.info, "00:"+strings.SplitN(g.stored, ":", 2)[1]) },
	} {
		got, err := try()
		if !errors.Is(err, ErrUnreadable) || got != nil {
			t.Errorf("%s: %q, %v", name, got, err)
		}
		if err != nil && strings.Contains(err.Error(), g.stored) {
			t.Errorf("%s: the error shows the value", name)
		}
	}
}

func TestLoad(t *testing.T) {
	if _, err := load(t, "too short\n"); err == nil {
		t.Error("a short secret was accepted")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file was accepted")
	}
	s, err := load(t, fixtureSecret)
	if err != nil {
		t.Fatal(err)
	}
	s.Forget()
	if s.ikm != nil {
		t.Error("Forget kept the secret")
	}
}
