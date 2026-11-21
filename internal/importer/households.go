package importer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// importHouseholds carries households with their IDs and blocked websites (the two lists keep
// the same rules). lock_message belongs to the devices import, so the table stays pending until it
// is mapped.
func importHouseholds(ctx context.Context, x *run) error {
	x.r.columnsRead("household", "id", "name", "time_zone", "created_at", "archived_at", "game_portal_domains", "game_domains")
	rows, err := x.src.Query(ctx, `SELECT id, name, time_zone, created_at, archived_at, game_portal_domains, game_domains
		FROM opencozy.household ORDER BY id`)
	if err != nil {
		return safe(err)
	}
	var n, domains int64
	for rows.Next() {
		var (
			id             uuid.UUID
			name, tz       string
			created        time.Time
			archived       *time.Time
			portals, games []string
		)
		if err := rows.Scan(&id, &name, &tz, &created, &archived, &portals, &games); err != nil {
			return safe(err)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO household (id, name, time_zone, created_at, archived_at, game_portal_domains, game_domains)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, name, tz, created, archived, portals, games); err != nil {
			return safe(err)
		}
		n++
		domains += int64(len(portals) + len(games))
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	if err := reconcile(ctx, x, "household", n, "household"); err != nil {
		return err
	}
	var got int64
	if err := x.dst.QueryRow(ctx, `SELECT COALESCE(sum(cardinality(game_portal_domains) + cardinality(game_domains)), 0) FROM household`).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.check("blocked websites", domains, got)
	return nil
}

// importChildren carries children with their IDs (credential_child refers to them) and their
// allowed hours (minutes after local midnight, as before). The colour belongs to a later slice.
func importChildren(ctx context.Context, x *run) error {
	x.r.columnsRead("child", "household_id", "id", "name", "created_at", "archived_at",
		"school_night_start", "school_night_end", "weekend_start", "weekend_end")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, name, created_at, archived_at,
		school_night_start, school_night_end, weekend_start, weekend_end FROM opencozy.child ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	var n, hours, partial int64
	for rows.Next() {
		var (
			household, id  uuid.UUID
			name           string
			created        time.Time
			archived       *time.Time
			snStart, snEnd *int32
			weStart, weEnd *int32
		)
		if err := rows.Scan(&household, &id, &name, &created, &archived, &snStart, &snEnd, &weStart, &weEnd); err != nil {
			return safe(err)
		}
		// The old check let a rule with one to three times missing through; both apps read it as
		// no rule, and the new check refuses it. Carry it as none, and say how many.
		if set := countSet(snStart, snEnd, weStart, weEnd); set > 0 && set < 4 {
			snStart, snEnd, weStart, weEnd = nil, nil, nil, nil
			partial++
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO child (household_id, id, name, created_at, archived_at,
			school_night_start, school_night_end, weekend_start, weekend_end) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			household, id, name, created, archived, snStart, snEnd, weStart, weEnd); err != nil {
			return safe(err)
		}
		n++
		if snStart != nil {
			hours++
		}
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	if err := reconcile(ctx, x, "child", n, "child"); err != nil {
		return err
	}
	var got int64
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM child WHERE school_night_start IS NOT NULL`).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.check("children with allowed hours", hours, got)
	if partial > 0 {
		x.r.note("child", fmt.Sprintf("%d with partly set allowed hours, carried as none (any time)", partial))
	}
	return nil
}

// countSet counts the times that are set.
func countSet(times ...*int32) int {
	n := 0
	for _, t := range times {
		if t != nil {
			n++
		}
	}
	return n
}

// reconcile records an old table carried one row to one row: want rows were written.
func reconcile(ctx context.Context, x *run, old string, want int64, target string) error {
	src, err := count(ctx, x.src, old)
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, target)
	if err != nil {
		return err
	}
	x.r.table(old, src, target, want, got)
	if src != want {
		x.r.check(old+" rows read", src, want)
	}
	return nil
}
