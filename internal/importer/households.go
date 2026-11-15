package importer

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// importHouseholds carries households with their IDs. lock_message and the game domains belong
// to later slices, so the table stays pending until they are mapped.
func importHouseholds(ctx context.Context, x *run) error {
	x.r.columnsRead("household", "id", "name", "time_zone", "created_at", "archived_at")
	rows, err := x.src.Query(ctx, `SELECT id, name, time_zone, created_at, archived_at FROM opencozy.household ORDER BY id`)
	if err != nil {
		return safe(err)
	}
	var n int64
	for rows.Next() {
		var (
			id       uuid.UUID
			name, tz string
			created  time.Time
			archived *time.Time
		)
		if err := rows.Scan(&id, &name, &tz, &created, &archived); err != nil {
			return safe(err)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO household (id, name, time_zone, created_at, archived_at) VALUES ($1, $2, $3, $4, $5)`,
			id, name, tz, created, archived); err != nil {
			return safe(err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	return reconcile(ctx, x, "household", n, "household")
}

// importChildren carries children with their IDs (credential_child refers to them). The colour
// and the allowed hours belong to later slices.
func importChildren(ctx context.Context, x *run) error {
	x.r.columnsRead("child", "household_id", "id", "name", "created_at", "archived_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, name, created_at, archived_at FROM opencozy.child ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	var n int64
	for rows.Next() {
		var (
			household, id uuid.UUID
			name          string
			created       time.Time
			archived      *time.Time
		)
		if err := rows.Scan(&household, &id, &name, &created, &archived); err != nil {
			return safe(err)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO child (household_id, id, name, created_at, archived_at) VALUES ($1, $2, $3, $4, $5)`,
			household, id, name, created, archived); err != nil {
			return safe(err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	return reconcile(ctx, x, "child", n, "child")
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
