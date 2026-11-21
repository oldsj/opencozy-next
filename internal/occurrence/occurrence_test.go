package occurrence

import (
	"testing"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/day"
)

// The IDs are stored: changing the namespace or the encoding would orphan every occurrence.
func TestIDIsStable(t *testing.T) {
	def := uuid.MustParse("0190a000-0000-7000-8000-000000000101")
	got := ID(def, day.Date{Year: 2026, Month: 3, Day: 9})
	if want := uuid.MustParse("311d9827-29f9-5f42-92eb-07e95548a6f9"); got != want {
		t.Fatalf("ID = %s, want %s", got, want)
	}
}
