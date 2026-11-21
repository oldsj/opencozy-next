package importer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/occurrence"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

// importInTx imports into an open target transaction, rolled back when t ends, so a test can read
// what an import that may not commit yet (pending tables) wrote.
func importInTx(t *testing.T, source string, target testdb.DB) (pgx.Tx, *Report, error) {
	t.Helper()
	ctx := context.Background()
	src, err := openSource(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.close(context.Background()) })
	if err := checkVersion(ctx, src.tx); err != nil {
		t.Fatal(err)
	}
	tx, err := target.Owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	r, err := importAll(ctx, Options{Keys: testKeys(t)}, src.tx, tx)
	return tx, r, err
}

func id(n string) uuid.UUID { return uuid.MustParse("0190a000-0000-7000-8000-" + n) }

func TestLedgerImport(t *testing.T) {
	tx, r, err := importInTx(t, oldDB(t), testdb.Open(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var out strings.Builder
	r.Print(&out)
	if r.Failed() {
		t.Fatalf("mismatch:\n%s", out.String())
	}
	st := statuses(out.String())
	for _, table := range []string{"point_ledger", "chore_award_reversal", "chore_active_award", "external_award"} {
		if st[table] != "ok" {
			t.Errorf("%s: %q\n%s", table, st[table], out.String())
		}
	}
	for _, c := range []string{"children whose balance matches", "points held, all children", "1 spends on rewards still waiting for an answer not carried: 3 points go back", "active awards (awards not reversed)", "active chore awards rebuilt = old"} {
		if !strings.Contains(out.String(), c) {
			t.Errorf("no check %q", c)
		}
	}

	// Each ledger row's source exists: learning awards and occurrences now; redemptions once their
	// importer runs (until then the check is pending, so a real import can't commit).
	for check, want := range map[string]string{
		"ledger rows whose learning_award exists": "ok",
		"ledger rows whose occurrence exists":     "ok",
		"ledger rows whose redemption exists":     "pending",
	} {
		if got := checkStatus(out.String(), check); got != want {
			t.Errorf("%s: %q, want %q", check, got, want)
		}
	}
	if !r.Pending() {
		t.Error("not pending while ledger sources are missing")
	}

	balance := func(child string) int64 {
		var b int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(delta), 0) FROM point_ledger WHERE child_id = $1`, id(child)).Scan(&b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if a, r := balance("0000000000c1"), balance("0000000000c2"); a != 11 || r != 3 {
		t.Errorf("balances: Avery %d, Rowan %d; want 11, 3 (the waiting Sticker's 3 points come back)", a, r)
	}

	type row struct {
		kind, sourceKind string
		source           uuid.UUID
		reverses         *uuid.UUID
		adult            *string
		created          time.Time
	}
	get := func(n string) row {
		var x row
		if err := tx.QueryRow(ctx, `SELECT l.kind, l.source_kind, l.source_id, l.reverses_id, a.email, l.created_at
			FROM point_ledger l LEFT JOIN adult a ON a.id = l.adult_id WHERE l.id = $1`, id(n)).
			Scan(&x.kind, &x.sourceKind, &x.source, &x.reverses, &x.adult, &x.created); err != nil {
			t.Fatalf("ledger row %s: %v", n, err)
		}
		return x
	}
	chore, routine := id("000000000101"), id("000000000102")
	// The approved chore: the occurrence the new app computes for that chore and day, paid by the
	// adult who approved it, at the old time.
	if x := get("000000000141"); x.kind != "award" || x.sourceKind != "occurrence" ||
		x.source != occurrence.ID(chore, day.Date{Year: 2026, Month: 3, Day: 9}) ||
		x.adult == nil || *x.adult != "owner@example.test" || !x.created.Equal(time.Date(2026, 3, 9, 18, 0, 0, 0, time.UTC)) {
		t.Errorf("approved chore award: %+v", x)
	}
	if x := get("000000000143"); x.kind != "reversal" || x.reverses == nil || *x.reverses != id("000000000142") ||
		x.source != occurrence.ID(chore, day.Date{Year: 2026, Month: 3, Day: 10}) {
		t.Errorf("chore reversal: %+v", x)
	}
	if x := get("000000000147"); x.kind != "reversal" || x.reverses == nil || *x.reverses != id("000000000146") ||
		x.source != occurrence.ID(routine, day.Date{Year: 2026, Month: 3, Day: 10}) {
		t.Errorf("routine reversal: %+v", x)
	}
	if x := get("000000000149"); x.kind != "redemption" || x.sourceKind != "redemption" || x.source != id("000000000132") {
		t.Errorf("spend: %+v", x)
	}
	if x := get("00000000014a"); x.kind != "refund" || x.reverses == nil || *x.reverses != id("000000000149") {
		t.Errorf("refund: %+v", x)
	}
	if x := get("00000000014e"); x.kind != "reversal" || x.sourceKind != "learning" || x.source != id("000000000152") ||
		x.reverses == nil || *x.reverses != id("00000000014d") {
		t.Errorf("learning reversal: %+v", x)
	}

	// The spend on the reward still waiting isn't carried.
	var waiting int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM point_ledger WHERE id = $1`, id("00000000014b")).Scan(&waiting); err != nil || waiting != 0 {
		t.Errorf("waiting spend: %d %v", waiting, err)
	}

	// Active awards: the two paid chores, the routine not undone and the learning award not undone.
	var active string
	if err := tx.QueryRow(ctx, `SELECT string_agg(right(ledger_id::text, 3), ',' ORDER BY ledger_id) FROM active_award`).Scan(&active); err != nil ||
		active != "141,144,145,14c" {
		t.Errorf("active awards: %q %v", active, err)
	}

	// Learning awards keep their IDs and event IDs; the day is the household's (23:30 in
	// Amsterdam), the external ID the child's link.
	var (
		event                        uuid.UUID
		awardDay                     time.Time
		provider, external, activity string
		points                       int32
	)
	if err := tx.QueryRow(ctx, `SELECT source_event_id, day, provider, external_id, activity_key, points FROM learning_award WHERE id = $1`,
		id("000000000151")).Scan(&event, &awardDay, &provider, &external, &activity, &points); err != nil {
		t.Fatal(err)
	}
	if event != id("000000000171") || awardDay.Format(time.DateOnly) != "2026-03-12" || provider != "minecraft" ||
		external != "Builder_42" || activity != "build_house" || points != 5 {
		t.Errorf("learning award: %s %s %s %s %s %d", event, awardDay.Format(time.DateOnly), provider, external, activity, points)
	}
}

func TestLedgerRefusesAHistoryBelowZero(t *testing.T) {
	// Rowan spends 10 points they never had, on a reward they were given.
	const overspend = `SET search_path TO opencozy;
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, fulfilled_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000134', '0190a000-0000-7000-8000-000000000104', '0190a000-0000-7000-8000-0000000000c2', 'Sticker', 10, 1, 2, 'fulfilled', '2026-03-11T13:00:00Z', '2026-03-11T13:05:00Z', '2026-03-11T13:05:00Z');
INSERT INTO point_ledger (household_id, id, child_id, redemption_id, reward_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014f', '0190a000-0000-7000-8000-0000000000c2', '0190a000-0000-7000-8000-000000000134', 'spend', '0190a000-0000-7000-8000-00000000016f', -10, 'Sticker', '2026-03-11T13:00:00Z');`
	_, _, err := importInTx(t, oldDB(t, overspend), testdb.Open(t))
	if err == nil || !strings.Contains(err.Error(), "below zero") {
		t.Fatalf("err = %v", err)
	}
}

// Ledger histories the import must refuse, report, or order on its own.
func TestLedgerEdgeCases(t *testing.T) {
	const (
		hh    = "'0190a000-0000-7000-8000-00000000000a'"
		avery = "'0190a000-0000-7000-8000-0000000000c1'"
		// The old tables' history triggers refuse changes; this fixture turns them off.
		edit = "SET search_path TO opencozy; SET session_replication_role = replica;\n"
	)
	// An award and a spend for one child at the same instant: Avery has 11 points, earns 5 and
	// spends 16. Only gains-first ordering keeps the balance from going below zero (the spend's
	// ID sorts first).
	const sameInstant = edit + `
INSERT INTO chore_occurrence (household_id, id, chore_id, assigned_child_id, local_day, time_zone_snapshot, title_snapshot, points_snapshot, schedule_kind_snapshot, definition_version, version, created_at) VALUES
  (` + hh + `, '0190a000-0000-7000-8000-000000000114', '0190a000-0000-7000-8000-000000000101', ` + avery + `, '2026-03-13', 'Europe/Amsterdam', 'Feed the cat', 5, 'daily', 1, 2, '2026-03-13T06:00:00Z');
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at, settled_at, fulfilled_at) VALUES
  (` + hh + `, '0190a000-0000-7000-8000-000000000135', '0190a000-0000-7000-8000-000000000103', ` + avery + `, 'Ice cream', 16, 1, 2, 'fulfilled', '2026-03-13T18:00:00Z', '2026-03-13T18:00:00Z', '2026-03-13T18:00:00Z');
INSERT INTO point_ledger (household_id, id, child_id, occurrence_id, occurrence_version, redemption_id, reward_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  (` + hh + `, '0190a000-0000-7000-8000-00000000014f', ` + avery + `, NULL, NULL, '0190a000-0000-7000-8000-000000000135', 'spend', '0190a000-0000-7000-8000-0000000001f1', -16, 'Ice cream', '2026-03-13T18:00:00Z'),
  (` + hh + `, '0190a000-0000-7000-8000-0000000001f0', ` + avery + `, '0190a000-0000-7000-8000-000000000114', 2, NULL, NULL, '0190a000-0000-7000-8000-0000000001f2', 5, 'Feed the cat', '2026-03-13T18:00:00Z');
INSERT INTO chore_active_award (household_id, occurrence_id, ledger_id) VALUES
  (` + hh + `, '0190a000-0000-7000-8000-000000000114', '0190a000-0000-7000-8000-0000000001f0');`

	for _, c := range []struct {
		name, sql string
		err       string // the import refuses with this
		mismatch  string // or the report shows this check failing
		note      string // or the report says this
	}{
		{name: "award and spend at one instant", sql: sameInstant},
		{name: "undo older than its award", err: "older than the row it undoes",
			sql: edit + `UPDATE point_ledger SET created_at = '2026-03-10T16:00:00Z' WHERE id = '0190a000-0000-7000-8000-000000000143';`},
		{name: "chore reversal with no pairing row", err: "import:",
			sql: edit + `DELETE FROM chore_award_reversal;`},
		{name: "old active awards disagree", mismatch: "active chore awards rebuilt = old",
			sql: edit + `DELETE FROM chore_active_award WHERE ledger_id = '0190a000-0000-7000-8000-000000000141';`},
		{name: "approver no longer an adult here", note: "1 awards approved by someone who is not an adult here",
			sql: edit + `UPDATE completion_approval SET decided_by = 'goneUser' WHERE id = '0190a000-0000-7000-8000-000000000181';`},
		{name: "learning award with no learner link", note: "2 for a child with no learner link",
			sql: edit + `DELETE FROM external_identity_link;`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, r, err := importInTx(t, oldDB(t, c.sql), testdb.Open(t))
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			r.Print(&out)
			if c.mismatch != "" {
				if !r.Failed() || !strings.Contains(out.String(), c.mismatch) {
					t.Errorf("want mismatch on %q:\n%s", c.mismatch, out.String())
				}
				return
			}
			if r.Failed() {
				t.Errorf("mismatch:\n%s", out.String())
			}
			if c.note != "" && !strings.Contains(out.String(), c.note) {
				t.Errorf("no note %q:\n%s", c.note, out.String())
			}
		})
	}
}

// checkStatus finds a check's status in a printed report.
func checkStatus(out, check string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, check+" ") {
			for _, st := range []string{"MISMATCH", "pending", "ok"} {
				if strings.Contains(l, " "+st+" ") || strings.HasSuffix(strings.TrimSpace(l), st) {
					return st
				}
			}
		}
	}
	return ""
}
