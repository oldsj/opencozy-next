package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/oldsj/opencozy-next/internal/testdb"
)

func TestBankedIPadTimeIsOneBalancePerChild(t *testing.T) {
	// Rowan has two all-day grants nobody spent (48 hours) and an open window: the balance is
	// split into day-sized credits, and the open window ends.
	const rowan = `SET search_path TO opencozy; SET session_replication_role = replica;
INSERT INTO personal_credit (household_id, redemption_id, child_id, minutes, seconds, used_seconds, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', gen_random_uuid(), '0190a000-0000-7000-8000-0000000000c2', 1440, 86400, 0, '2026-03-12T08:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', gen_random_uuid(), '0190a000-0000-7000-8000-0000000000c2', 1440, 86400, 0, '2026-03-12T08:00:00Z');
INSERT INTO screen_time_grant (household_id, id, redemption_id, child_id, target, minutes, starts_at, ends_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', gen_random_uuid(), gen_random_uuid(), '0190a000-0000-7000-8000-0000000000c2', 'personal', 240, now(), now() + interval '4 hours');`
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete, rowan), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	st := statuses(out)
	for table, want := range map[string]string{
		"personal_credit": "ok", "screen_time_grant": "not carried", "time_request": "not carried",
	} {
		if st[table] != want {
			t.Errorf("%s: %q, want %q\n%s", table, st[table], want, out)
		}
	}
	for _, s := range []string{
		"one balance per child: 2 children with time left, 3 credits",
		"1 still open at import end",
		"1 answered (a yes is in the balance), 1 still waiting",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("no %q in\n%s", s, out)
		}
	}
	for _, c := range []string{"children whose banked iPad seconds match", "banked iPad seconds, all children"} {
		if got := checkStatus(out, c); got != "ok" {
			t.Errorf("%s: %q", c, got)
		}
	}
	var credits string
	if err := target.Owner.QueryRow(context.Background(), `SELECT string_agg(right(child_id::text, 2) || ':' || source || ':' || seconds || ':' || minutes, ' '
		ORDER BY child_id, seconds) FROM screen_time_credit`).Scan(&credits); err != nil ||
		credits != "c1:grant:900:15 c2:grant:86400:1440 c2:grant:86400:1440" {
		t.Errorf("credits: %q %v", credits, err)
	}
}

func TestBankedTVTimeIsOneBalancePerChild(t *testing.T) {
	// Avery's TV reward left 1200 seconds; add another 25 hours for her (two day-sized credits
	// together with the rest) and nothing for Rowan, whose only credit is used up.
	const more = `SET search_path TO opencozy; SET session_replication_role = replica;
INSERT INTO tv_credit (household_id, redemption_id, child_id, target, minutes, seconds, used_seconds, created_at) VALUES
  ('0190a000-0000-7000-8000-00000000000a', gen_random_uuid(), '0190a000-0000-7000-8000-0000000000c1', 'tv', 1500, 90000, 0, '2026-03-12T08:00:00Z'),
  ('0190a000-0000-7000-8000-00000000000a', gen_random_uuid(), '0190a000-0000-7000-8000-0000000000c2', 'tv', 30, 1800, 1800, '2026-03-12T08:00:00Z');`
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete, more), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "one balance per child: 1 children with time left, 2 credits") {
		t.Errorf("no balance note:\n%s", out)
	}
	for _, c := range []string{"children whose banked TV seconds match", "banked TV seconds, all children"} {
		if got := checkStatus(out, c); got != "ok" {
			t.Errorf("%s: %q", c, got)
		}
	}
	var credits string
	if err := target.Owner.QueryRow(context.Background(), `SELECT string_agg(right(child_id::text, 2) || ':' || COALESCE(redemption_id::text, '-') || ':' ||
		granted_seconds || ':' || seconds || ':' || used_seconds, ' ' ORDER BY seconds) FROM tv_credit`).Scan(&credits); err != nil ||
		credits != "c1:-:4800:4800:0 c1:-:86400:86400:0" {
		t.Errorf("credits: %q %v", credits, err)
	}
}
