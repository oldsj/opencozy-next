package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/oldsj/opencozy-next/internal/testdb"
)

func TestDevicesAndPasskeysCommit(t *testing.T) {
	target := testdb.Open(t)
	out, err := importOnce(t, oldDB(t, complete), target, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "1 push registrations not carried") {
		t.Errorf("no push note:\n%s", out)
	}
	ctx, db := context.Background(), target.Owner

	// Devices keep child, key (now bytes) and revocation.
	var devices string
	if err := db.QueryRow(ctx, `SELECT string_agg(right(id::text, 2) || ':' || COALESCE(right(child_id::text, 2), '-') || ':' || platform || ':' ||
		length(public_key) || ':' || get_byte(public_key, 0) || ':' || (revoked_at IS NOT NULL), ' ' ORDER BY id) FROM device`).Scan(&devices); err != nil ||
		devices != "e1:c1:ios:65:4:false e2:-:android:65:4:true" {
		t.Errorf("devices: %q %v", devices, err)
	}

	// The passkey belongs to the owner, whose user handle is the old user ID it was made for.
	var (
		handle             []byte
		credLen, keyLen    int
		count              int64
		transports         string
		eligible, backedUp bool
	)
	if err := db.QueryRow(ctx, `SELECT a.webauthn_user_handle, length(p.credential_id), length(p.public_key), p.sign_count,
		array_to_string(p.transports, ','), p.backup_eligible, p.backed_up
		FROM passkey p JOIN adult a ON a.id = p.adult_id`).Scan(&handle, &credLen, &keyLen, &count, &transports, &eligible, &backedUp); err != nil {
		t.Fatal(err)
	}
	if string(handle) != "fixtureOwnerUser01" || credLen != 32 || keyLen != 77 || count != 7 || transports != "internal,hybrid" || !eligible || !backedUp {
		t.Errorf("passkey: %q %d %d %d %s %v %v", handle, credLen, keyLen, count, transports, eligible, backedUp)
	}
}
