package importer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// importTV carries the TV gate (slice 9): TVs with their IDs, power, attempt run, open hours and
// override; their MACs; sessions with their IDs (who started or ended one maps to the adult, or
// to the child's device, by ID; anything else is left empty); and the history of openings. TV
// credits need their redemption: those whose redemption isn't imported wait for it (pending,
// the rewards import). tv_prompt is not carried (notCarried).
func importTV(ctx context.Context, x *run) error {
	x.r.columnsRead("gated_tv", "household_id", "id", "name", "created_at", "archived_at", "room", "ha_entity_id", "power", "power_changed_at",
		"attempts_since", "last_attempt_at", "open_start", "open_end", "open_until", "open_days")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, name, created_at, archived_at, room, ha_entity_id, power, power_changed_at,
		attempts_since, last_attempt_at, open_start, open_end, open_until, open_days FROM opencozy.gated_tv ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	type tvRow struct {
		household, id                         uuid.UUID
		name, power                           string
		created                               time.Time
		archived, changed, since, last, until *time.Time
		room, entity                          *string
		start, end                            *int32
		days                                  *int16
	}
	var tvs []tvRow
	for rows.Next() {
		var t tvRow
		if err := rows.Scan(&t.household, &t.id, &t.name, &t.created, &t.archived, &t.room, &t.entity, &t.power, &t.changed,
			&t.since, &t.last, &t.start, &t.end, &t.until, &t.days); err != nil {
			return safe(err)
		}
		tvs = append(tvs, t)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	// Power and the router's attempt run are forgotten, not copied: what reported them (the old
	// app's Home Assistant connection or push) isn't carried, and a frozen "off" would keep a TV
	// from ever asking again. Each TV is unknown until Home Assistant reports it here.
	forgotten := 0
	for _, t := range tvs {
		if t.power != "unknown" || t.since != nil || t.last != nil {
			forgotten++
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO tv (household_id, id, name, room, ha_entity_id, power, power_changed_at, attempts_since,
			last_attempt_at, open_start, open_end, open_days, open_until, created_at, archived_at)
			VALUES ($1, $2, $3, $4, $5, 'unknown', NULL, NULL, NULL, $6, $7, $8, $9, $10, $11)`,
			t.household, t.id, strings.TrimSpace(t.name), t.room, t.entity, t.start, t.end, t.days, t.until,
			t.created, t.archived); err != nil {
			return safe(err)
		}
	}
	if forgotten > 0 {
		x.r.note("gated_tv", fmt.Sprintf("%d TVs' power and router attempts forgotten: unknown until Home Assistant reports here", forgotten))
	}
	if err := reconcile(ctx, x, "gated_tv", int64(len(tvs)), "tv"); err != nil {
		return err
	}

	x.r.columnsRead("gated_tv_mac", "household_id", "tv_id", "mac")
	n, err := copyRows(ctx, x, `SELECT household_id, tv_id, mac FROM opencozy.gated_tv_mac ORDER BY household_id, mac`,
		`INSERT INTO tv_mac (household_id, tv_id, mac) VALUES ($1, $2, $3)`, 3)
	if err != nil {
		return err
	}
	if err := reconcile(ctx, x, "gated_tv_mac", n, "tv_mac"); err != nil {
		return err
	}

	if err := importTVSessions(ctx, x); err != nil {
		return err
	}
	if err := importTVOpenEvents(ctx, x); err != nil {
		return err
	}
	return importTVCredits(ctx, x)
}

// copyRows copies rows of cols columns unchanged.
func copyRows(ctx context.Context, x *run, query, insert string, cols int) (int64, error) {
	rows, err := x.src.Query(ctx, query)
	if err != nil {
		return 0, safe(err)
	}
	var all [][]any
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			return 0, safe(err)
		}
		all = append(all, v[:cols])
	}
	if err := rows.Err(); err != nil {
		return 0, safe(err)
	}
	for _, v := range all {
		if _, err := x.dst.Exec(ctx, insert, v...); err != nil {
			return 0, safe(err)
		}
	}
	return int64(len(all)), nil
}

// oldActor reads the old app's started_by / ended_by / by text: an adult's user ID, or
// "device:<id>" for a child's own device. Anything else (or an adult of another household) is
// nobody.
func (x *run) oldActor(household uuid.UUID, by *string) (adult, device *uuid.UUID) {
	if by == nil {
		return nil, nil
	}
	if id, ok := strings.CutPrefix(*by, "device:"); ok {
		if d, err := uuid.Parse(id); err == nil {
			return nil, &d
		}
		return nil, nil
	}
	if a, ok := x.adults[*by]; ok && a.household == household {
		return &a.id, nil
	}
	return nil, nil
}

func importTVSessions(ctx context.Context, x *run) error {
	x.r.columnsRead("tv_session", "household_id", "id", "tv_id", "watcher", "child_id", "charged_seconds", "started_at", "ended_at",
		"end_reason", "started_by", "ended_by", "ends_by", "warned_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, tv_id, watcher, child_id, charged_seconds, started_at, ended_at, end_reason,
		started_by, ended_by, ends_by, warned_at FROM opencozy.tv_session ORDER BY household_id, started_at, id`)
	if err != nil {
		return safe(err)
	}
	type sessionRow struct {
		household, id, tv          uuid.UUID
		watcher                    string
		child                      *uuid.UUID
		charged                    int32
		started                    time.Time
		ended, endsBy, warned      *time.Time
		reason, startedBy, endedBy *string
	}
	var all []sessionRow
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.household, &s.id, &s.tv, &s.watcher, &s.child, &s.charged, &s.started, &s.ended, &s.reason,
			&s.startedBy, &s.endedBy, &s.endsBy, &s.warned); err != nil {
			return safe(err)
		}
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	// Open sessions are dropped, counted (James, 2026-11-21): the TV goes offline at cutover and
	// the child starts again. The old app charged a session as it ran, so its credit already
	// holds what is left, less the few seconds since the last charge.
	var nobody, open, carried int
	for _, s := range all {
		if s.ended == nil {
			open++
			continue
		}
		carried++
		adult, device := x.oldActor(s.household, s.startedBy)
		if adult == nil && device == nil {
			nobody++
		}
		endedBy, _ := x.oldActor(s.household, s.endedBy)
		if _, err := x.dst.Exec(ctx, `INSERT INTO tv_session (household_id, id, tv_id, watcher, child_id, charged_seconds, ends_by, started_at,
			ended_at, end_reason, started_by_adult, started_by_device, ended_by_adult, warned_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			s.household, s.id, s.tv, s.watcher, s.child, s.charged, s.endsBy, s.started, s.ended, s.reason, adult, device, endedBy, s.warned); err != nil {
			return safe(err)
		}
	}
	src, err := count(ctx, x.src, "tv_session")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "tv_session")
	if err != nil {
		return err
	}
	x.r.table("tv_session", src, "tv_session", int64(carried), got)
	if src != int64(len(all)) {
		x.r.check("tv_session rows read", src, int64(len(all)))
	}
	if open > 0 {
		x.r.note("tv_session", fmt.Sprintf("%d open sessions dropped: those TVs go offline at cutover and start again", open))
	}
	if nobody > 0 {
		x.r.note("tv_session", fmt.Sprintf("%d started by no adult or device of the household: started_by left empty", nobody))
	}
	return nil
}

func importTVOpenEvents(ctx context.Context, x *run) error {
	x.r.columnsRead("tv_open_event", "household_id", "id", "tv_id", "action", "until", "at", "by", "assistant_credential_id", "ended_session_id")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, tv_id, action, until, at, by, assistant_credential_id, ended_session_id
		FROM opencozy.tv_open_event ORDER BY household_id, at, id`)
	if err != nil {
		return safe(err)
	}
	type eventRow struct {
		household, id, tv uuid.UUID
		action, by        string
		until             *time.Time
		at                time.Time
		via, ended        *uuid.UUID
	}
	var all []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.household, &e.id, &e.tv, &e.action, &e.until, &e.at, &e.by, &e.via, &e.ended); err != nil {
			return safe(err)
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	for _, e := range all {
		adult, _ := x.oldActor(e.household, &e.by)
		if _, err := x.dst.Exec(ctx, `INSERT INTO tv_open_event (household_id, id, tv_id, action, until, at, by_adult, via_credential, ended_session_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, e.household, e.id, e.tv, e.action, e.until, e.at, adult, e.via, e.ended); err != nil {
			return safe(err)
		}
	}
	return reconcile(ctx, x, "tv_open_event", int64(len(all)), "tv_open_event")
}

// importTVCredits carries each child's banked TV time as one balance (James, 2026-11-21), as
// importScreenTime does for the iPad: what the old credits still hold (seconds - used_seconds),
// in day-sized credits, a grant from no one in particular (redemption and granted_by NULL), so a
// parent can still take it back. The old app charged a session as it ran, so the balance already
// holds an open session's rest.
func importTVCredits(ctx context.Context, x *run) error {
	x.r.columnsRead("tv_credit", "household_id", "child_id", "seconds", "used_seconds",
		// Dropped: one balance per child replaces the per-redemption credits.
		"redemption_id", "target", "minutes", "created_at")
	return bankBalances(ctx, x, "tv_credit", "tv_credit", "TV", func(h, c uuid.UUID, s int64) error {
		_, err := x.dst.Exec(ctx, `INSERT INTO tv_credit (household_id, child_id, granted_seconds, seconds, created_at)
			VALUES ($1, $2, $3, $3, $4)`, h, c, s, x.started)
		return err
	})
}
