package importer

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// creditMax is the most one new screen_time_credit holds (a day).
const creditMax = 86400

// importScreenTime carries each child's banked iPad time (allowed hours come with the children).
//
// The old app kept a credit per purchase, parent grant and "yes" (personal_credit), spent into
// unlock windows (screen_time_grant) that a parent could cut short (screen_time_grant_cut, the
// unused rest going back to the credit). James (2026-11-21): banked iPad time comes across as
// one balance per child. So each child with time left gets one credit holding the sum of what
// their old credits still hold (seconds - used_seconds); a balance over a day is split into
// day-sized credits. It is a grant from no one in particular (source grant, granted_by NULL), so
// a parent can still take it back; it is not tied to a redemption, so cancelling an imported
// screen-time redemption answers "no record" instead of refunding.
//
// Not carried, counted: the windows and cuts (history; a window still open at import ends, its
// rest is not given back: James, open sessions are dropped) and time requests (one still waiting
// is dropped, as pending approvals are; an answered one's "yes" is already in the balance).
func importScreenTime(ctx context.Context, x *run) error {
	x.r.columnsRead("personal_credit", "household_id", "child_id", "seconds", "used_seconds",
		// Dropped: one balance per child replaces the per-redemption credits.
		"redemption_id", "target", "minutes", "created_at")
	if err := bankBalances(ctx, x, "personal_credit", "screen_time_credit", "iPad", func(h, c uuid.UUID, s int64) error {
		_, err := x.dst.Exec(ctx, `INSERT INTO screen_time_credit (household_id, child_id, source, minutes, seconds, created_at)
			VALUES ($1, $2, 'grant', $3, $4, $5)`, h, c, (s+59)/60, s, x.started)
		return err
	}); err != nil {
		return err
	}
	return notCarriedScreenTime(ctx, x)
}

// notCarriedScreenTime reports the windows, cuts and time requests, which are not carried.
func notCarriedScreenTime(ctx context.Context, x *run) error {
	x.r.columnsRead("screen_time_grant", "household_id", "redemption_id", "child_id", "target", "minutes", "starts_at", "ends_at", "id")
	x.r.columnsRead("screen_time_grant_cut", "household_id", "grant_id", "child_id", "cut_at", "refunded_seconds", "cut_by", "created_at")
	x.r.columnsRead("time_request", "household_id", "id", "child_id", "device_id", "state", "version", "created_at", "decided_at",
		"decided_by", "choice", "redemption_id", "granted_until")
	var windows, open, openSeconds int64
	if err := x.src.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE g.ends_at > $1 AND c.grant_id IS NULL),
		COALESCE(sum(extract(epoch FROM g.ends_at - $1)) FILTER (WHERE g.ends_at > $1 AND c.grant_id IS NULL), 0)::bigint
		FROM opencozy.screen_time_grant g
		LEFT JOIN opencozy.screen_time_grant_cut c ON c.household_id = g.household_id AND c.grant_id = g.id`, x.started).
		Scan(&windows, &open, &openSeconds); err != nil {
		return safe(err)
	}
	if windows > 0 {
		x.r.line("screen_time_grant").SourceRows = windows
		x.r.line("screen_time_grant").Status = NotCarried
		x.r.note("screen_time_grant", "unlock windows: the history of spent time")
		if open > 0 {
			x.r.note("screen_time_grant", fmt.Sprintf("%d still open at import end (%d seconds unused, not given back)", open, openSeconds))
		}
	}
	cuts, err := count(ctx, x.src, "screen_time_grant_cut")
	if err != nil {
		return err
	}
	if cuts > 0 {
		x.r.line("screen_time_grant_cut").SourceRows = cuts
		x.r.line("screen_time_grant_cut").Status = NotCarried
		x.r.note("screen_time_grant_cut", "windows ended early: their rest is already back in the balance")
	}
	var requests, waiting int64
	if err := x.src.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state = 'pending') FROM opencozy.time_request`).
		Scan(&requests, &waiting); err != nil {
		return safe(err)
	}
	if requests > 0 {
		x.r.line("time_request").SourceRows = requests
		x.r.line("time_request").Status = NotCarried
		x.r.note("time_request", fmt.Sprintf("%d answered (a yes is in the balance), %d still waiting (dropped: the child asks again)", requests-waiting, waiting))
	}
	return nil
}

// bankBalances writes, for each child with time left in the old credit table, that time as
// day-sized new credits (insert writes one, dated at import), and checks each child's banked
// seconds before and after.
func bankBalances(ctx context.Context, x *run, old, target, what string, insert func(household, child uuid.UUID, seconds int64) error) error {
	rows, err := x.src.Query(ctx, `SELECT household_id, child_id, sum(seconds - used_seconds)::bigint
		FROM `+pgx.Identifier{oldSchema, old}.Sanitize()+` GROUP BY household_id, child_id HAVING sum(seconds - used_seconds) > 0
		ORDER BY household_id, child_id`)
	if err != nil {
		return safe(err)
	}
	type balance struct {
		household, child uuid.UUID
		seconds          int64
	}
	var bs []balance
	for rows.Next() {
		var b balance
		if err := rows.Scan(&b.household, &b.child, &b.seconds); err != nil {
			rows.Close()
			return safe(err)
		}
		bs = append(bs, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var credits, oldTotal int64
	for _, b := range bs {
		oldTotal += b.seconds
		for left := b.seconds; left > 0; left -= creditMax {
			s := min(left, creditMax)
			if err := insert(b.household, b.child, s); err != nil {
				return safe(err)
			}
			credits++
		}
	}
	src, err := count(ctx, x.src, old)
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, target)
	if err != nil {
		return err
	}
	x.r.table(old, src, target, credits, got)
	if src > 0 {
		x.r.note(old, fmt.Sprintf("one balance per child: %d children with time left, %d credits", len(bs), credits))
	}

	// §13: each child's banked seconds, before and after.
	var newTotal, equal int64
	for _, b := range bs {
		var s int64
		if err := x.dst.QueryRow(ctx, `SELECT COALESCE(sum(seconds - used_seconds), 0)::bigint FROM `+pgx.Identifier{target}.Sanitize()+`
			WHERE household_id = $1 AND child_id = $2`, b.household, b.child).Scan(&s); err != nil {
			return safe(err)
		}
		newTotal += s
		if s == b.seconds {
			equal++
		}
	}
	x.r.check("children whose banked "+what+" seconds match", int64(len(bs)), equal)
	x.r.check("banked "+what+" seconds, all children", oldTotal, newTotal)
	return nil
}
