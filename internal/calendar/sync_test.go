package calendar

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/keyring"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

type poolTx struct{ pool *pgxpool.Pool }

func (p poolTx) Tx(ctx context.Context, fn func(*dbq.Queries) error) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error { return fn(dbq.New(tx)) })
}

func testKeyring(t *testing.T) *keyring.Keyring {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	kr, err := keyring.Parse("k1:" + base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

// A feed is fetched, stored, refetched conditionally, rebuilt when it changes (keeping event
// ids and assignments), and backed off when it fails.
func TestRefreshStoresAndReplaces(t *testing.T) {
	db := testdb.Open(t)
	ctx := t.Context()
	kr := testKeyring(t)
	var body atomic.Value
	body.Store(string(cal(
		ev("UID:swim@example.test", "DTSTART;TZID=America/New_York:20260914T153000", "DTEND;TZID=America/New_York:20260914T163000",
			"RRULE:FREQ=WEEKLY;COUNT=3", "SUMMARY:Swimming"),
		ev("UID:fair@example.test", "DTSTART;VALUE=DATE:20260918", "SUMMARY:Fair"),
	)))
	var status atomic.Int32
	status.Store(200)
	var gets, conditional atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		if s := int(status.Load()); s != 200 {
			w.WriteHeader(s)
			return
		}
		b := body.Load().(string)
		etag := `"` + Hash([]byte(b))[:8] + `"`
		if r.Header.Get("If-None-Match") == etag {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = io.WriteString(w, b)
	}))
	defer srv.Close()
	f := Fetcher{client: srv.Client()}

	var h, child uuid.UUID
	if err := db.Owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Fictional', 'America/New_York') RETURNING id`).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if err := db.Owner.QueryRow(ctx, `INSERT INTO child (household_id, name) VALUES ($1, 'Ash') RETURNING id`, h).Scan(&child); err != nil {
		t.Fatal(err)
	}
	src := uuid.Must(uuid.NewV7())
	sealed, mac, err := SealFeedURL(kr, h, src, srv.URL+"/school.ics")
	if err != nil {
		t.Fatal(err)
	}
	q := dbq.New(db.Runtime)
	if _, err := q.CreateCalendarSource(ctx, dbq.CreateCalendarSourceParams{ID: src, HouseholdID: h, Name: "School", Color: "butter",
		ShowOnDisplay: true, FeedUrl: &sealed, FeedUrlMac: &mac}); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func() {
		t.Helper()
		if _, err := db.Owner.Exec(ctx, `UPDATE calendar_source SET next_sync_at = now() - interval '1 second'`); err != nil {
			t.Fatal(err)
		}
		if err := Refresh(ctx, poolTx{db.Runtime}, kr, f, log, now); err != nil {
			t.Fatal(err)
		}
	}
	events := func() map[string]uuid.UUID {
		t.Helper()
		rows, err := db.Owner.Query(ctx, `SELECT r.uid || '/' || e.recurrence_key, e.id FROM calendar_event e JOIN calendar_series r ON r.id = e.series_id`)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]uuid.UUID{}
		for rows.Next() {
			var k string
			var id uuid.UUID
			if err := rows.Scan(&k, &id); err != nil {
				t.Fatal(err)
			}
			out[k] = id
		}
		return out
	}
	source := func() dbq.CalendarSource {
		t.Helper()
		s, err := q.GetCalendarSource(ctx, dbq.GetCalendarSourceParams{HouseholdID: h, ID: src})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	run()
	first := events()
	if len(first) != 4 || source().LastSuccessAt == nil || source().ErrorCode != nil {
		t.Fatalf("first sync: %v %+v", first, source())
	}
	swim := first["swim@example.test/20260921T193000Z"]
	if swim == uuid.Nil {
		t.Fatalf("no second swim: %v", first)
	}
	// Assign the series to the child (as the handler does).
	var series uuid.UUID
	if err := db.Owner.QueryRow(ctx, `SELECT series_id FROM calendar_event WHERE id = $1`, swim).Scan(&series); err != nil {
		t.Fatal(err)
	}
	aid, err := q.UpsertAssignment(ctx, dbq.UpsertAssignmentParams{HouseholdID: h, SeriesID: series, RecurrenceKey: "*"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.AddAssignmentChildren(ctx, dbq.AddAssignmentChildrenParams{HouseholdID: h, AssignmentID: aid, ChildIds: []uuid.UUID{child}}); err != nil {
		t.Fatal(err)
	}

	run() // unchanged: a 304
	if conditional.Load() != 1 || len(events()) != 4 {
		t.Fatalf("conditional %d, events %d", conditional.Load(), len(events()))
	}

	// The fair is gone, swimming moves: ids of kept occurrences stay, the assignment stays.
	body.Store(string(cal(ev("UID:swim@example.test", "DTSTART;TZID=America/New_York:20260914T160000", "DTEND;TZID=America/New_York:20260914T170000",
		"RRULE:FREQ=WEEKLY;COUNT=3", "SUMMARY:Swimming (new time)"))))
	run()
	after := events()
	if len(after) != 3 || after["fair@example.test/single"] != uuid.Nil {
		t.Fatalf("after change: %v", after)
	}
	var n int
	if err := db.Owner.QueryRow(ctx, `SELECT count(*) FROM calendar_assignment_child WHERE assignment_id = $1`, aid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("assignment kept: %d %v", n, err)
	}

	// Failures are recorded and backed off (15 min, then 30, 60 … up to 6 h); a success clears them.
	status.Store(http.StatusForbidden)
	run()
	s := source()
	if s.ErrorCode == nil || *s.ErrorCode != CodeAccessDenied || s.ConsecutiveFailures != 1 || len(events()) != 3 {
		t.Fatalf("failure: %+v", s)
	}
	if d := time.Until(s.NextSyncAt); d < 10*time.Minute || d > 20*time.Minute {
		t.Errorf("backoff %s", d)
	}
	status.Store(200)
	run()
	if s := source(); s.ErrorCode != nil || s.ConsecutiveFailures != 0 {
		t.Fatalf("recovered: %+v", s)
	}
	// A removed source is skipped, not an error.
	if _, err := db.Owner.Exec(ctx, `DELETE FROM calendar_source WHERE id = $1`, src); err != nil {
		t.Fatal(err)
	}
	run()
}
