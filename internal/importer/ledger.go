package importer

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/occurrence"
)

// The new ledger's kinds and source kinds (internal/ledger, internal/server).
const (
	kindAward      = "award"
	kindReversal   = "reversal"
	kindRedemption = "redemption"
	kindRefund     = "refund"

	sourceOccurrence = "occurrence"
	sourceRedemption = "redemption"
	sourceLearning   = "learning"
)

// oldEntry is one old point_ledger row, with what it is for.
type oldEntry struct {
	household, id, child uuid.UUID
	delta                int32
	created              time.Time
	// Exactly one origin, as the old ledger_domain_origin check requires.
	choreDef, routineDef *uuid.UUID
	localDay             *time.Time
	redemption           *uuid.UUID
	rewardType           *string
	routineType          *string
	external             *uuid.UUID
	externalType         *string
	// The row this one undoes: from chore_award_reversal for chores, otherwise the award or spend
	// row of the same routine occurrence, external award or redemption.
	undoes *uuid.UUID
	// The adult who approved the completion this award paid for.
	approver *string
	// A spend on a reward still waiting for an adult's answer (James: not carried).
	waiting bool
}

// newEntry is the row it becomes.
type newEntry struct {
	old                    oldEntry
	kind, sourceKind       string
	sourceID               uuid.UUID
	reverses               *uuid.UUID
	adult                  *uuid.UUID
	fromChore, fromLearner bool
}

// importLedger carries the points ledger (design §5, §13): every old row keeps its ID, child,
// amount and time. Chore and routine rows point at the new occurrence of the same definition and
// household day (internal/occurrence); redemption rows at the redemption, which keeps its ID;
// learning rows at the learning award, which keeps the old external award's ID. Reversals and
// refunds name the row they undo, so the new same-source trigger checks each one. The active
// awards are rebuilt from awards minus reversals and compared with the old chore_active_award.
// The rows are written oldest first under the new never-below-zero trigger, so the import fails
// if the old history ever went below zero. A spend on a reward still waiting for an adult's answer
// is not carried, as the waiting reward isn't: those points go back to the child.
func importLedger(ctx context.Context, x *run) error {
	x.r.columnsRead("point_ledger", "household_id", "id", "child_id", "occurrence_id", "redemption_id", "reward_entry_type",
		"routine_occurrence_id", "routine_entry_type", "delta", "created_at", "external_award_id", "external_entry_type",
		// Dropped: the occurrence's version at the time, the old command ID (the command log is not
		// carried), and the description, which the new history reads from the chore, routine,
		// redemption or learning award.
		"occurrence_version", "command_id", "description_snapshot")
	x.r.columnsRead("chore_award_reversal", "household_id", "award_id", "reversal_id")
	x.r.columnsRead("chore_active_award", "household_id", "occurrence_id", "ledger_id")

	rows, err := x.src.Query(ctx, `
		SELECT l.household_id, l.id, l.child_id, l.delta, l.created_at,
		       co.chore_id, ro.routine_id, COALESCE(co.local_day, ro.local_day),
		       l.redemption_id, l.reward_entry_type, l.routine_entry_type, l.external_award_id, l.external_entry_type,
		       COALESCE(car.award_id, ra.id, rs.id, ea.id),
		       ca.decided_by, COALESCE(rr.state = 'pending', false)
		FROM opencozy.point_ledger l
		LEFT JOIN opencozy.chore_occurrence co ON co.household_id = l.household_id AND co.id = l.occurrence_id
		LEFT JOIN opencozy.routine_occurrence ro ON ro.household_id = l.household_id AND ro.id = l.routine_occurrence_id
		LEFT JOIN opencozy.chore_award_reversal car ON car.household_id = l.household_id AND car.reversal_id = l.id
		LEFT JOIN opencozy.point_ledger ra ON l.routine_entry_type = 'reversal' AND ra.household_id = l.household_id
		     AND ra.routine_occurrence_id = l.routine_occurrence_id AND ra.routine_entry_type = 'award'
		LEFT JOIN opencozy.point_ledger rs ON l.reward_entry_type = 'refund' AND rs.household_id = l.household_id
		     AND rs.redemption_id = l.redemption_id AND rs.reward_entry_type = 'spend'
		LEFT JOIN opencozy.point_ledger ea ON l.external_entry_type = 'reversal' AND ea.household_id = l.household_id
		     AND ea.external_award_id = l.external_award_id AND ea.external_entry_type = 'award'
		LEFT JOIN opencozy.completion_approval ca ON ca.household_id = l.household_id AND ca.ledger_id = l.id
		     AND ca.state = 'approved'
		LEFT JOIN opencozy.reward_redemption rr ON rr.household_id = l.household_id AND rr.id = l.redemption_id`)
	if err != nil {
		return safe(err)
	}
	var olds []oldEntry
	for rows.Next() {
		var e oldEntry
		if err := rows.Scan(&e.household, &e.id, &e.child, &e.delta, &e.created, &e.choreDef, &e.routineDef, &e.localDay,
			&e.redemption, &e.rewardType, &e.routineType, &e.external, &e.externalType, &e.undoes, &e.approver, &e.waiting); err != nil {
			return safe(err)
		}
		olds = append(olds, e)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}

	entries := make([]newEntry, 0, len(olds))
	var waiting, returned int64
	for _, o := range olds {
		if o.waiting {
			// A reward still waiting for an answer isn't carried (James, 2026-11-21): its spend
			// isn't either, so the points go back to the child, who can ask again.
			waiting++
			returned -= int64(o.delta)
			continue
		}
		e, err := x.mapEntry(o)
		if err != nil {
			return err
		}
		entries = append(entries, e)
	}
	// Oldest first. At one instant, originals before what undoes them, and gains before spends,
	// so the order never makes a balance dip that the old app did not have.
	slices.SortStableFunc(entries, func(a, b newEntry) int {
		return cmp.Or(a.old.created.Compare(b.old.created),
			cmp.Compare(boolInt(a.reverses != nil), boolInt(b.reverses != nil)),
			cmp.Compare(boolInt(a.old.delta < 0), boolInt(b.old.delta < 0)),
			slices.Compare(a.old.id[:], b.old.id[:]))
	})

	written := map[uuid.UUID]bool{}
	reversed := map[uuid.UUID]bool{}
	var choreReversals []uuid.UUID
	for _, e := range entries {
		if e.reverses != nil {
			if !written[*e.reverses] {
				return errors.New("import: a point_ledger reversal or refund is older than the row it undoes")
			}
			if e.kind == kindReversal {
				reversed[*e.reverses] = true
			}
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO point_ledger (id, household_id, child_id, delta, kind, source_kind, source_id, reverses_id, adult_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			e.old.id, e.old.household, e.old.child, e.old.delta, e.kind, e.sourceKind, e.sourceID, e.reverses, e.adult, e.old.created); err != nil {
			return safe(err)
		}
		written[e.old.id] = true
		if e.fromChore && e.kind == kindReversal {
			choreReversals = append(choreReversals, e.old.id)
		}
	}
	src, err := count(ctx, x.src, "point_ledger")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "point_ledger")
	if err != nil {
		return err
	}
	x.r.table("point_ledger", src, "point_ledger", int64(len(entries)), got)
	x.r.check("point_ledger rows read = carried + spends on waiting rewards", src, int64(len(entries))+waiting)
	if waiting > 0 {
		x.r.note("point_ledger", fmt.Sprintf("%d spends on rewards still waiting for an answer not carried: %d points go back to the children", waiting, returned))
	}

	// Active awards: every award not reversed.
	var active, choreActive int64
	var choreActiveIDs []uuid.UUID
	for _, e := range entries {
		if e.kind != kindAward || reversed[e.old.id] {
			continue
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO active_award (source_kind, source_id, household_id, ledger_id) VALUES ($1, $2, $3, $4)`,
			e.sourceKind, e.sourceID, e.old.household, e.old.id); err != nil {
			return safe(err)
		}
		active++
		if e.fromChore {
			choreActive++
			choreActiveIDs = append(choreActiveIDs, e.old.id)
		}
	}

	// chore_award_reversal became the chore reversals' reverses_id.
	src, err = count(ctx, x.src, "chore_award_reversal")
	if err != nil {
		return err
	}
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM point_ledger WHERE id = ANY($1) AND kind = 'reversal' AND reverses_id IS NOT NULL`,
		choreReversals).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.table("chore_award_reversal", src, "point_ledger (reversals)", src, got)

	// chore_active_award is rebuilt; it must name exactly the old active chore awards.
	src, err = count(ctx, x.src, "chore_active_award")
	if err != nil {
		return err
	}
	rows, err = x.src.Query(ctx, `SELECT ledger_id FROM opencozy.chore_active_award`)
	if err != nil {
		return safe(err)
	}
	oldActive, err := collectIDs(rows)
	if err != nil {
		return err
	}
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM active_award WHERE ledger_id = ANY($1) AND ledger_id = ANY($2)`,
		oldActive, choreActiveIDs).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.table("chore_active_award", src, "active_award (chores)", src, got)
	x.r.check("active chore awards rebuilt = old", src, choreActive)
	newActive, err := countNew(ctx, x.dst, "active_award")
	if err != nil {
		return err
	}
	x.r.check("active awards (awards not reversed)", active, newActive)

	if err := importExternalAwards(ctx, x, entries); err != nil {
		return err
	}
	return checkBalances(ctx, x)
}

// mapEntry decides an old row's kind and source.
func (x *run) mapEntry(o oldEntry) (newEntry, error) {
	e := newEntry{old: o}
	switch {
	case o.choreDef != nil || o.routineDef != nil:
		if o.localDay == nil {
			return e, errors.New("import: a point_ledger row names an occurrence that is missing")
		}
		def := o.choreDef
		e.fromChore = def != nil
		if def == nil {
			def = o.routineDef
		}
		d := day.Date{Year: o.localDay.Year(), Month: o.localDay.Month(), Day: o.localDay.Day()}
		e.sourceKind, e.sourceID = sourceOccurrence, occurrence.ID(*def, d)
		e.kind = kindAward
		if o.delta < 0 {
			e.kind = kindReversal
		}
		if o.approver != nil {
			if a, ok := x.adults[*o.approver]; ok {
				e.adult = &a.id
			}
		}
	case o.redemption != nil:
		e.sourceKind, e.sourceID = sourceRedemption, *o.redemption
		e.kind = kindRedemption
		if o.rewardType != nil && *o.rewardType == "refund" {
			e.kind = kindRefund
		}
	case o.external != nil:
		e.sourceKind, e.sourceID = sourceLearning, *o.external
		e.fromLearner = true
		e.kind = kindAward
		if o.externalType != nil && *o.externalType == "reversal" {
			e.kind = kindReversal
		}
	default:
		return e, errors.New("import: a point_ledger row has no origin")
	}
	if e.kind == kindReversal || e.kind == kindRefund {
		if o.undoes == nil {
			return e, fmt.Errorf("import: a point_ledger %s does not name the row it undoes", e.kind)
		}
		e.reverses = o.undoes
	}
	return e, nil
}

// importExternalAwards carries external_award as learning_award, keeping the ID (the ledger's
// source), the source event ID (so a learning app's retry still replays and never pays twice),
// the child, the rule and its snapshot. The old table kept no provider, external ID, day or
// credential: the day is the household day of the award; the provider and external ID are the
// child's link at that time (the link every old award went through); the credential stays empty.
func importExternalAwards(ctx context.Context, x *run, entries []newEntry) error {
	x.r.columnsRead("external_award", "household_id", "id", "source_event_id", "child_id", "rule_id", "rule_snapshot", "ledger_id")
	ledger := map[uuid.UUID]newEntry{}
	for _, e := range entries {
		if e.fromLearner && e.kind == kindAward {
			ledger[e.old.id] = e
		}
	}
	zones := map[uuid.UUID]*time.Location{}
	rows, err := x.dst.Query(ctx, `SELECT id, time_zone FROM household`)
	if err != nil {
		return safe(err)
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return safe(err)
		}
		loc, err := day.Zone(name)
		if err != nil {
			return fmt.Errorf("import: a household's time zone: %w", err)
		}
		zones[id] = loc
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}

	type award struct {
		household, id, child, rule, ledgerID uuid.UUID
		event                                string
		snapshot                             []byte
		provider, external                   *string
	}
	rows, err = x.src.Query(ctx, `
		SELECT a.household_id, a.id, a.source_event_id, a.child_id, a.rule_id, a.rule_snapshot, a.ledger_id, k.provider, k.external_id
		FROM opencozy.external_award a
		JOIN opencozy.point_ledger l ON l.household_id = a.household_id AND l.id = a.ledger_id
		LEFT JOIN LATERAL (
		    SELECT provider, external_id FROM opencozy.external_identity_link k
		    WHERE k.household_id = a.household_id AND k.child_id = a.child_id
		    ORDER BY (k.linked_at <= l.created_at AND (k.unlinked_at IS NULL OR k.unlinked_at > l.created_at)) DESC, k.linked_at DESC
		    LIMIT 1
		) k ON true
		ORDER BY l.created_at, a.id`)
	if err != nil {
		return safe(err)
	}
	var awards []award
	for rows.Next() {
		var a award
		if err := rows.Scan(&a.household, &a.id, &a.event, &a.child, &a.rule, &a.snapshot, &a.ledgerID, &a.provider, &a.external); err != nil {
			return safe(err)
		}
		awards = append(awards, a)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var n, unlinked int64
	for _, a := range awards {
		e, ok := ledger[a.ledgerID]
		if !ok || e.sourceID != a.id || e.old.child != a.child {
			return errors.New("import: an external_award does not match its point_ledger award")
		}
		loc := zones[a.household]
		if loc == nil {
			return errors.New("import: an external_award's household was not carried")
		}
		event, err := uuid.Parse(a.event)
		if err != nil {
			return errors.New("import: an external_award's source event ID is not a UUID")
		}
		var rule struct {
			ActivityKey string `json:"activityKey"`
		}
		if err := json.Unmarshal(a.snapshot, &rule); err != nil || rule.ActivityKey == "" {
			return errors.New("import: an external_award's rule snapshot has no activity key")
		}
		provider, external := "minecraft", ""
		if a.provider != nil && a.external != nil {
			provider, external = *a.provider, *a.external
		} else {
			unlinked++
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO learning_award (id, household_id, source_event_id, child_id, rule_id, rule, points, day,
			provider, external_id, activity_key, ledger_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10, $11, $12, $13)`,
			a.id, a.household, event, a.child, a.rule, a.snapshot, e.old.delta, day.Of(e.old.created, loc).String(),
			provider, external, rule.ActivityKey, a.ledgerID, e.old.created); err != nil {
			return safe(err)
		}
		n++
	}
	if err := reconcile(ctx, x, "external_award", n, "learning_award"); err != nil {
		return err
	}
	if unlinked > 0 {
		x.r.note("external_award", fmt.Sprintf("%d for a child with no learner link: external ID left empty", unlinked))
	}
	return nil
}

// checkBalances compares every child's balance, and the total, in both apps.
func checkBalances(ctx context.Context, x *run) error {
	// Each child's old balance, plus what they spent on rewards still waiting for an answer
	// (those points come back; see importLedger).
	rows, err := x.src.Query(ctx, `SELECT c.household_id, c.id, COALESCE(sum(l.delta) FILTER (WHERE rr.state IS DISTINCT FROM 'pending'), 0)::bigint
		FROM opencozy.child c
		LEFT JOIN opencozy.point_ledger l ON l.household_id = c.household_id AND l.child_id = c.id
		LEFT JOIN opencozy.reward_redemption rr ON rr.household_id = l.household_id AND rr.id = l.redemption_id
		GROUP BY c.household_id, c.id`)
	if err != nil {
		return safe(err)
	}
	type bal struct {
		household, child uuid.UUID
		points           int64
	}
	var olds []bal
	for rows.Next() {
		var b bal
		if err := rows.Scan(&b.household, &b.child, &b.points); err != nil {
			return safe(err)
		}
		olds = append(olds, b)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var equal, oldTotal, newTotal int64
	for _, b := range olds {
		var got int64
		if err := x.dst.QueryRow(ctx, `SELECT COALESCE(sum(delta), 0)::bigint FROM point_ledger WHERE household_id = $1 AND child_id = $2`,
			b.household, b.child).Scan(&got); err != nil {
			return safe(err)
		}
		if got == b.points {
			equal++
		}
		oldTotal += b.points
	}
	if err := x.dst.QueryRow(ctx, `SELECT COALESCE(sum(delta), 0)::bigint FROM point_ledger`).Scan(&newTotal); err != nil {
		return safe(err)
	}
	x.r.check("children whose balance matches (+ waiting rewards' points)", int64(len(olds)), equal)
	x.r.check("points held, all children (+ waiting rewards' points)", oldTotal, newTotal)
	return nil
}

// collectIDs reads one UUID column.
func collectIDs(rows pgx.Rows) ([]uuid.UUID, error) {
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return ids, safe(err)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
