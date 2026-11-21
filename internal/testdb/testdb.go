// Package testdb gives each integration test its own migrated PostgreSQL database.
//
// The server comes from TEST_DATABASE_URL (any database on a disposable server, with a role that
// may CREATE DATABASE and CREATE ROLE). Without it the test is skipped, except in CI (CI=true),
// where it fails: a silent skip would hide missing coverage. `make test` starts one in Docker
// Compose.
//
// Like production, PUBLIC has no CONNECT on the database, the schema is migrated by its owner
// (the admin role), and the application connects as a runtime role that can only read and write
// rows.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/db"
)

// DB is a fresh test database.
type DB struct {
	// Runtime is the pool the server uses: rows only.
	Runtime *pgxpool.Pool
	// Owner owns the schema. Tests use it to inspect or arrange what the runtime role cannot.
	Owner *pgxpool.Pool
}

// New creates and migrates a fresh database, dropped when t ends, and returns the runtime pool.
func New(t testing.TB) *pgxpool.Pool { t.Helper(); return Open(t).Runtime }

// Open is New with the owner's pool as well.
func Open(t testing.TB) DB {
	t.Helper()
	admin := config.TestDatabaseURL()
	if admin == "" {
		if config.InCI() {
			t.Fatal("TEST_DATABASE_URL is not set")
		}
		t.Skip("TEST_DATABASE_URL is not set; run `make test` for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "test_" + hex.EncodeToString(b)
	role := name + "_app"
	pw := make([]byte, 16)
	_, _ = rand.Read(pw)
	password := hex.EncodeToString(pw)

	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("testdb: create: %v", err)
	}
	// As production's databases (the CNPG bootstraps): no CONNECT for PUBLIC, so the runtime
	// role connects only because migrations grant it.
	if _, err := conn.Exec(ctx, "REVOKE ALL ON DATABASE "+pgx.Identifier{name}.Sanitize()+" FROM PUBLIC"); err != nil {
		t.Fatalf("testdb: revoke: %v", err)
	}
	// The password is random hex, so quoting it is safe.
	if _, err := conn.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatalf("testdb: create role: %v", err)
	}

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("testdb: parse url: %v", err)
	}
	u.Path = "/" + name
	owner, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	var runtime *pgxpool.Pool
	t.Cleanup(func() {
		if runtime != nil {
			runtime.Close()
		}
		owner.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Errorf("testdb: cleanup connect: %v", err)
			return
		}
		defer func() { _ = c.Close(ctx) }()
		if _, err := c.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("testdb: drop: %v", err)
		}
		if _, err := c.Exec(ctx, "DROP ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
			t.Errorf("testdb: drop role: %v", err)
		}
	})
	if err := db.Migrate(ctx, owner, slog.New(slog.NewTextHandler(io.Discard, nil)), role); err != nil {
		t.Fatalf("testdb: %v", err)
	}
	u.User = url.UserPassword(role, password)
	if runtime, err = db.Open(ctx, u.String()); err != nil {
		t.Fatalf("testdb: runtime: %v", err)
	}
	return DB{Runtime: runtime, Owner: owner}
}

// Scratch makes an empty database with no migrations, runs each SQL script in it in order, and
// returns its URL; it is dropped when t ends. The importer's tests use it for the old app's
// database.
func Scratch(t testing.TB, scripts ...string) string {
	t.Helper()
	admin := config.TestDatabaseURL()
	if admin == "" {
		if config.InCI() {
			t.Fatal("TEST_DATABASE_URL is not set")
		}
		t.Skip("TEST_DATABASE_URL is not set; run `make test` for integration tests")
	}
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "scratch_" + hex.EncodeToString(b)
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	for _, sql := range scripts {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatalf("scratch database: %v", err)
		}
	}
	return u.String()
}
