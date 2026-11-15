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
	if cfg.AllowSignup {
		t.Error("signup must be off by default")
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
