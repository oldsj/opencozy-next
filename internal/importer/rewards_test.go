package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/oldsj/opencozy-next/internal/testdb"
)

func TestRewardsAndRedemptionsCommit(t *testing.T) {
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, s := range []string{
		"1 still waiting for an answer not carried",
		"1 parent grants and \"yes\" answers are not redemptions here",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("no %q in\n%s", s, out)
		}
	}
	for _, c := range []string{"redemptions whose ledger_id is their spend", "ledger rows whose redemption exists"} {
		if st := checkStatus(out, c); st != "ok" {
			t.Errorf("%s: %q\n%s", c, st, out)
		}
	}
	ctx, db := context.Background(), target.Owner

	// Rewards: kind, minutes and target ("personal" is the iPad), archive, and a title and cost over the new input limits, as they are.
	var rewards string
	if err := db.QueryRow(ctx, `SELECT string_agg(right(id::text, 3) || ':' || kind || ':' || COALESCE(minutes::text, '-') || ':' ||
		COALESCE(screen_time_target, '-') || ':' || (archived_at IS NOT NULL) || ':' || length(title) || ':' || cost, ' ' ORDER BY id) FROM reward`).Scan(&rewards); err != nil ||
		rewards != "103:item:-:-:false:9:4 104:item:-:-:false:7:3 109:screen_time:30:ipad:false:9:2 10a:screen_time:20:tv:true:7:5 10b:item:-:-:false:96:250000 9f1:screen_time:30:tv:false:7:10" {
		t.Errorf("rewards: %q %v", rewards, err)
	}

	// Redemptions: decided ones only, each naming its spend; the waiting one and the grant are not
	// redemptions.
	var redemptions string
	if err := db.QueryRow(ctx, `SELECT string_agg(right(r.id::text, 3) || ':' || r.state || ':' || COALESCE(r.minutes::text, '-') || ':' ||
		(r.fulfilled_at IS NOT NULL) || ':' || (r.cancelled_at IS NOT NULL) || ':' || right(r.ledger_id::text, 3), ' ' ORDER BY r.id)
		FROM redemption r`).Scan(&redemptions); err != nil ||
		redemptions != "131:fulfilled:-:true:false:148 132:cancelled:-:false:true:149 136:cancelled:30:true:true:1a1 9f2:fulfilled:30:true:false:1d2" {
		t.Errorf("redemptions: %q %v", redemptions, err)
	}
}
