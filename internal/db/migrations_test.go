package db

import (
	"fmt"
	"io/fs"
	"regexp"
	"testing"
)

// Two branches can each add migration 00007 under different names: git merges both without a
// conflict and goose fails only after they land. This catches it on every PR.
//
// Numbers are also contiguous from 00001: they were renumbered once before cutover, and from
// now on a migration, once deployed, keeps its number. A new one takes the next number.
const contiguousBeforeCutover = true

func TestMigrationNumbersAreUnique(t *testing.T) {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^migrations/(\d{5})_[a-z0-9_]+\.sql$`)
	seen := map[string]string{}
	for i, name := range names { // fs.Glob returns them sorted
		m := re.FindStringSubmatch(name)
		if m == nil {
			t.Fatalf("%s: want migrations/NNNNN_name.sql", name)
		}
		if other, ok := seen[m[1]]; ok {
			t.Fatalf("%s and %s share number %s", other, name, m[1])
		}
		seen[m[1]] = name
		if want := fmt.Sprintf("%05d", i+1); contiguousBeforeCutover && m[1] != want {
			t.Fatalf("%s: want number %s (unique and contiguous from 00001)", name, want)
		}
	}
	if len(names) == 0 {
		t.Fatal("no migrations")
	}
}
