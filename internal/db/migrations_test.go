package db

import (
	"fmt"
	"io/fs"
	"regexp"
	"testing"
)

// Two branches can each add migration 00007 under different names: git merges both without a
// conflict and goose fails only after they land. This catches it on every PR.
func TestMigrationNumbersAreUniqueAndContiguous(t *testing.T) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^migrations/(\d{5})_[a-z0-9_]+\.sql$`)
	for i, name := range names { // fs.Glob returns them sorted
		m := re.FindStringSubmatch(name)
		if m == nil {
			t.Fatalf("%s: want migrations/NNNNN_name.sql", name)
		}
		if want := fmt.Sprintf("%05d", i+1); m[1] != want {
			t.Fatalf("%s: want number %s (unique and contiguous from 00001)", name, want)
		}
	}
	if len(names) == 0 {
		t.Fatal("no migrations")
	}
}
