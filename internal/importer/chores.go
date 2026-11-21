package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/occurrence"
)

// everyDay is the new weekday mask for every day (bit 0 Sunday ... bit 6 Saturday).
const everyDay = 127

// importChores carries chores, routines, their occurrences and the approvals of them.
//
//   - Chores and routines keep their IDs, child, title, points, version, created and archive
//     times. Old weekdays (1 Monday ... 7 Sunday) become the new mask; daily, and every routine,
//     is every day. The new app has no one-off chores, so an old one-off arrives archived and the
//     report counts those that were never done.
//   - Routine steps become routine_step rows with their old IDs, in order.
//   - Every old occurrence becomes the new occurrence of the same definition and household day
//     (internal/occurrence), the ID the ledger import already points at. Its state follows from
//     its history: complete while it has an award not reversed, otherwise open, with the reason
//     if an adult last sent it back.
//   - Decided approvals keep their IDs, child, title, points, state, reason, times and who
//     decided. Requests still waiting for an adult are not carried (James, 2026-11-21): the
//     report counts them and their points, and the child marks the chore done again. Requests
//     the child took back (withdrawn) are not carried either: nobody decided them.
func importChores(ctx context.Context, x *run) error {
	if err := importChoreDefinitions(ctx, x); err != nil {
		return err
	}
	if err := importRoutineDefinitions(ctx, x); err != nil {
		return err
	}
	if err := importOccurrences(ctx, x); err != nil {
		return err
	}
	return importApprovals(ctx, x)
}

func importChoreDefinitions(ctx context.Context, x *run) error {
	x.r.columnsRead("chore_definition", "household_id", "id", "assigned_child_id", "title", "points", "schedule_kind",
		"scheduled_day", "weekdays", "version", "created_at", "archived_at")
	rows, err := x.src.Query(ctx, `
		SELECT d.household_id, d.id, d.assigned_child_id, d.title, d.points, d.schedule_kind, d.weekdays, d.version,
		       d.created_at, d.archived_at,
		       EXISTS (SELECT 1 FROM opencozy.chore_active_award a JOIN opencozy.chore_occurrence o
		               ON o.household_id = a.household_id AND o.id = a.occurrence_id
		               WHERE o.household_id = d.household_id AND o.chore_id = d.id)
		FROM opencozy.chore_definition d ORDER BY d.household_id, d.created_at, d.id`)
	if err != nil {
		return safe(err)
	}
	type chore struct {
		household, id, child uuid.UUID
		title, kind          string
		points, version      int32
		weekdays             []int32
		created              time.Time
		archived             *time.Time
		done                 bool
	}
	var chores []chore
	for rows.Next() {
		var c chore
		if err := rows.Scan(&c.household, &c.id, &c.child, &c.title, &c.points, &c.kind, &c.weekdays, &c.version,
			&c.created, &c.archived, &c.done); err != nil {
			return safe(err)
		}
		chores = append(chores, c)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var n, oneOffs, oneOffsUndone int64
	for _, c := range chores {
		days, err := weekdayMask(c.kind, c.weekdays)
		if err != nil {
			return err
		}
		archived := c.archived
		if c.kind == "once" && archived == nil {
			// Done or not, a one-off has no place in a weekly plan: it arrives archived.
			oneOffs++
			if !c.done {
				oneOffsUndone++
			}
			archived = &x.started
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO chore (id, household_id, child_id, title, points, days, version, created_at, archived_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			c.id, c.household, c.child, c.title, c.points, days, c.version, c.created, archived); err != nil {
			return safe(err)
		}
		n++
	}
	if err := reconcile(ctx, x, "chore_definition", n, "chore"); err != nil {
		return err
	}
	if oneOffs > 0 {
		x.r.note("chore_definition", fmt.Sprintf("%d live one-off chores arrive archived (no one-offs in the new app), %d of them never done: add those again", oneOffs, oneOffsUndone))
	}
	return nil
}

// weekdayMask turns an old schedule into the new weekday mask.
func weekdayMask(kind string, weekdays []int32) (int16, error) {
	switch kind {
	case "daily", "once":
		return everyDay, nil
	case "weekdays":
		var m int16
		for _, w := range weekdays {
			if w < 1 || w > 7 {
				return 0, errors.New("import: a chore has a weekday outside 1-7")
			}
			m |= 1 << (w % 7) // old 7 is Sunday, the new bit 0
		}
		if m == 0 {
			return 0, errors.New("import: a weekday chore has no weekdays")
		}
		return m, nil
	}
	return 0, errors.New("import: a chore has an unknown schedule")
}

func importRoutineDefinitions(ctx context.Context, x *run) error {
	x.r.columnsRead("routine_definition", "household_id", "id", "assigned_child_id", "title", "points", "steps", "version",
		"created_at", "archived_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, assigned_child_id, title, points, steps, version, created_at, archived_at
		FROM opencozy.routine_definition ORDER BY household_id, created_at, id`)
	if err != nil {
		return safe(err)
	}
	type routine struct {
		household, id, child uuid.UUID
		title                string
		points, version      int32
		steps                []byte
		created              time.Time
		archived             *time.Time
	}
	var routines []routine
	for rows.Next() {
		var r routine
		if err := rows.Scan(&r.household, &r.id, &r.child, &r.title, &r.points, &r.steps, &r.version, &r.created, &r.archived); err != nil {
			return safe(err)
		}
		routines = append(routines, r)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var n, steps int64
	for _, r := range routines {
		if _, err := x.dst.Exec(ctx, `INSERT INTO routine (id, household_id, child_id, title, points, days, version, created_at, archived_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			r.id, r.household, r.child, r.title, r.points, everyDay, r.version, r.created, r.archived); err != nil {
			return safe(err)
		}
		n++
		var list []struct {
			ID    uuid.UUID `json:"id"`
			Title string    `json:"title"`
		}
		if err := json.Unmarshal(r.steps, &list); err != nil {
			return errors.New("import: a routine's steps are not a list of steps")
		}
		for i, s := range list {
			if _, err := x.dst.Exec(ctx, `INSERT INTO routine_step (id, household_id, routine_id, title, position) VALUES ($1, $2, $3, $4, $5)`,
				s.ID, r.household, r.id, s.Title, i); err != nil {
				return safe(err)
			}
			steps++
		}
	}
	if err := reconcile(ctx, x, "routine_definition", n, "routine"); err != nil {
		return err
	}
	var oldSteps int64
	if err := x.src.QueryRow(ctx, `SELECT COALESCE(sum(jsonb_array_length(steps)), 0)::bigint FROM opencozy.routine_definition`).Scan(&oldSteps); err != nil {
		return safe(err)
	}
	gotSteps, err := countNew(ctx, x.dst, "routine_step")
	if err != nil {
		return err
	}
	x.r.check("routine steps", oldSteps, gotSteps)
	x.r.check("routine steps written", steps, gotSteps)
	return nil
}

func importOccurrences(ctx context.Context, x *run) error {
	x.r.columnsRead("chore_occurrence", "household_id", "id", "chore_id", "assigned_child_id", "local_day", "points_snapshot",
		"created_at",
		// Dropped: snapshots of the definition (the new occurrence keeps only its points), versions,
		// and archived_at, which the old app never set.
		"time_zone_snapshot", "title_snapshot", "schedule_kind_snapshot", "scheduled_day_snapshot", "weekdays_snapshot",
		"definition_version", "version", "archived_at")
	x.r.columnsRead("routine_occurrence", "household_id", "id", "routine_id", "assigned_child_id", "local_day", "points_snapshot",
		"checked_step_ids", "created_at",
		"time_zone_snapshot", "title_snapshot", "steps_snapshot", "definition_version", "version")

	// The occurrence's state comes from its history: its newest approval, and whether a chore
	// has an active award (chore_active_award) or a routine an award not reversed.
	const q = `
		WITH occ AS (
		    SELECT household_id, id, 'chore' AS kind, chore_id AS definition, assigned_child_id, local_day, points_snapshot,
		           ARRAY[]::uuid[] AS checked, created_at,
		           EXISTS (SELECT 1 FROM opencozy.chore_active_award a WHERE a.household_id = o.household_id AND a.occurrence_id = o.id) AS awarded
		    FROM opencozy.chore_occurrence o
		    UNION ALL
		    SELECT household_id, id, 'routine', routine_id, assigned_child_id, local_day, points_snapshot, checked_step_ids, created_at,
		           EXISTS (SELECT 1 FROM opencozy.point_ledger l WHERE l.household_id = o.household_id AND l.routine_occurrence_id = o.id
		                   AND l.routine_entry_type = 'award')
		           AND NOT EXISTS (SELECT 1 FROM opencozy.point_ledger l WHERE l.household_id = o.household_id AND l.routine_occurrence_id = o.id
		                   AND l.routine_entry_type = 'reversal')
		    FROM opencozy.routine_occurrence o
		)
		SELECT o.household_id, o.kind, o.definition, o.assigned_child_id, o.local_day, o.points_snapshot, o.checked, o.created_at,
		       o.awarded, a.state, a.reason
		FROM occ o
		LEFT JOIN LATERAL (
		    SELECT state, reason FROM opencozy.completion_approval a
		    WHERE a.household_id = o.household_id AND a.state <> 'withdrawn'
		      AND (a.chore_occurrence_id = o.id OR a.routine_occurrence_id = o.id)
		    ORDER BY a.requested_at DESC, a.id DESC LIMIT 1
		) a ON true
		ORDER BY o.local_day, o.created_at, o.id`
	rows, err := x.src.Query(ctx, q)
	if err != nil {
		return safe(err)
	}
	type occ struct {
		household, definition, child uuid.UUID
		kind                         string
		localDay, created            time.Time
		points                       int32
		checked                      []uuid.UUID
		awarded                      bool
		approval, reason             *string
	}
	var all []occ
	for rows.Next() {
		var o occ
		if err := rows.Scan(&o.household, &o.kind, &o.definition, &o.child, &o.localDay, &o.points, &o.checked, &o.created,
			&o.awarded, &o.approval, &o.reason); err != nil {
			return safe(err)
		}
		all = append(all, o)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var chores, routines int64
	for _, o := range all {
		d := day.Date{Year: o.localDay.Year(), Month: o.localDay.Month(), Day: o.localDay.Day()}
		state := "open"
		var rejected *string
		switch {
		case o.awarded:
			state = "complete"
		case o.approval != nil && *o.approval == "rejected":
			rejected = o.reason
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO occurrence (id, household_id, child_id, kind, definition_id, day, state, points,
			checked_steps, rejected_reason, updated_at) VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8, $9, $10, $11)`,
			occurrence.ID(o.definition, d), o.household, o.child, o.kind, o.definition, d.String(), state, o.points,
			o.checked, rejected, o.created); err != nil {
			return safe(err)
		}
		if o.kind == "chore" {
			chores++
		} else {
			routines++
		}
	}
	src, err := count(ctx, x.src, "chore_occurrence")
	if err != nil {
		return err
	}
	var got int64
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM occurrence WHERE kind = 'chore'`).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.table("chore_occurrence", src, "occurrence (chores)", chores, got)
	if src, err = count(ctx, x.src, "routine_occurrence"); err != nil {
		return err
	}
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM occurrence WHERE kind = 'routine'`).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.table("routine_occurrence", src, "occurrence (routines)", routines, got)

	// Every ledger row for an occurrence now has it, and every award still in effect is for a
	// complete one.
	var want int64
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM point_ledger WHERE source_kind = 'occurrence'`).Scan(&want); err != nil {
		return safe(err)
	}
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM point_ledger l JOIN occurrence o
		ON o.household_id = l.household_id AND o.id = l.source_id AND o.child_id = l.child_id
		WHERE l.source_kind = 'occurrence'`).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.check("ledger rows whose occurrence was carried", want, got)
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM active_award WHERE source_kind = 'occurrence'`).Scan(&want); err != nil {
		return safe(err)
	}
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM occurrence WHERE state = 'complete'`).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.check("complete occurrences = active awards", want, got)
	return nil
}

func importApprovals(ctx context.Context, x *run) error {
	x.r.columnsRead("completion_approval", "household_id", "id", "child_id", "chore_occurrence_id", "routine_occurrence_id",
		"title_snapshot", "points_snapshot", "state", "version", "requested_at", "decided_at", "decided_by", "reason",
		// Dropped: the day (the occurrence's), the old command, the device that asked (devices
		// pair again) and the ledger row (the ledger names its occurrence).
		"local_day", "request_command_id", "device_id", "ledger_id")
	rows, err := x.src.Query(ctx, `
		SELECT a.household_id, a.id, a.child_id, COALESCE(co.chore_id, ro.routine_id), COALESCE(co.local_day, ro.local_day),
		       co.id IS NOT NULL, a.title_snapshot, a.points_snapshot, a.state, a.requested_at, a.decided_at, a.decided_by, a.reason
		FROM opencozy.completion_approval a
		LEFT JOIN opencozy.chore_occurrence co ON co.household_id = a.household_id AND co.id = a.chore_occurrence_id
		LEFT JOIN opencozy.routine_occurrence ro ON ro.household_id = a.household_id AND ro.id = a.routine_occurrence_id
		WHERE a.state IN ('approved', 'rejected')
		ORDER BY a.requested_at, a.id`)
	if err != nil {
		return safe(err)
	}
	type approval struct {
		household, id, child, definition uuid.UUID
		localDay, requested              time.Time
		chore                            bool
		title, state                     string
		points                           int32
		decided                          *time.Time
		decidedBy, reason                *string
	}
	var all []approval
	for rows.Next() {
		var a approval
		if err := rows.Scan(&a.household, &a.id, &a.child, &a.definition, &a.localDay, &a.chore, &a.title, &a.points, &a.state,
			&a.requested, &a.decided, &a.decidedBy, &a.reason); err != nil {
			return safe(err)
		}
		all = append(all, a)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var n, unknownDecider int64
	for _, a := range all {
		kind := "routine"
		if a.chore {
			kind = "completion"
		}
		var by *uuid.UUID
		if a.decidedBy != nil {
			if ad, ok := x.adults[*a.decidedBy]; ok {
				by = &ad.id
			} else {
				unknownDecider++
			}
		}
		d := day.Date{Year: a.localDay.Year(), Month: a.localDay.Month(), Day: a.localDay.Day()}
		if _, err := x.dst.Exec(ctx, `INSERT INTO approval (id, household_id, child_id, kind, occurrence_id, title, points, state, reason,
			version, decided_at, decided_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			a.id, a.household, a.child, kind, occurrence.ID(a.definition, d), a.title, a.points, a.state, a.reason,
			2, a.decided, by, a.requested); err != nil {
			return safe(err)
		}
		n++
	}
	src, err := count(ctx, x.src, "completion_approval")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "approval")
	if err != nil {
		return err
	}
	x.r.table("completion_approval", src, "approval", n, got)
	var waiting, withdrawn, waitingPoints int64
	if err := x.src.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state = 'pending'), count(*) FILTER (WHERE state = 'withdrawn'),
		COALESCE(sum(points_snapshot) FILTER (WHERE state = 'pending'), 0)::bigint FROM opencozy.completion_approval`).
		Scan(&waiting, &withdrawn, &waitingPoints); err != nil {
		return safe(err)
	}
	x.r.check("completion_approval rows read = carried + waiting + withdrawn", src, n+waiting+withdrawn)
	if waiting > 0 {
		x.r.note("completion_approval", fmt.Sprintf("%d still waiting for an adult (%d points) not carried: the chore is open again, the child marks it done again", waiting, waitingPoints))
	}
	if withdrawn > 0 {
		x.r.note("completion_approval", fmt.Sprintf("%d withdrawn (taken back before anyone decided), not carried", withdrawn))
	}
	if unknownDecider > 0 {
		x.r.note("completion_approval", fmt.Sprintf("%d decided by someone who is not an adult here: decider left empty", unknownDecider))
	}
	return nil
}
