package importer

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

var (
	//go:embed testdata/old_schema.sql
	oldSchemaSQL string
	//go:embed testdata/old_seed.sql
	oldSeedSQL string
)

// The fixture's secrets, made for it (testdata/old_seed.sql stores only their hashes).
const (
	fixturePassword  = "fixture-owner-password"
	fixtureReadToken = "ocz_W1-v8zbJaANPs1JmBbtLUMuPg_JCKYayABFBJ4F6zZc"
	fixtureGateToken = "ocz__koCd_CivNhz2YxuA1Boq99DgqwxWJ9ULvrZgF5RUd0"
	fixtureAsstToken = "oca_O_Fo_u99Yi1dz98twjLyVAfg2GzHdo9FMd_EIYafaS4"
)

// oldDB makes a database holding the old app's schema and the fictional seed, dropped when t
// ends, and returns its URL.
func oldDB(t *testing.T, arrange ...string) string {
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
	name := "old_" + hex.EncodeToString(b)
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
	old, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close(ctx) }()
	for _, sql := range append([]string{oldSchemaSQL, oldSeedSQL}, arrange...) {
		if _, err := old.Exec(ctx, sql); err != nil {
			t.Fatalf("old database: %v", err)
		}
	}
	return u.String()
}

func importOnce(t *testing.T, source string, target testdb.DB, dryRun bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), Options{SourceURL: source, Target: target.Owner, DryRun: dryRun, Out: &out})
	return out.String(), err
}

func empty(t *testing.T, target testdb.DB) {
	t.Helper()
	var n int
	if err := target.Owner.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM household) + (SELECT count(*) FROM adult) + (SELECT count(*) FROM credential)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the target has %d rows (%v)", n, err)
	}
}

// statuses parses the printed report into old table -> status.
func statuses(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		for _, s := range []string{"ok", "MISMATCH", "pending"} {
			for _, x := range f[1:] {
				if x == s {
					m[f[0]] = s
				}
			}
		}
		if strings.Contains(line, "not carried") && !strings.Contains(line, ", not carried") {
			m[f[0]] = "not carried"
		}
	}
	return m
}

func TestDryRunReconcilesAndRollsBack(t *testing.T) {
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t), target, true)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	empty(t, target)
	got := statuses(out)
	for table, want := range map[string]string{
		"household":                "pending", // lock_message and the game domains wait on later slices
		"child":                    "pending", // the colour and allowed hours too
		"auth_user":                "ok",
		"household_owner":          "ok",
		"auth_account":             "ok",
		"service_credential":       "pending", // the router's tv:gate has no new name yet
		"service_credential_child": "pending",
		"assistant_credential":     "pending",
		"auth_session":             "not carried",
	} {
		if got[table] != want {
			t.Errorf("%s: %q, want %q\n%s", table, got[table], want, out)
		}
	}
	for _, secret := range []string{"fixture-access-token", "fixture-refresh-token", "Alex", "example.test", "Avery", "Kitchen"} {
		if strings.Contains(out, secret) {
			t.Errorf("the report shows row contents (%q)", secret)
		}
	}
	if !strings.Contains(out, "tv:gate (1)") || !strings.Contains(out, "1 without a household") {
		t.Errorf("missing notes:\n%s", out)
	}
}

func TestRealImportRefusesWhilePending(t *testing.T) {
	target := testdb.Open(t)
	if _, err := importOnce(t, oldDB(t), target, false); !errors.Is(err, ErrPending) {
		t.Fatalf("err = %v, want ErrPending", err)
	}
	empty(t, target)
}

// complete removes what later slices will map, so a real import can commit.
const complete = `SET search_path TO opencozy;
DELETE FROM service_credential_child WHERE credential_id = '0190a000-0000-7000-8000-0000000000d2';
DELETE FROM service_credential WHERE id = '0190a000-0000-7000-8000-0000000000d2';
DELETE FROM assistant_credential;
ALTER TABLE household DROP COLUMN lock_message, DROP COLUMN game_portal_domains, DROP COLUMN game_domains;
ALTER TABLE child DROP COLUMN color, DROP COLUMN school_night_start, DROP COLUMN school_night_end, DROP COLUMN weekend_start, DROP COLUMN weekend_end;`

func TestImportCommitsAndOldSecretsWork(t *testing.T) {
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ctx := context.Background()
	q := dbq.New(target.Runtime)

	// The owner signs in with the old password, and is told to rehash.
	var hash, scheme string
	var handle []byte
	if err := target.Owner.QueryRow(ctx, `SELECT password_hash, password_scheme, webauthn_user_handle FROM adult WHERE email = 'owner@example.test'`).Scan(&hash, &scheme, &handle); err != nil {
		t.Fatal(err)
	}
	if ok, rehash, err := auth.Verify(scheme, hash, fixturePassword); !ok || !rehash || err != nil {
		t.Fatalf("old password: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if string(handle) != "fixtureOwnerUser01" {
		t.Errorf("user handle %q", handle)
	}
	var oidcOnly *string
	if err := target.Owner.QueryRow(ctx, `SELECT password_hash FROM adult WHERE email = 'sam@example.test'`).Scan(&oidcOnly); err != nil || oidcOnly != nil {
		t.Fatalf("OIDC-only adult: %v %v", oidcOnly, err)
	}

	// The old service secret authenticates, with exactly its old child.
	r, _ := http.NewRequest(http.MethodGet, "/api/children", nil)
	r.Header.Set("Authorization", "Bearer "+fixtureReadToken)
	a, ok, err := actor.Resolver{Q: q}.Resolve(ctx, r)
	if err != nil || !ok || a.Kind != actor.Service || a.AllChildren || len(a.Children) != 1 {
		t.Fatalf("old ocz_ secret: %+v ok=%v err=%v", a, ok, err)
	}

	// bootstrap-owner and a second import both refuse now.
	if _, err := importOnce(t, oldDB(t, complete), target, true); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("second import: %v", err)
	}
	_ = time.Second
}

func TestRefusesAnotherVersion(t *testing.T) {
	target := testdb.Open(t)
	for name, arrange := range map[string]string{
		"older":   `DELETE FROM opencozy.schema_migrations WHERE version = '0044_screen_time_grant_cut_guards'`,
		"edited":  `UPDATE opencozy.schema_migrations SET checksum = repeat('0', 64) WHERE version = '0010_screen_time_fulfillment'`,
		"newer":   `INSERT INTO opencozy.schema_migrations (version, checksum) VALUES ('0045_next', repeat('0', 64))`,
		"missing": `DROP TABLE opencozy.schema_migrations`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := importOnce(t, oldDB(t, arrange), target, true); err == nil || !strings.Contains(err.Error(), "old app") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSourceIsReadOnly(t *testing.T) {
	src, err := openSource(context.Background(), oldDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer src.close(context.Background())
	_, err = src.tx.Exec(context.Background(), "DELETE FROM opencozy.household")
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "25006" {
		t.Fatalf("a write on the source: %v", err)
	}
}

func TestSafeErrorsHideRows(t *testing.T) {
	err := safe(&pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "adult_email_key"`,
		Detail: "Key (email)=(alex@example.test) already exists.", TableName: "adult", ConstraintName: "adult_email_key"})
	if strings.Contains(err.Error(), "alex") || !strings.Contains(err.Error(), "adult_email_key") {
		t.Errorf("integrity: %v", err)
	}
	err = safe(&pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type uuid: "alex@example.test"`})
	if strings.Contains(err.Error(), "alex") {
		t.Errorf("other: %v", err)
	}
}
