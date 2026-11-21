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

func TestLoadGoogle(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/app")
	t.Setenv("GOOGLE_CLIENT_ID", "fixture-client.apps.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("a Google client ID alone was accepted")
	}
	t.Setenv("GOOGLE_CLIENT_SECRET", "fixture-client-secret")
	if _, err := Load(); err == nil {
		t.Fatal("Google without PUBLIC_ORIGIN was accepted")
	}
	t.Setenv("PUBLIC_ORIGIN", "https://cozy.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("Google without OPENCOZY_SECRET_KEYS was accepted")
	}
	t.Setenv("OPENCOZY_SECRET_KEYS", "k1:BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc=")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GoogleEnabled() || cfg.GoogleRedirectURL() != "https://cozy.example.test/api/calendar/google/callback" {
		t.Errorf("enabled %v, redirect %q", cfg.GoogleEnabled(), cfg.GoogleRedirectURL())
	}
}

func TestLoadValidatesOIDC(t *testing.T) {
	full := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost/app", "PUBLIC_ORIGIN": "https://cozy.example.test",
		"OIDC_ISSUER": "https://id.example.test", "OIDC_CLIENT_ID": "opencozy", "OIDC_CLIENT_SECRET": "fixture-secret",
	}
	set := func(over map[string]string) {
		for k, v := range full {
			t.Setenv(k, v)
		}
		for k, v := range over {
			t.Setenv(k, v)
		}
	}
	set(nil)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OIDCEnabled() || cfg.OIDCName != "single sign-on" {
		t.Errorf("enabled %v, name %q", cfg.OIDCEnabled(), cfg.OIDCName)
	}
	set(map[string]string{"OIDC_ISSUER": "", "OIDC_CLIENT_ID": "", "OIDC_CLIENT_SECRET": "", "PUBLIC_ORIGIN": ""})
	if cfg, err := Load(); err != nil || cfg.OIDCEnabled() {
		t.Errorf("none set: %v, enabled %v", err, cfg.OIDCEnabled())
	}
	for name, over := range map[string]map[string]string{
		"no secret":        {"OIDC_CLIENT_SECRET": ""},
		"only the issuer":  {"OIDC_CLIENT_ID": "", "OIDC_CLIENT_SECRET": ""},
		"http issuer":      {"OIDC_ISSUER": "http://id.example.test"},
		"loopback http":    {"OIDC_ISSUER": "http://127.0.0.1:9000"},
		"query":            {"OIDC_ISSUER": "https://id.example.test/?x=1"},
		"fragment":         {"OIDC_ISSUER": "https://id.example.test/#x"},
		"user":             {"OIDC_ISSUER": "https://u@id.example.test"},
		"not a URL":        {"OIDC_ISSUER": "not a url"},
		"no public origin": {"PUBLIC_ORIGIN": ""},
		"long name":        {"OIDC_NAME": "a name far too long to fit on the sign-in button"},
		"spaced client":    {"OIDC_CLIENT_ID": " opencozy"},
	} {
		if name == "loopback http" && E2EBuild {
			continue // an e2e build's fake provider (validIssuer)
		}
		set(over)
		if _, err := Load(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
