package importer

import (
	"context"
	"fmt"
)

// The old app's iPad time. Every credit, window and answered request hangs off a redemption (a
// purchase, a parent's grant and a "yes" were all redemptions there), and shield events and
// requests off a device. Redemptions and devices wait on the importer backlog task (slices 5 and
// 7), so these tables wait with them. Allowed hours are carried with the children.
//
// The mapping, for when they land:
//
//	personal_credit        -> screen_time_credit (source redemption, or grant / time_request when
//	                          the redemption was a parent's grant or a "yes"; seconds, used_seconds)
//	screen_time_grant      -> screen_time_window (same id; credit by redemption_id)
//	screen_time_grant_cut  -> screen_time_cut (window_id = grant_id; cut_by mapped to the adult)
//	time_request           -> time_request (credit by redemption_id) and, while pending, its
//	                          time_request approval
//
// Then §13's check: each child's banked seconds (sum of seconds - used_seconds) match before
// and after.
var screenTimeTables = []struct{ table, waits string }{
	{"personal_credit", "redemptions"},
	{"screen_time_grant", "redemptions"},
	{"screen_time_grant_cut", "redemptions"},
	{"time_request", "devices and redemptions"},
}

// importScreenTime reports the old screen-time tables that have rows as waiting, and why.
func importScreenTime(ctx context.Context, x *run) error {
	for _, t := range screenTimeTables {
		n, err := count(ctx, x.src, t.table)
		if err != nil {
			return err
		}
		if n == 0 {
			continue // the coverage check reports it empty
		}
		x.r.line(t.table).SourceRows = n
		x.r.pending(t.table, fmt.Sprintf("waits on the import of %s (importer backlog)", t.waits))
	}
	return nil
}
