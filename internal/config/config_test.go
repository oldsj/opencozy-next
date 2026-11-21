package config

import (
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
		set(over)
		if _, err := Load(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
