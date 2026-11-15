package importer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// importLearning carries award rules (IDs, versions, archive times), learner links (each gets a
// new ID; unlinked ones stay unlinked) and the rules each carried learning credential may use.
// The awards themselves wait on the ledger import.
func importLearning(ctx context.Context, x *run) error {
	x.r.columnsRead("award_rule", "household_id", "id", "activity_key", "points", "daily_limit", "version", "archived_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, activity_key, points, daily_limit, version, archived_at
		FROM opencozy.award_rule ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	var n int64
	for rows.Next() {
		var (
			household, id          uuid.UUID
			key                    string
			points, limit, version int32
			archived               *time.Time
		)
		if err := rows.Scan(&household, &id, &key, &points, &limit, &version, &archived); err != nil {
			return safe(err)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO award_rule (household_id, id, activity_key, points, daily_limit, version, archived_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, household, id, key, points, limit, version, archived); err != nil {
			return safe(err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	if err := reconcile(ctx, x, "award_rule", n, "award_rule"); err != nil {
		return err
	}

	x.r.columnsRead("external_identity_link", "household_id", "child_id", "provider", "external_id", "linked_at", "unlinked_at")
	rows, err = x.src.Query(ctx, `SELECT household_id, child_id, provider, external_id, linked_at, unlinked_at
		FROM opencozy.external_identity_link ORDER BY household_id, linked_at, child_id`)
	if err != nil {
		return safe(err)
	}
	n = 0
	for rows.Next() {
		var (
			household, child   uuid.UUID
			provider, external string
			linked             time.Time
			unlinked           *time.Time
		)
		if err := rows.Scan(&household, &child, &provider, &external, &linked, &unlinked); err != nil {
			return safe(err)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		// The new provider names are lower-case slugs; one that isn't fails the import.
		if _, err := x.dst.Exec(ctx, `INSERT INTO learning_link (household_id, id, child_id, provider, external_id, linked_at, unlinked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, household, id, child, provider, external, linked, unlinked); err != nil {
			return safe(err)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	if err := reconcile(ctx, x, "external_identity_link", n, "learning_link"); err != nil {
		return err
	}

	// Rules of credentials that were carried; the others wait with their credentials.
	x.r.columnsRead("service_credential_award_rule", "household_id", "credential_id", "rule_id")
	rows, err = x.src.Query(ctx, `SELECT household_id, credential_id, rule_id FROM opencozy.service_credential_award_rule ORDER BY credential_id, rule_id`)
	if err != nil {
		return safe(err)
	}
	var links, skipped int64
	type ruleRow struct{ household, credential, rule uuid.UUID }
	var all []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.household, &r.credential, &r.rule); err != nil {
			return safe(err)
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	for _, r := range all {
		res, err := x.dst.Exec(ctx, `INSERT INTO credential_award_rule (household_id, credential_id, rule_id)
			SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM credential WHERE id = $2)`, r.household, r.credential, r.rule)
		if err != nil {
			return safe(err)
		}
		if res.RowsAffected() == 0 {
			skipped++
			continue
		}
		links++
	}
	src, err := count(ctx, x.src, "service_credential_award_rule")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "credential_award_rule")
	if err != nil {
		return err
	}
	x.r.table("service_credential_award_rule", src, "credential_award_rule", links, got)
	if skipped > 0 {
		x.r.pending("service_credential_award_rule", fmt.Sprintf("%d rows wait with their credentials", skipped))
	}
	return nil
}
