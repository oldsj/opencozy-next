package importer

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/calendar"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/importer/oldsecret"
	"github.com/oldsj/opencozy-next/internal/keyring"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

var (
	//go:embed testdata/old_schema.sql
	oldSchemaSQL string
	//go:embed testdata/old_seed.sql
	oldSeedSQL string
	// complete removes what later slices will map, so a real import can commit.
	//go:embed testdata/complete.sql
	complete string
)

// The fixture's secrets, made for it (testdata/old_seed.sql stores only their hashes).
const (
	fixturePassword   = "fixture-owner-password"
	fixtureReadToken  = "ocz_W1-v8zbJaANPs1JmBbtLUMuPg_JCKYayABFBJ4F6zZc"
	fixtureGateToken  = "ocz__koCd_CivNhz2YxuA1Boq99DgqwxWJ9ULvrZgF5RUd0"
	fixtureAsstToken  = "oca_O_Fo_u99Yi1dz98twjLyVAfg2GzHdo9FMd_EIYafaS4"
	fixtureLearnToken = "ocz_f1QOWWJY-49KChmmsnZw7-vg4mzAPST9AS4AoWeo9P0"
)

// oldDB makes a database holding the old app's schema and the fictional seed, dropped when t
// ends, and returns its URL.
func oldDB(t *testing.T, arrange ...string) string {
	t.Helper()
	return testdb.Scratch(t, append([]string{oldSchemaSQL, oldSeedSQL}, arrange...)...)
}

func importOnce(t *testing.T, source string, target testdb.DB, dryRun bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), Options{SourceURL: source, Target: target.Owner, DryRun: dryRun, Out: &out, Keys: testKeys(t)})
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
		"household":                     "pending", // lock_message waits on the devices import
		"child":                         "pending", // the colour too
		"auth_user":                     "ok",
		"household_owner":               "ok",
		"auth_account":                  "ok",
		"service_credential":            "ok",
		"service_credential_child":      "ok",
		"assistant_credential":          "pending", // approvals:read has no new name yet
		"gated_tv":                      "ok",
		"gated_tv_mac":                  "ok",
		"tv_session":                    "ok",
		"tv_open_event":                 "ok",
		"tv_credit":                     "pending", // its redemption waits for the rewards import
		"tv_prompt":                     "not carried",
		"auth_session":                  "not carried",
		"recipe":                        "ok",
		"meal_plan_entry":               "ok",
		"award_rule":                    "ok",
		"external_identity_link":        "ok",
		"service_credential_award_rule": "ok",
		"calendar_source":               "ok",
		"calendar_series":               "ok",
		"calendar_assignment":           "ok",
		"calendar_assignment_person":    "ok",
		"calendar_occurrence":           "not carried",
		"calendar_google_connection":    "ok",
	} {
		if got[table] != want {
			t.Errorf("%s: %q, want %q\n%s", table, got[table], want, out)
		}
	}
	for _, secret := range []string{"fixture-access-token", "fixture-refresh-token", "Alex", "example.test", "Avery", "Kitchen", "Lentil", "Builder_42", "build_house", "fixture-feed-token", "School", "swim@", "Swimming", "fixture-sync-token", "Piano", "Family", "1001"} {
		if strings.Contains(out, secret) {
			t.Errorf("the report shows row contents (%q)", secret)
		}
	}
	if !strings.Contains(out, "1 open sessions dropped") || !strings.Contains(out, "power and router attempts forgotten") {
		t.Errorf("the report doesn't count the dropped TV sessions or forgotten power:\n%s", out)
	}
	if !strings.Contains(out, "approvals:read (1)") || !strings.Contains(out, "1 without a household") {
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

func TestImportCommitsAndOldSecretsWork(t *testing.T) {
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ctx := context.Background()
	q := dbq.New(target.Runtime)
	keys := testKeys(t)

	// Blocked websites come with their household.
	var filter string
	if err := target.Owner.QueryRow(ctx, `SELECT game_portal_domains::text || ' ' || game_domains::text FROM household`).Scan(&filter); err != nil ||
		filter != "{games.example.test} {blocks.example.test,puzzles.example.test}" {
		t.Errorf("web filter: %q %v", filter, err)
	}
	// Allowed hours come with their child; children without any stay without.
	var hours string
	if err := target.Owner.QueryRow(ctx, `SELECT string_agg(name || '=' || COALESCE(school_night_start || '-' || school_night_end || '/' || weekend_start || '-' || weekend_end, 'none'), ' ' ORDER BY name)
		FROM child`).Scan(&hours); err != nil || hours != "Avery=900-1140/540-1200 Mika=none Rowan=none" {
		t.Errorf("allowed hours: %q %v", hours, err)
	}

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
	// Recipes keep their IDs, steps (line breaks included), versions and archive times; meals keep
	// their day, slot and recipe.
	var (
		steps    []string
		version  int32
		archived bool
	)
	if err := target.Owner.QueryRow(ctx, `SELECT steps, version, archived_at IS NOT NULL FROM recipe WHERE id = '0190a000-0000-7000-8000-0000000000f1'`).Scan(&steps, &version, &archived); err != nil ||
		len(steps) != 2 || steps[1] != "Add the lentils and stock.\nSimmer for 20 minutes." || version != 2 || archived {
		t.Errorf("recipe: %q %d %v %v", steps, version, archived, err)
	}
	var meals int
	if err := target.Owner.QueryRow(ctx, `SELECT count(*) FROM meal WHERE household_id = '0190a000-0000-7000-8000-00000000000a'
		AND ((day = '2026-01-12' AND slot = 'dinner' AND recipe_id = '0190a000-0000-7000-8000-0000000000f1' AND version = 1)
		  OR (day = '2026-01-13' AND slot = 'breakfast' AND recipe_id = '0190a000-0000-7000-8000-0000000000f2' AND version = 3))
		AND title IS NULL AND note IS NULL`).Scan(&meals); err != nil || meals != 2 {
		t.Errorf("meals: %d %v", meals, err)
	}

	// The learning credential keeps its rule; rules keep versions and archive times; links keep
	// whether they were unlinked.
	var learning string
	if err := target.Owner.QueryRow(ctx, `SELECT c.scopes::text || ' ' || r.activity_key || ' ' || r.version
		FROM credential c JOIN credential_award_rule cr ON cr.credential_id = c.id JOIN award_rule r ON r.id = cr.rule_id
		WHERE c.id = '0190a000-0000-7000-8000-0000000000d3'`).Scan(&learning); err != nil || learning != "{learning:award} build_house 2" {
		t.Errorf("learning credential: %q %v", learning, err)
	}
	var archivedRules, unlinked, live int
	if err := target.Owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM award_rule WHERE archived_at IS NOT NULL),
		(SELECT count(*) FROM learning_link WHERE unlinked_at IS NOT NULL),
		(SELECT count(*) FROM learning_link WHERE unlinked_at IS NULL AND external_id = 'Builder_42')`).Scan(&archivedRules, &unlinked, &live); err != nil ||
		archivedRules != 1 || unlinked != 1 || live != 1 {
		t.Errorf("rules/links: %d %d %d %v", archivedRules, unlinked, live, err)
	}

	// The old service secret authenticates, with exactly its old child.
	r, _ := http.NewRequest(http.MethodGet, "/api/children", nil)
	r.Header.Set("Authorization", "Bearer "+fixtureReadToken)
	a, ok, err := actor.Resolver{Q: q}.Resolve(ctx, r)
	if err != nil || !ok || a.Kind != actor.Service || a.AllChildren || len(a.Children) != 1 {
		t.Fatalf("old ocz_ secret: %+v ok=%v err=%v", a, ok, err)
	}

	// The calendar link is sealed (normalized to https); series keep their IDs, the removed one
	// its removal time; the series is for Avery, one occurrence for nobody, the cleared one gone.
	var sealed, name, color string
	var show bool
	if err := target.Owner.QueryRow(ctx, `SELECT feed_url, name, color, show_on_display FROM calendar_source WHERE id = '0190a000-0000-7000-8000-0000000000b1'`).Scan(&sealed, &name, &color, &show); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "example.test") || name != "School" || color != "sky" || show {
		t.Errorf("calendar: %q %q %q %v", sealed, name, color, show)
	}
	h := uuid.MustParse("0190a000-0000-7000-8000-00000000000a")
	if got, err := keys.Decrypt(keyring.AAD{HouseholdID: h.String(), Table: "calendar_source", RowID: "0190a000-0000-7000-8000-0000000000b1", Purpose: "feed_url"}, sealed); err != nil ||
		string(got) != "https://calendar.example.test/school.ics?token=fixture-feed-token" {
		t.Errorf("feed URL does not decrypt to the normalized link: %v", err)
	}
	var assignments string
	if err := target.Owner.QueryRow(ctx, `SELECT string_agg(a.recurrence_key || '=' || COALESCE(c.child_id::text, 'nobody'), ' ' ORDER BY a.recurrence_key)
		FROM calendar_assignment a LEFT JOIN calendar_assignment_child c ON c.assignment_id = a.id
		WHERE a.series_id = '0190a000-0000-7000-8000-0000000000b2'`).Scan(&assignments); err != nil ||
		assignments != "*=0190a000-0000-7000-8000-0000000000c1 20260114T163000Z=nobody" {
		t.Errorf("assignments: %q %v", assignments, err)
	}
	var removedSeen time.Time
	if err := target.Owner.QueryRow(ctx, `SELECT last_seen_at FROM calendar_series WHERE id = '0190a000-0000-7000-8000-0000000000b3'`).Scan(&removedSeen); err != nil ||
		!removedSeen.Equal(time.Date(2026, 1, 9, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("removed series: %v %v", removedSeen, err)
	}

	// The learning credential's old secret authenticates too, with its scope renamed.
	r, _ = http.NewRequest(http.MethodPost, "/api/learning/awards", nil)
	r.Header.Set("Authorization", "Bearer "+fixtureLearnToken)
	a, ok, err = actor.Resolver{Q: q}.Resolve(ctx, r)
	if err != nil || !ok || a.Kind != actor.Service || len(a.Children) != 1 || !slices.Equal(a.Scopes, []string{"learning:award"}) {
		t.Fatalf("old learning secret: %+v ok=%v err=%v", a, ok, err)
	}

	// The router's old secret authenticates with tv:gate. TVs keep their IDs, open hours, power
	// and MACs; sessions their starters (an adult, a child's device) and enders; openings their
	// adult and ended session.
	r, _ = http.NewRequest(http.MethodGet, "/api/tv/allowances", nil)
	r.Header.Set("Authorization", "Bearer "+fixtureGateToken)
	a, ok, err = actor.Resolver{Q: q}.Resolve(ctx, r)
	if err != nil || !ok || a.Kind != actor.Service || !slices.Equal(a.Scopes, []string{"tv:gate"}) {
		t.Fatalf("old router secret: %+v ok=%v err=%v", a, ok, err)
	}
	var tvs string
	if err := target.Owner.QueryRow(ctx, `SELECT string_agg(name || ' ' || power || ' ' || coalesce(open_start::text, '-') || '-' || coalesce(open_end::text, '-') || ' ' ||
		coalesce(open_days::text, '-') || ' ' || (archived_at IS NOT NULL) || ' ' || (SELECT count(*) FROM tv_mac m WHERE m.tv_id = tv.id), ', ' ORDER BY name) FROM tv`).Scan(&tvs); err != nil ||
		tvs != "Den TV unknown 1260-420 62 false 2, Old TV unknown --- - true 0" {
		t.Errorf("tvs: %q %v", tvs, err)
	}
	// Power and the attempt run are forgotten: nothing reports them here yet.
	var remembered int
	if err := target.Owner.QueryRow(ctx, `SELECT count(*) FROM tv WHERE power_changed_at IS NOT NULL OR attempts_since IS NOT NULL OR last_attempt_at IS NOT NULL`).Scan(&remembered); err != nil || remembered != 0 {
		t.Errorf("tv power remembered: %d %v", remembered, err)
	}
	var sessions string
	if err := target.Owner.QueryRow(ctx, `SELECT string_agg(s.watcher || ' ' || s.end_reason || ' ' || coalesce(a.email, '-') || ' ' || coalesce(s.started_by_device::text, '-') || ' ' ||
		coalesce(e.email, '-'), ', ' ORDER BY s.started_at)
		FROM tv_session s LEFT JOIN adult a ON a.id = s.started_by_adult LEFT JOIN adult e ON e.id = s.ended_by_adult`).Scan(&sessions); err != nil ||
		sessions != "child time_up - 0190a000-0000-7000-8000-0000000009e1 -, parents parent_override owner@example.test - owner@example.test" {
		t.Errorf("tv sessions: %q %v", sessions, err)
	}
	var openings string
	if err := target.Owner.QueryRow(ctx, `SELECT string_agg(o.action || ' ' || a.email || ' ' || coalesce(o.ended_session_id::text, '-'), ', ' ORDER BY o.at)
		FROM tv_open_event o JOIN adult a ON a.id = o.by_adult`).Scan(&openings); err != nil ||
		openings != "open owner@example.test 0190a000-0000-7000-8000-0000000009b2, close owner@example.test -" {
		t.Errorf("tv openings: %q %v", openings, err)
	}

	// bootstrap-owner and a second import both refuse now.
	if _, err := importOnce(t, oldDB(t, complete), target, true); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("second import: %v", err)
	}
	_ = time.Second
}

// testKeys is one fixed keyring per test binary, so a test can decrypt what an import sealed.
func testKeys(t *testing.T) *keyring.Keyring {
	t.Helper()
	kr, err := keyring.Parse("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestDuplicateCalendarLinks(t *testing.T) {
	// The same link spelled with https: carried once; the copy, its series and its assignment
	// are reported, and the import still commits.
	const dup = `SET search_path TO opencozy;
INSERT INTO calendar_source (household_id, id, provider, name, feed_url, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b8', 'ics', 'School again', 'https://calendar.example.test/school.ics?token=fixture-feed-token', '2026-01-08T08:00:00Z');
INSERT INTO calendar_series (household_id, id, source_id, provider_uid, summary, all_day, recurring, status, content_hash) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000b9', '0190a000-0000-7000-8000-0000000000b8', 'swim@example.test', 'Swimming lesson', false, true, 'confirmed', repeat('e', 64));
INSERT INTO calendar_assignment (household_id, id, series_id, occurrence_key, version, active) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-0000000000ba', '0190a000-0000-7000-8000-0000000000b9', NULL, 1, true);`
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete, dup), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	st := statuses(out)
	for _, table := range []string{"calendar_source", "calendar_series", "calendar_assignment"} {
		if st[table] != "ok" {
			t.Errorf("%s: %q", table, st[table])
		}
	}
	for _, note := range []string{"1 left out: the same link as another calendar", "1 left out with their duplicate links", "colours assigned in turn"} {
		if !strings.Contains(out, note) {
			t.Errorf("the report does not say %q", note)
		}
	}
	if strings.Contains(out, "fixture-feed-token") {
		t.Error("the report shows a link")
	}
	var names string
	if err := target.Owner.QueryRow(context.Background(), `SELECT string_agg(name, ',') FROM calendar_source WHERE provider = 'ics'`).Scan(&names); err != nil || names != "School" {
		t.Errorf("calendars: %q %v", names, err)
	}
}

func TestReportSaysWhetherOldSecretsOpen(t *testing.T) {
	source := oldDB(t)
	out, err := importOnce(t, source, testdb.Open(t), true)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, `no --old-auth-secret-file, so Google, iCloud and Home Assistant import as "reconnect needed"`) {
		t.Errorf("without the old secret:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), "old-secret")
	if err := os.WriteFile(path, []byte("fixture-old-auth-secret-0123456789abcdefghij\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := oldsecret.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Run(context.Background(), Options{SourceURL: source, Target: testdb.Open(t).Owner, DryRun: true, Out: &buf, Keys: testKeys(t), OldSecret: old}); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "opened with --old-auth-secret-file") || strings.Contains(buf.String(), "fixture-old-auth-secret") {
		t.Errorf("with the old secret:\n%s", buf.String())
	}
}

func TestCalendarLinksNeedKeys(t *testing.T) {
	target := testdb.Open(t)
	var out bytes.Buffer
	err := Run(context.Background(), Options{SourceURL: oldDB(t), Target: target.Owner, DryRun: true, Out: &out})
	if !errors.Is(err, ErrNoKeys) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out.String()+err.Error(), "example.test") {
		t.Fatal("the link was printed")
	}
	empty(t, target)
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

func TestGoogleAccountsAndCalendars(t *testing.T) {
	ctx := context.Background()
	h := uuid.MustParse("0190a000-0000-7000-8000-00000000000a")
	path := filepath.Join(t.TempDir(), "old-secret")
	if err := os.WriteFile(path, []byte("fixture-old-auth-secret-0123456789abcdefghij\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, withSecret := range []bool{true, false} {
		t.Run(fmt.Sprintf("old secret %v", withSecret), func(t *testing.T) {
			var old *oldsecret.Secret
			if withSecret {
				var err error
				if old, err = oldsecret.Load(path); err != nil {
					t.Fatal(err)
				}
			}
			target := testdb.Open(t)
			var out bytes.Buffer
			if err := Run(ctx, Options{SourceURL: oldDB(t, complete), Target: target.Owner, Out: &out, Keys: testKeys(t), OldSecret: old}); err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			if strings.Contains(out.String(), "fixture-google-refresh-token") || strings.Contains(out.String(), "example.test") {
				t.Errorf("the report shows a secret or an address:\n%s", out.String())
			}
			// The connected account opens with the old secret and is sealed under the keyring;
			// without it, or when the old app already needed it, it needs connecting again.
			type conn struct {
				email     string
				token     *string
				reconnect bool
			}
			got := map[string]conn{}
			rows, err := target.Owner.Query(ctx, `SELECT id::text, email, refresh_token, needs_reconnect FROM calendar_google_connection`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var id string
				var c conn
				if err := rows.Scan(&id, &c.email, &c.token, &c.reconnect); err != nil {
					t.Fatal(err)
				}
				got[id] = c
			}
			if rows.Err() != nil || len(got) != 2 {
				t.Fatalf("connections: %+v %v", got, rows.Err())
			}
			first := got["0190a000-0000-7000-8000-000000000101"]
			if first.email != "parent@example.test" || first.reconnect == withSecret || (first.token != nil) != withSecret {
				t.Errorf("connected account: %+v", first)
			}
			if withSecret {
				tok, err := calendar.OpenGoogleToken(testKeys(t), h, uuid.MustParse("0190a000-0000-7000-8000-000000000101"), *first.token)
				if err != nil || tok != "fixture-google-refresh-token" {
					t.Errorf("refresh token: %q %v", tok, err)
				}
			}
			if second := got["0190a000-0000-7000-8000-000000000102"]; !second.reconnect || second.token != nil {
				t.Errorf("account the old app needed connected again: %+v", second)
			}
			// Calendars of carried accounts keep their Google IDs, not their sync tokens; the
			// one whose account was gone is left out with its series and assignment.
			var calendars string
			if err := target.Owner.QueryRow(ctx, `SELECT string_agg(name || ':' || google_calendar_id || ':' || (google_sync_token IS NULL)::text || ':' || version, ' ' ORDER BY name)
				FROM calendar_source WHERE provider = 'google'`).Scan(&calendars); err != nil ||
				calendars != "Family:family@group.calendar.example.test:true:3 Work:other@example.test:true:1" {
				t.Errorf("Google calendars: %q %v", calendars, err)
			}
			var assigned string
			if err := target.Owner.QueryRow(ctx, `SELECT s.uid || '=' || c.child_id FROM calendar_assignment a
				JOIN calendar_series s ON s.id = a.series_id JOIN calendar_assignment_child c ON c.assignment_id = a.id
				WHERE s.source_id = '0190a000-0000-7000-8000-000000000111'`).Scan(&assigned); err != nil ||
				assigned != "fixtureevent0001=0190a000-0000-7000-8000-0000000000c2" {
				t.Errorf("Google assignment: %q %v", assigned, err)
			}
			for _, note := range []string{"1 Google calendars left out: their account was gone", "1 left out with their Google calendars"} {
				if !strings.Contains(out.String(), note) {
					t.Errorf("the report does not say %q:\n%s", note, out.String())
				}
			}
		})
	}
}

// Old screen time waits on redemptions and devices: a source with any says so and can't commit.
func TestScreenTimeWaitsOnRedemptionsAndDevices(t *testing.T) {
	event := `SET search_path TO opencozy;
		ALTER TABLE screen_time_event DROP CONSTRAINT screen_time_event_device_fk;
		INSERT INTO screen_time_event (household_id, id, child_id, device_id, kind, occurred_at, created_at) VALUES
		  ('0190a000-0000-7000-8000-00000000000a', gen_random_uuid(), '0190a000-0000-7000-8000-0000000000c1', gen_random_uuid(),
		   'one_more_minute', '2026-01-05T08:00:00Z', '2026-01-05T08:00:01Z');`
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete, event), target, false)
	if !errors.Is(err, ErrPending) {
		t.Fatalf("err = %v, want ErrPending\n%s", err, out)
	}
	if statuses(out)["screen_time_event"] != "pending" || !strings.Contains(out, "waits on the import of devices") {
		t.Errorf("screen_time_event:\n%s", out)
	}
	empty(t, target)
}
