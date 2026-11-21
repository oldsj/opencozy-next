package db_test

import (
	"context"
	"testing"

	"github.com/oldsj/opencozy-next/internal/testdb"
)

// A deployment's database gives PUBLIC no CONNECT (the CNPG bootstraps revoke it), so the
// runtime role connects only because migrations grant it CONNECT by name. testdb revokes it too.
func TestRuntimeRoleConnectsWithoutPublicConnect(t *testing.T) {
	d := testdb.Open(t)
	ctx := context.Background()

	var public bool
	if err := d.Owner.QueryRow(ctx, `
		SELECT coalesce(bool_or(a.grantee = 0 AND a.privilege_type = 'CONNECT'), false)
		FROM pg_database d, aclexplode(coalesce(d.datacl, acldefault('d', d.datdba))) a
		WHERE d.datname = current_database()`).Scan(&public); err != nil {
		t.Fatal(err)
	}
	if public {
		t.Fatal("PUBLIC has CONNECT on the test database; testdb should revoke it")
	}

	var user string
	if err := d.Runtime.QueryRow(ctx, "SELECT current_user").Scan(&user); err != nil {
		t.Fatalf("the runtime role can't connect: %v", err)
	}
	var can bool
	if err := d.Owner.QueryRow(ctx, "SELECT has_database_privilege($1, current_database(), 'CONNECT')", user).Scan(&can); err != nil {
		t.Fatal(err)
	}
	if !can {
		t.Fatalf("%s has no CONNECT on the database", user)
	}
}
