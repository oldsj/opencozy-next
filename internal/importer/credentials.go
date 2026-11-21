package importer

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Old scope names and their new names. A credential with any scope missing here waits on the
// slice that adds it (pending), so a credential is never imported with less, or more, access
// than it had. Each domain slice adds its scopes.
var (
	serviceScopes = map[string]string{"children:read": "children:read", "external:award": "learning:award", "games:gate": "games:gate", "screentime:read": "screentime:read"}
	// The old assistant's opt-in scopes. Its tv:* scopes wait for the TV slice's tools.
	assistantScopes = map[string]string{
		"approvals:read": "inbox:read", "approvals:write": "inbox:decide",
		"chores:log": "today:complete", "screen-time:grant": "screentime:grant",
	}
	// The old assistant could always read the household's children, chores, routines, points,
	// history, rewards and screen time; the new one needs these scopes for that.
	assistantReads = []string{"children:read", "household:read", "points:read", "rewards:read", "screentime:read", "today:read"}
)

// importCredentials carries service (ocz_) and assistant (oca_) credentials with their IDs.
// The old secrets are prefix + 43 base64url characters stored as hex SHA-256, which is exactly
// the new format, so imported secrets keep working. A service credential keeps exactly its old
// children; one with none keeps none. An assistant acts for its adult, for all children, as
// before. last_used_at is not carried; importLearning carries the award rules.
func importCredentials(ctx context.Context, x *run) error {
	x.r.columnsRead("service_credential", "household_id", "id", "name", "credential_hash", "scopes", "created_at", "revoked_at")
	x.r.columnsRead("service_credential_child", "household_id", "credential_id", "child_id")
	x.r.columnsRead("assistant_credential", "household_id", "id", "user_id", "name", "credential_hash", "scopes", "created_at", "revoked_at", "last_used_at")

	carried := map[uuid.UUID]bool{}
	var services int64
	waiting := map[string]int{}
	err := eachCredential(ctx, x, `SELECT household_id, id, NULL::text, name, credential_hash, scopes, created_at, revoked_at
		FROM opencozy.service_credential ORDER BY household_id, id`, func(c oldCredential) error {
		scopes, missing := mapScopes(c.scopes, serviceScopes)
		if len(missing) > 0 {
			for _, m := range missing {
				waiting[m]++
			}
			return nil
		}
		if err := insertCredential(ctx, x, c, "service", scopes, nil, false); err != nil {
			return err
		}
		carried[c.id] = true
		services++
		return nil
	})
	if err != nil {
		return err
	}
	if err := report(ctx, x, "service_credential", "credential (service)", "service", services, waiting); err != nil {
		return err
	}

	// Children of carried credentials.
	rows, err := x.src.Query(ctx, `SELECT household_id, credential_id, child_id FROM opencozy.service_credential_child ORDER BY credential_id, child_id`)
	if err != nil {
		return safe(err)
	}
	var links, skipped int64
	for rows.Next() {
		var h, cred, child uuid.UUID
		if err := rows.Scan(&h, &cred, &child); err != nil {
			return safe(err)
		}
		if !carried[cred] {
			skipped++
			continue
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO credential_child (household_id, credential_id, child_id) VALUES ($1, $2, $3)`, h, cred, child); err != nil {
			return safe(err)
		}
		links++
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	src, err := count(ctx, x.src, "service_credential_child")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "credential_child")
	if err != nil {
		return err
	}
	x.r.table("service_credential_child", src, "credential_child", links, got)
	if skipped > 0 {
		x.r.pending("service_credential_child", fmt.Sprintf("%d rows wait with their credentials", skipped))
	}

	var assistants int64
	waiting = map[string]int{}
	err = eachCredential(ctx, x, `SELECT household_id, id, user_id, name, credential_hash, scopes, created_at, revoked_at
		FROM opencozy.assistant_credential ORDER BY household_id, id`, func(c oldCredential) error {
		scopes, missing := mapScopes(c.scopes, assistantScopes)
		scopes = slices.Compact(slices.Sorted(slices.Values(slices.Concat(scopes, assistantReads))))
		if len(missing) > 0 {
			for _, m := range missing {
				waiting[m]++
			}
			return nil
		}
		a, ok := x.adults[*c.userID]
		if !ok || a.household != c.household {
			return fmt.Errorf("import: an assistant credential's adult is not an adult of its household")
		}
		if err := insertCredential(ctx, x, c, "assistant", scopes, &a.id, true); err != nil {
			return err
		}
		assistants++
		return nil
	})
	if err != nil {
		return err
	}
	return report(ctx, x, "assistant_credential", "credential (assistant)", "assistant", assistants, waiting)
}

type oldCredential struct {
	household, id uuid.UUID
	userID        *string
	name, hash    string
	scopes        []string
	created       time.Time
	revoked       *time.Time
}

func eachCredential(ctx context.Context, x *run, query string, fn func(oldCredential) error) error {
	rows, err := x.src.Query(ctx, query)
	if err != nil {
		return safe(err)
	}
	defer rows.Close()
	var all []oldCredential
	for rows.Next() {
		var c oldCredential
		if err := rows.Scan(&c.household, &c.id, &c.userID, &c.name, &c.hash, &c.scopes, &c.created, &c.revoked); err != nil {
			return safe(err)
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	for _, c := range all {
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

func mapScopes(old []string, table map[string]string) (scopes, missing []string) {
	for _, s := range old {
		if n, ok := table[s]; ok {
			scopes = append(scopes, n)
		} else {
			missing = append(missing, s)
		}
	}
	slices.Sort(scopes)
	return slices.Compact(scopes), missing
}

func insertCredential(ctx context.Context, x *run, c oldCredential, kind string, scopes []string, adult *uuid.UUID, allChildren bool) error {
	hash, err := hex.DecodeString(c.hash)
	if err != nil || len(hash) != 32 {
		return fmt.Errorf("import: a %s credential's hash is not hex SHA-256", kind)
	}
	_, err = x.dst.Exec(ctx, `INSERT INTO credential (id, household_id, kind, name, token_hash, scopes, all_children, adult_id, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		c.id, c.household, kind, c.name, hash, scopes, allChildren, adult, c.created, c.revoked)
	return safe(err)
}

func report(ctx context.Context, x *run, old, label, kind string, want int64, waiting map[string]int) error {
	src, err := count(ctx, x.src, old)
	if err != nil {
		return err
	}
	var got int64
	if err := x.dst.QueryRow(ctx, `SELECT count(*) FROM credential WHERE kind = $1`, kind).Scan(&got); err != nil {
		return safe(err)
	}
	x.r.table(old, src, label, want, got)
	if len(waiting) > 0 {
		names := make([]string, 0, len(waiting))
		for s, n := range waiting {
			names = append(names, fmt.Sprintf("%s (%d)", s, n))
		}
		sort.Strings(names)
		x.r.pending(old, fmt.Sprintf("%d credentials wait on scopes with no new name yet: %s", src-want, strings.Join(names, ", ")))
	}
	return nil
}
