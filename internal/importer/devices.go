package importer

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// importDevices carries paired child devices (slice 7) and adults' passkeys (slice 10a).
//
//   - A device keeps its ID, child, name, platform, key and times, so an iPad that signs its
//     requests with its old key needs no new pairing. Revoked devices come too: the list shows
//     them as revoked. The old public key (base64 of the uncompressed P-256 point) becomes bytes.
//   - Push tokens are not carried: the kids' app is a new build on the new API and registers
//     again.
//   - A passkey keeps its ID, name, credential ID, public key, sign counter, transports, backup
//     flag and times. Its user handle is the old user ID, which the adult import kept as
//     adult.webauthn_user_handle, so it signs in as before (on the same hostname: the relying
//     party ID must not change). The old app recorded only "backed up" (at registration), so
//     "backup eligible" is left unknown (NULL, migration 00010): the server adopts the flag from
//     the first sign-in. A guess would be enforced by go-webauthn and could refuse the passkey.
func importDevices(ctx context.Context, x *run) error {
	if err := importChildDevices(ctx, x); err != nil {
		return err
	}
	return importPasskeys(ctx, x)
}

func importChildDevices(ctx context.Context, x *run) error {
	x.r.columnsRead("device", "household_id", "id", "child_id", "name", "platform", "public_key", "paired_at",
		"last_seen_at", "revoked_at",
		// Dropped: the new kids' app registers for push again.
		"push_token", "push_environment", "push_registered_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, child_id, name, platform, public_key, paired_at, last_seen_at,
		revoked_at, push_token IS NOT NULL FROM opencozy.device ORDER BY household_id, paired_at, id`)
	if err != nil {
		return safe(err)
	}
	type device struct {
		household, id       uuid.UUID
		child               *uuid.UUID
		name, platform, key string
		paired              time.Time
		lastSeen, revoked   *time.Time
		push                bool
	}
	var ds []device
	for rows.Next() {
		var d device
		if err := rows.Scan(&d.household, &d.id, &d.child, &d.name, &d.platform, &d.key, &d.paired, &d.lastSeen, &d.revoked, &d.push); err != nil {
			rows.Close()
			return safe(err)
		}
		ds = append(ds, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var push int64
	for _, d := range ds {
		key, err := base64.StdEncoding.DecodeString(d.key)
		if err != nil || len(key) != 65 || key[0] != 4 {
			return fmt.Errorf("import: device %s: the public key is not a P-256 point", d.id)
		}
		if d.push {
			push++
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO device (id, household_id, child_id, name, platform, public_key, paired_at, last_seen_at, revoked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			d.id, d.household, d.child, d.name, d.platform, key, d.paired, d.lastSeen, d.revoked); err != nil {
			return safe(err)
		}
	}
	if err := reconcile(ctx, x, "device", int64(len(ds)), "device"); err != nil {
		return err
	}
	if push > 0 {
		x.r.note("device", fmt.Sprintf("%d push registrations not carried: the new app registers again", push))
	}
	return nil
}

func importPasskeys(ctx context.Context, x *run) error {
	x.r.columnsRead("parent_passkey", "id", "user_id", "name", "credential_id", "public_key", "counter", "transports",
		"backed_up", "created_at", "last_used_at")
	rows, err := x.src.Query(ctx, `SELECT id, user_id, name, credential_id, public_key, counter, transports, backed_up,
		created_at, last_used_at FROM opencozy.parent_passkey ORDER BY created_at, id`)
	if err != nil {
		return safe(err)
	}
	type passkey struct {
		id                    uuid.UUID
		user, name, cred, key string
		counter               int64
		transports            []string
		backedUp              bool
		created               time.Time
		lastUsed              *time.Time
	}
	var ps []passkey
	for rows.Next() {
		var p passkey
		if err := rows.Scan(&p.id, &p.user, &p.name, &p.cred, &p.key, &p.counter, &p.transports, &p.backedUp, &p.created, &p.lastUsed); err != nil {
			rows.Close()
			return safe(err)
		}
		ps = append(ps, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var carried, orphaned int64
	for _, p := range ps {
		a, ok := x.adults[p.user]
		if !ok {
			orphaned++ // the adult was not carried (no household)
			continue
		}
		cred, err1 := base64.RawURLEncoding.DecodeString(p.cred)
		key, err2 := base64.RawURLEncoding.DecodeString(p.key)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("import: passkey %s: the credential ID or public key is not base64url", p.id)
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO passkey (id, household_id, adult_id, credential_id, public_key, sign_count, transports,
				backup_eligible, backed_up, name, created_at, last_used_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, $8, $9, $10, $11)`,
			p.id, a.household, a.id, cred, key, p.counter, p.transports, p.backedUp, p.name, p.created, p.lastUsed); err != nil {
			return safe(err)
		}
		carried++
	}
	got, err := countNew(ctx, x.dst, "passkey")
	if err != nil {
		return err
	}
	x.r.table("parent_passkey", int64(len(ps)), "passkey", carried, got)
	x.r.check("parent_passkey rows read = carried + of adults not carried", int64(len(ps)), carried+orphaned)
	if orphaned > 0 {
		x.r.note("parent_passkey", fmt.Sprintf("%d belong to someone who is not an adult here: not carried", orphaned))
	}
	return nil
}
