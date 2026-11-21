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
	for _, c := range []string{"children whose balance matches", "points held, all children", "active awards (awards not reversed)", "active chore awards rebuilt = old"} {
		if !strings.Contains(out.String(), c) {
			t.Errorf("no check %q", c)
		}
	}

	balance := func(child string) int64 {
		var b int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(delta), 0) FROM point_ledger WHERE child_id = $1`, id(child)).Scan(&b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if a, r := balance("0000000000c1"), balance("0000000000c2"); a != 11 || r != 0 {
		t.Errorf("balances: Avery %d, Rowan %d; want 11, 0", a, r)
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
	// Rowan spends 10 points they never had.
	const overspend = `SET search_path TO opencozy;
INSERT INTO reward_redemption (household_id, id, reward_id, child_id, name_snapshot, cost_snapshot, reward_version, version, state, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-000000000134', '0190a000-0000-7000-8000-000000000104', '0190a000-0000-7000-8000-0000000000c2', 'Sticker', 10, 1, 1, 'pending', '2026-03-11T13:00:00Z');
INSERT INTO point_ledger (household_id, id, child_id, redemption_id, reward_entry_type, command_id, delta, description_snapshot, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', '0190a000-0000-7000-8000-00000000014f', '0190a000-0000-7000-8000-0000000000c2', '0190a000-0000-7000-8000-000000000134', 'spend', '0190a000-0000-7000-8000-00000000016f', -10, 'Sticker', '2026-03-11T13:00:00Z');`
	_, _, err := importInTx(t, oldDB(t, overspend), testdb.Open(t))
	if err == nil || !strings.Contains(err.Error(), "below zero") {
		t.Fatalf("err = %v", err)
	}
}
