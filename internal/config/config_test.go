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
