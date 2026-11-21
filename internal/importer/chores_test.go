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

// The old app allowed titles up to 160 characters, up to 1,000,000 points and 32 routine
// steps; the import carries such rows as they are, without shortening anything.
func TestChoresOverTheNewInputLimitsAreCarriedAsTheyAre(t *testing.T) {
	long := strings.Repeat("Tidy the shelf, ", 9) + "then dust" // 153 characters
	steps := `[{"id": "0190a000-0000-7000-8000-000000000105", "title": "Brush teeth"}, {"id": "0190a000-0000-7000-8000-000000000106", "title": "Get dressed"}`
	for i := range 30 {
		steps += `, {"id": "` + uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(i)}).String() + `", "title": "` + long + `"}`
	}
	steps += "]"
	source := oldDB(t, complete, `SET search_path TO opencozy; SET session_replication_role = replica; -- the old history triggers refuse edits
		UPDATE chore_definition SET title = '`+long+`', points = 1000000 WHERE id = '0190a000-0000-7000-8000-000000000101';
		UPDATE routine_definition SET title = '`+long+`', points = 5000, steps = '`+steps+`' WHERE id = '0190a000-0000-7000-8000-000000000102';
		UPDATE completion_approval SET title_snapshot = '`+long+`', points_snapshot = 999999 WHERE id = '0190a000-0000-7000-8000-000000000181';`)
	target := testdb.Open(t)
	if out, err := importOnce(t, source, target, false); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ctx := context.Background()
	var got string
	if err := target.Owner.QueryRow(ctx, `SELECT (SELECT title || ':' || points FROM chore WHERE id = $1)
		|| ' ' || (SELECT title || ':' || points FROM routine WHERE id = $2)
		|| ' ' || (SELECT count(*) || ':' || max(position) || ':' || max(length(title)) FROM routine_step WHERE routine_id = $2)
		|| ' ' || (SELECT title || ':' || points FROM approval WHERE id = $3)`,
		id("000000000101"), id("000000000102"), id("000000000181")).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := long + ":1000000 " + long + ":5000 32:31:153 " + long + ":999999"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
