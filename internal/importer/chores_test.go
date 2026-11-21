package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/occurrence"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

func TestChoresRoutinesAndApprovalsCommit(t *testing.T) {
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ctx := context.Background()
	db := target.Owner
	for _, note := range []string{"1 live one-off chores arrive archived", "1 of them never done", "1 withdrawn", "1 still waiting for an adult (2 points) not carried"} {
		if !strings.Contains(out, note) {
			t.Errorf("the report does not say %q\n%s", note, out)
		}
	}

	// Chores keep IDs, versions and children; weekdays become the mask (Mon, Wed, Sun); the
	// one-off arrives archived.
	var chores string
	if err := db.QueryRow(ctx, `SELECT string_agg(right(id::text, 3) || ':' || days || ':' || version || ':' || (archived_at IS NOT NULL), ' ' ORDER BY id)
		FROM chore`).Scan(&chores); err != nil || chores != "101:127:1:false 107:11:3:false 108:127:1:true" {
		t.Errorf("chores: %q %v", chores, err)
	}
	var steps string
	if err := db.QueryRow(ctx, `SELECT string_agg(right(id::text, 3) || '=' || title || '@' || position, ' ' ORDER BY position)
		FROM routine_step WHERE routine_id = $1`, id("000000000102")).Scan(&steps); err != nil || steps != "105=Brush teeth@0 106=Get dressed@1" {
		t.Errorf("routine steps: %q %v", steps, err)
	}

	// Each occurrence is the one the app computes for its definition and day, in the state its
	// history gives it.
	occ := func(def string, d int) uuid.UUID {
		return occurrence.ID(id(def), day.Date{Year: 2026, Month: 3, Day: d})
	}
	for _, c := range []struct {
		id              uuid.UUID
		state, rejected string
		checked         int
	}{
		{occ("000000000101", 9), "complete", "", 0},          // approved
		{occ("000000000101", 10), "complete", "", 0},         // paid, undone, paid again
		{occ("000000000107", 11), "open", "", 0},             // was waiting for an adult: open again
		{occ("000000000102", 9), "complete", "", 2},          // routine paid
		{occ("000000000102", 10), "open", "Bed not made", 1}, // undone, then sent back
	} {
		var state, rejected string
		var checked int
		if err := db.QueryRow(ctx, `SELECT state, COALESCE(rejected_reason, ''), cardinality(checked_steps) FROM occurrence WHERE id = $1`, c.id).
			Scan(&state, &rejected, &checked); err != nil || state != c.state || rejected != c.rejected || checked != c.checked {
			t.Errorf("occurrence %s: %s %q %d (%v), want %s %q %d", c.id, state, rejected, checked, err, c.state, c.rejected, c.checked)
		}
	}

	// Decided approvals keep IDs, state and who decided; the waiting and withdrawn ones are gone.
	var approvals string
	if err := db.QueryRow(ctx, `SELECT string_agg(right(a.id::text, 3) || ':' || a.kind || ':' || a.state || ':' || a.version || ':' || COALESCE(ad.email, '-'), ' ' ORDER BY a.id)
		FROM approval a LEFT JOIN adult ad ON ad.id = a.decided_by`).Scan(&approvals); err != nil ||
		approvals != "181:completion:approved:2:owner@example.test 185:routine:rejected:2:sam@example.test" {
		t.Errorf("approvals: %q %v", approvals, err)
	}
}
