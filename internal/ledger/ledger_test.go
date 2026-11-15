package ledger_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

type fixture struct {
	db           testdb.DB
	house, child uuid.UUID
	other        uuid.UUID // a second child in the same household
}

func setup(t *testing.T) fixture {
	t.Helper()
	f := fixture{db: testdb.Open(t)}
	ctx := t.Context()
	if err := f.db.Owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Fictional', 'America/New_York') RETURNING id`).Scan(&f.house); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*uuid.UUID{&f.child, &f.other} {
		if err := f.db.Owner.QueryRow(ctx, `INSERT INTO child (household_id, name) VALUES ($1, 'Ash') RETURNING id`, f.house).Scan(p); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// run is one command, as a job runs it: household lock, then the children, then fn. It commits
// when fn succeeds.
func (f fixture) run(t *testing.T, children []uuid.UUID, fn func(context.Context, *command.Cmd) error) error {
	t.Helper()
	ctx := t.Context()
	tx, err := f.db.Runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	c, err := command.Lock(ctx, tx, f.house, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LockChildren(ctx, children...); err != nil {
		t.Fatal(err)
	}
	if err := fn(ctx, c); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (f fixture) balance(t *testing.T, child uuid.UUID) int64 {
	t.Helper()
	b, err := ledger.Balance(t.Context(), dbq.New(f.db.Runtime), f.house, child)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func entry(child, source uuid.UUID, points int32) ledger.Entry {
	return ledger.Entry{Child: child, Points: points, SourceKind: "chore_occurrence", SourceID: source}
}

func TestAwardOncePerSource(t *testing.T) {
	f := setup(t)
	chore := uuid.New()
	award := func(points int32) error {
		return f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
			_, err := ledger.Award(ctx, c, entry(f.child, chore, points))
			return err
		})
	}
	reverse := func() error {
		return f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
			_, err := ledger.Reverse(ctx, c, entry(f.child, chore, 0))
			return err
		})
	}
	if err := award(10); err != nil {
		t.Fatal(err)
	}
	if err := award(10); !errors.Is(err, ledger.ErrAlreadyAwarded) {
		t.Fatalf("second award: %v", err)
	}
	if err := reverse(); err != nil {
		t.Fatal(err)
	}
	if err := reverse(); !errors.Is(err, ledger.ErrNotAwarded) {
		t.Fatalf("second reversal: %v", err)
	}
	// Undone, then completed again: it can be paid again, once.
	if err := award(15); err != nil {
		t.Fatal(err)
	}
	if err := award(15); !errors.Is(err, ledger.ErrAlreadyAwarded) {
		t.Fatalf("award after re-award: %v", err)
	}
	if got := f.balance(t, f.child); got != 15 {
		t.Fatalf("balance %d, want 15", got)
	}
	var rows int
	if err := f.db.Owner.QueryRow(t.Context(), `SELECT count(*) FROM point_ledger WHERE source_id = $1`, chore).Scan(&rows); err != nil || rows != 3 {
		t.Fatalf("ledger rows %d, %v; want award, reversal, award", rows, err)
	}
}

func TestBalanceNeverBelowZero(t *testing.T) {
	f := setup(t)
	chore, reward := uuid.New(), uuid.New()
	on := func(fn func(context.Context, *command.Cmd) error) error {
		return f.run(t, []uuid.UUID{f.child}, fn)
	}
	if err := on(func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, chore, 10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := on(func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Redeem(ctx, c, entry(f.child, reward, 11))
		return err
	}); !errors.Is(err, ledger.ErrInsufficient) {
		t.Fatalf("overspend: %v", err)
	}
	var redemption uuid.UUID
	if err := on(func(ctx context.Context, c *command.Cmd) error {
		row, err := ledger.Redeem(ctx, c, entry(f.child, reward, 8))
		redemption = row.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The points are spent, so the award cannot be taken back.
	if err := on(func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Reverse(ctx, c, entry(f.child, chore, 0))
		return err
	}); !errors.Is(err, ledger.ErrInsufficient) {
		t.Fatalf("reverse spent points: %v", err)
	}
	refund := func(points int32) error {
		return on(func(ctx context.Context, c *command.Cmd) error {
			_, err := ledger.Refund(ctx, c, redemption, ledger.Entry{Points: points})
			return err
		})
	}
	// 8 spent: a partial refund of 3, then 6 is too much, then the remaining 5, then nothing.
	if err := refund(3); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.child); got != 5 {
		t.Fatalf("balance after partial refund %d, want 5", got)
	}
	if err := refund(6); !errors.Is(err, ledger.ErrOverRefund) {
		t.Fatalf("over-refund: %v", err)
	}
	if err := refund(5); err != nil {
		t.Fatal(err)
	}
	if err := refund(1); !errors.Is(err, ledger.ErrOverRefund) {
		t.Fatalf("refund past the full amount: %v", err)
	}
	if err := refund(0); !errors.Is(err, ledger.ErrAmount) {
		t.Fatalf("refund of 0: %v", err)
	}
	if got := f.balance(t, f.child); got != 10 {
		t.Fatalf("balance %d, want 10", got)
	}
	// The trigger holds the same line without Go: a direct over-refund is refused.
	if _, err := f.db.Runtime.Exec(t.Context(), `INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id, reverses_id)
		VALUES ($1, $2, 1, 'refund', 'chore_occurrence', $3, $4)`, f.house, f.child, reward, redemption); err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("direct over-refund: %v", err)
	}
}

func TestLedgerNeedsTheChildLock(t *testing.T) {
	f := setup(t)
	// Only the other child is locked.
	err := f.run(t, []uuid.UUID{f.other}, func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, uuid.New(), 5))
		return err
	})
	if !errors.Is(err, ledger.ErrNotLocked) {
		t.Fatalf("award without the child's lock: %v", err)
	}
	for _, p := range []int32{0, -3, ledger.MaxAmount + 1} {
		err := f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
			_, err := ledger.Award(ctx, c, entry(f.child, uuid.New(), p))
			return err
		})
		if !errors.Is(err, ledger.ErrAmount) {
			t.Errorf("award of %d: %v", p, err)
		}
	}
	// A reversal names the source's child; another child's reversal is refused.
	chore := uuid.New()
	if err := f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, chore, 5))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A reversal names the award's child. Another child's attempt is refused and, even if its
	// command commits anyway, writes nothing: the award is still there to reverse.
	tx, err := f.db.Runtime.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c, err := command.Lock(t.Context(), tx, f.house, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LockChildren(t.Context(), f.other); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Reverse(t.Context(), c, entry(f.other, chore, 0)); !errors.Is(err, ledger.ErrNotAwarded) {
		t.Fatalf("reverse through another child: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Reverse(ctx, c, entry(f.child, chore, 0))
		return err
	}); err != nil {
		t.Fatalf("the award is gone after a refused reversal: %v", err)
	}
}

// The ledger is append-only for everyone: the runtime role has no UPDATE, DELETE or TRUNCATE,
// cannot drop or replace the triggers, and the owner is stopped by the triggers.
func TestLedgerIsAppendOnly(t *testing.T) {
	f := setup(t)
	chore := uuid.New()
	if err := f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, chore, 10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	refuse := func(who string, pool *pgxpool.Pool, sql, want string) {
		t.Helper()
		_, err := pool.Exec(ctx, sql)
		if err == nil || !slices.ContainsFunc(strings.Split(want, "|"), func(w string) bool { return strings.Contains(err.Error(), w) }) {
			t.Errorf("%s ran %q: %v (want %q)", who, sql, err, want)
		}
	}
	for _, sql := range []string{
		`UPDATE point_ledger SET delta = 1000`,
		`DELETE FROM point_ledger`,
		`TRUNCATE point_ledger CASCADE`, // CASCADE: past active_award's key, to the trigger
	} {
		refuse("runtime", f.db.Runtime, sql, "permission denied")
		refuse("owner", f.db.Owner, sql, "append-only")
	}
	for _, sql := range []string{
		`DROP TRIGGER point_ledger_append_only ON point_ledger`,
		`ALTER TABLE point_ledger DISABLE TRIGGER ALL`,
		`CREATE OR REPLACE FUNCTION refuse_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
	} {
		refuse("runtime", f.db.Runtime, sql, "must be owner|permission denied for schema")
	}
	// A reversal must undo exactly its row.
	var award uuid.UUID
	if err := f.db.Owner.QueryRow(ctx, `SELECT id FROM point_ledger WHERE source_id = $1`, chore).Scan(&award); err != nil {
		t.Fatal(err)
	}
	for _, delta := range []int{-9, -11} {
		_, err := f.db.Runtime.Exec(ctx, `INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id, reverses_id)
			VALUES ($1, $2, $3, 'reversal', 'chore_occurrence', $4, $5)`, f.house, f.child, delta, chore, award)
		if err == nil || !strings.Contains(err.Error(), "does not undo") {
			t.Errorf("reversal of %d for a 10-point award: %v", delta, err)
		}
	}
	if _, err := f.db.Runtime.Exec(ctx, `INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id, reverses_id)
		VALUES ($1, $2, -10, 'reversal', 'chore_occurrence', $3, $4)`, f.house, f.other, chore, award); err == nil {
		t.Error("a reversal on another child was accepted")
	}
	var rows int
	if err := f.db.Owner.QueryRow(ctx, `SELECT count(*) FROM point_ledger`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("ledger rows %d, %v", rows, err)
	}
}

// The database refuses a row that takes a balance below zero, whoever writes it, and allows one
// that lands exactly on zero.
func TestDatabaseRefusesBelowZero(t *testing.T) {
	f := setup(t)
	chore := uuid.New()
	if err := f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, chore, 10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	redeem := `INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id)
		VALUES ($1, $2, $3, 'redemption', 'reward_redemption', $4)`
	for who, pool := range map[string]*pgxpool.Pool{"runtime": f.db.Runtime, "owner": f.db.Owner} {
		if _, err := pool.Exec(ctx, redeem, f.house, f.child, -11, uuid.New()); err == nil || !strings.Contains(err.Error(), "below zero") {
			t.Errorf("%s: 11 from 10: %v", who, err)
		}
		if _, err := pool.Exec(ctx, redeem, f.house, f.other, -1, uuid.New()); err == nil || !strings.Contains(err.Error(), "below zero") {
			t.Errorf("%s: 1 from the other child's 0: %v", who, err)
		}
	}
	if _, err := f.db.Runtime.Exec(ctx, redeem, f.house, f.child, -10, uuid.New()); err != nil {
		t.Fatalf("10 from 10: %v", err)
	}
	if b := f.balance(t, f.child); b != 0 {
		t.Fatalf("balance %d", b)
	}
	// The award can't be reversed now: that would be -10.
	var award uuid.UUID
	if err := f.db.Owner.QueryRow(ctx, `SELECT id FROM point_ledger WHERE kind = 'award'`).Scan(&award); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Runtime.Exec(ctx, `INSERT INTO point_ledger (household_id, child_id, delta, kind, source_kind, source_id, reverses_id)
		VALUES ($1, $2, -10, 'reversal', 'chore_occurrence', $3, $4)`, f.house, f.child, chore, award); err == nil || !strings.Contains(err.Error(), "below zero") {
		t.Errorf("reversal past zero: %v", err)
	}
}

// Racing commands serialize on the child's lock: of eight 5-point redemptions against 10 points,
// exactly two succeed, and of eight awards for one source, exactly one does.
func TestConcurrentChangesSerialize(t *testing.T) {
	f := setup(t)
	if err := f.run(t, []uuid.UUID{f.child}, func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, uuid.New(), 10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	race := func(fn func(context.Context, *command.Cmd) error) (ok, refused int) {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() { errs <- f.run(t, []uuid.UUID{f.child}, fn) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ledger.ErrInsufficient), errors.Is(err, ledger.ErrAlreadyAwarded):
				refused++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}
		return ok, refused
	}
	if ok, refused := race(func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Redeem(ctx, c, entry(f.child, uuid.New(), 5))
		return err
	}); ok != 2 || refused != 6 {
		t.Fatalf("redemptions: %d ok, %d refused", ok, refused)
	}
	if got := f.balance(t, f.child); got != 0 {
		t.Fatalf("balance %d, want 0", got)
	}
	chore := uuid.New()
	if ok, refused := race(func(ctx context.Context, c *command.Cmd) error {
		_, err := ledger.Award(ctx, c, entry(f.child, chore, 3))
		return err
	}); ok != 1 || refused != 7 {
		t.Fatalf("awards: %d ok, %d refused", ok, refused)
	}
}
