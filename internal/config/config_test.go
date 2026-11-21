package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without DATABASE_URL")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db.example:5432/app")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
}

func TestLoadRejectsPlaintextRemoteDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db.example:5432/app?sslmode=disable")
	if _, err := Load(); err == nil {
		t.Fatal("sslmode=disable to a remote host was accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/app?sslmode=disable")
	if _, err := Load(); err != nil {
		t.Fatalf("localhost without TLS rejected: %v", err)
	}
}

func TestLoadValidatesTTL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/app")
	t.Setenv("SESSION_TTL", "1s")
	if _, err := Load(); err == nil {
		t.Fatal("SESSION_TTL=1s accepted")
	}
}

func TestLoadNeedsRuntimeRoleWithMigrationURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://app:p@localhost/app")
	t.Setenv("MIGRATION_DATABASE_URL", "postgres://owner:p@localhost/app")
	if _, err := Load(); err == nil {
		t.Fatal("MIGRATION_DATABASE_URL without DATABASE_RUNTIME_ROLE was accepted")
	}
	t.Setenv("DATABASE_RUNTIME_ROLE", "app")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MigrationURL() != "postgres://owner:p@localhost/app" {
		t.Errorf("MigrationURL = %q", cfg.MigrationURL())
	}
}

func TestLoadValidatesPublicOrigin(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/app")
	for _, o := range []string{"https://cozy.example.test", "https://cozy.example.test:8443", "http://localhost:8080"} {
		t.Setenv("PUBLIC_ORIGIN", o)
		if _, err := Load(); err != nil {
			t.Errorf("%s refused: %v", o, err)
		}
	}
	for _, o := range []string{"http://cozy.example.test", "https://cozy.example.test/", "https://cozy.example.test/app", "cozy.example.test", "https://u@cozy.example.test"} {
		t.Setenv("PUBLIC_ORIGIN", o)
		if _, err := Load(); err == nil {
			t.Errorf("%s accepted", o)
		}
	}
}

func TestLoadValidatesApple(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/app")
	for name, env := range map[string]map[string]string{
		"team without bundle": {"APPLE_TEAM_ID": "ABCDE12345"},
		"bad team":            {"APPLE_TEAM_ID": "abc", "APPLE_BUNDLE_ID": "org.example.opencozy"},
		"key without team":    {"APNS_KEY_ID": "KEY1234567", "APNS_KEY": "x"},
		"bad key": {"APPLE_TEAM_ID": "ABCDE12345", "APPLE_BUNDLE_ID": "org.example.opencozy",
			"APNS_KEY_ID": "KEY1234567", "APNS_KEY": "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, k := range []string{"APPLE_TEAM_ID", "APPLE_BUNDLE_ID", "APNS_KEY_ID", "APNS_KEY"} {
				t.Setenv(k, env[k])
			}
			_, err := Load()
			if err == nil {
				t.Fatal("accepted")
			}
			if strings.Contains(err.Error(), "BEGIN") {
				t.Error("the error echoes the key")
			}
		})
	}
	t.Setenv("APPLE_TEAM_ID", "ABCDE12345")
	t.Setenv("APPLE_BUNDLE_ID", "org.example.opencozy")
	t.Setenv("APNS_KEY_ID", "")
	t.Setenv("APNS_KEY", "")
	if c, err := Load(); err != nil || c.PushEnabled() {
		t.Fatalf("passkeys only: %v push=%v", err, c.PushEnabled())
	}
}
