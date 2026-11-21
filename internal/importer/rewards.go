package importer

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// titleMax is the new app's limit on a reward's title (the old app allowed 160 characters).
const titleMax = 80

// importRewards carries rewards and the redemptions decided on them. It runs after the ledger:
// a redemption names its spend row.
//
//   - Rewards keep their IDs, cost, version and times. Screen-time rewards keep their minutes;
//     the old "personal" target is the iPad. A title longer than the new limit is shortened, and
//     the report counts them.
//   - Redemptions of a reward keep their IDs, child, title, cost, minutes, version and times:
//     a fulfilled one stays fulfilled, a cancelled one cancelled (the ledger already holds its
//     refund). ledger_id is the old spend row, which the ledger import carried with its ID.
//   - A redemption still waiting for an answer is not carried (James, 2026-11-21); nor is its
//     spend, so its points are back with the child (see importLedger).
//   - A parent's grant or a "yes" to more time was a redemption with no reward in the old app.
//     It is not a redemption here: its time is part of the child's banked screen time, which the
//     screen-time import carries.
func importRewards(ctx context.Context, x *run) error {
	if err := importRewardDefinitions(ctx, x); err != nil {
		return err
	}
	return importRedemptions(ctx, x)
}

// shorten cuts a title to the new limit, on a character boundary.
func shorten(s string) (string, bool) {
	if utf8.RuneCountInString(s) <= titleMax {
		return s, false
	}
	return string([]rune(s)[:titleMax]), true
}

// screenTarget maps the old screen-time target to the new one.
func screenTarget(old *string) (*string, error) {
	if old == nil {
		return nil, nil
	}
	var t string
	switch *old {
	case "personal":
		t = "ipad"
	case "tv":
		t = "tv"
	default:
		return nil, errors.New("import: a reward has an unknown screen-time target")
	}
	return &t, nil
}

func importRewardDefinitions(ctx context.Context, x *run) error {
	x.r.columnsRead("reward", "household_id", "id", "name", "cost", "version", "created_at", "archived_at",
		"screen_time_minutes", "screen_time_target")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, name, cost, version, created_at, archived_at,
		screen_time_minutes, screen_time_target FROM opencozy.reward ORDER BY household_id, created_at, id`)
	if err != nil {
		return safe(err)
	}
	type reward struct {
		household, id uuid.UUID
		name          string
		cost, version int32
		created       time.Time
		archived      *time.Time
		minutes       *int32
		target        *string
	}
	var rs []reward
	for rows.Next() {
		var r reward
		if err := rows.Scan(&r.household, &r.id, &r.name, &r.cost, &r.version, &r.created, &r.archived, &r.minutes, &r.target); err != nil {
			rows.Close()
			return safe(err)
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var shortened int64
	for _, r := range rs {
		title, cut := shorten(r.name)
		if cut {
			shortened++
		}
		kind := "item"
		if r.minutes != nil {
			kind = "screen_time"
		}
		target, err := screenTarget(r.target)
		if err != nil {
			return err
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO reward (id, household_id, title, cost, kind, minutes, screen_time_target, version, created_at, archived_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			r.id, r.household, title, r.cost, kind, r.minutes, target, r.version, r.created, r.archived); err != nil {
			return safe(err)
		}
	}
	if err := reconcile(ctx, x, "reward", int64(len(rs)), "reward"); err != nil {
		return err
	}
	if shortened > 0 {
		x.r.note("reward", fmt.Sprintf("%d titles longer than %d characters shortened", shortened, titleMax))
	}
	return nil
}

func importRedemptions(ctx context.Context, x *run) error {
	// reward_version: the new redemption keeps its own title, cost and minutes instead.
	// refunded_points: the refund is a ledger row. device_id: which iPad the time went to is
	// not kept (the new app banks time per child).
	x.r.columnsRead("reward_redemption", "household_id", "id", "reward_id", "child_id", "name_snapshot", "cost_snapshot",
		"version", "state", "created_at", "settled_at", "fulfilled_at", "screen_time_minutes_snapshot",
		"screen_time_target_snapshot", "reward_version", "refunded_points", "device_id")
	rows, err := x.src.Query(ctx, `
		SELECT r.household_id, r.id, r.reward_id, r.child_id, r.name_snapshot, r.cost_snapshot, r.version, r.state,
		       r.created_at, r.settled_at, r.fulfilled_at, r.screen_time_minutes_snapshot, r.screen_time_target_snapshot,
		       l.id
		FROM opencozy.reward_redemption r
		LEFT JOIN opencozy.point_ledger l ON l.household_id = r.household_id AND l.redemption_id = r.id AND l.reward_entry_type = 'spend'
		WHERE r.reward_id IS NOT NULL AND r.state <> 'pending'
		ORDER BY r.household_id, r.created_at, r.id`)
	if err != nil {
		return safe(err)
	}
	type redemption struct {
		household, id, reward, child uuid.UUID
		name, state                  string
		cost, version                int32
		created                      time.Time
		settled, fulfilled           *time.Time
		minutes                      *int32
		target                       *string
		spend                        *uuid.UUID
	}
	var rs []redemption
	for rows.Next() {
		var r redemption
		if err := rows.Scan(&r.household, &r.id, &r.reward, &r.child, &r.name, &r.cost, &r.version, &r.state,
			&r.created, &r.settled, &r.fulfilled, &r.minutes, &r.target, &r.spend); err != nil {
			rows.Close()
			return safe(err)
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var shortened int64
	for _, r := range rs {
		if r.spend == nil {
			return errors.New("import: a redemption of a reward has no spend in the ledger")
		}
		title, cut := shorten(r.name)
		if cut {
			shortened++
		}
		target, err := screenTarget(r.target)
		if err != nil {
			return err
		}
		var cancelled *time.Time
		state := "fulfilled"
		if r.state == "canceled" {
			state, cancelled = "cancelled", r.settled
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO redemption (id, household_id, child_id, reward_id, title, cost, minutes,
				screen_time_target, state, ledger_id, version, fulfilled_at, cancelled_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			r.id, r.household, r.child, r.reward, title, r.cost, r.minutes, target, state, r.spend, r.version,
			r.fulfilled, cancelled, r.created); err != nil {
			return safe(err)
		}
	}
	x.carried[sourceRedemption] = true

	src, err := count(ctx, x.src, "reward_redemption")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "redemption")
	if err != nil {
		return err
	}
	x.r.table("reward_redemption", src, "redemption", int64(len(rs)), got)
	var waiting, grants int64
	if err := x.src.QueryRow(ctx, `SELECT count(*) FILTER (WHERE reward_id IS NOT NULL AND state = 'pending'),
		count(*) FILTER (WHERE reward_id IS NULL) FROM opencozy.reward_redemption`).Scan(&waiting, &grants); err != nil {
		return safe(err)
	}
	x.r.check("reward_redemption rows read = carried + waiting + grants", src, int64(len(rs))+waiting+grants)
	if waiting > 0 {
		x.r.note("reward_redemption", fmt.Sprintf("%d still waiting for an answer not carried: their points go back to the children", waiting))
	}
	if grants > 0 {
		x.r.note("reward_redemption", fmt.Sprintf("%d parent grants and \"yes\" answers are not redemptions here: their time is in the banked screen time", grants))
	}
	if shortened > 0 {
		x.r.note("reward_redemption", fmt.Sprintf("%d titles longer than %d characters shortened", shortened, titleMax))
	}

	// The other direction of the ledger check: each redemption names its own spend, for its child.
	var named int64
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM redemption r JOIN point_ledger l
		ON l.household_id = r.household_id AND l.id = r.ledger_id AND l.child_id = r.child_id
		AND l.kind = 'redemption' AND l.source_kind = 'redemption' AND l.source_id = r.id`).Scan(&named); err != nil {
		return safe(err)
	}
	x.r.check("redemptions whose ledger_id is their spend", got, named)
	return nil
}
