package calendar_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/calendar"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/importer"
	"github.com/oldsj/opencozy-next/internal/keyring"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

// The fixture feed for the imported "School" link: the swimming series the old app had
// assigned, weekly from 7 January 2026, with keys in the same form as the old app's.
const swimFeed = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//Fixture//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:swim@example.test\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260107T163000Z\r\n" +
	"DTEND:20260107T173000Z\r\nRRULE:FREQ=WEEKLY;COUNT=4\r\nSUMMARY:Swimming lesson\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

// feed answers only the imported link, so the test also shows the link survived sealing.
type feed struct{ t *testing.T }

func (f feed) Do(r *http.Request) (*http.Response, error) {
	if r.URL.String() != "https://calendar.example.test/school.ics?token=fixture-feed-token" {
		f.t.Errorf("fetched an unexpected link")
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(swimFeed)), Header: http.Header{}, Request: r}, nil
}

type poolTx struct{ db testdb.DB }

func (p poolTx) Tx(ctx context.Context, fn func(*dbq.Queries) error) error {
	return pgx.BeginFunc(ctx, p.db.Runtime, func(tx pgx.Tx) error { return fn(dbq.New(tx)) })
}

// Imported assignments land on their events once the first refresh rebuilds them: the old
// app's series UIDs and occurrence keys are the new app's.
func TestImportedAssignmentsSurviveTheFirstRefresh(t *testing.T) {
	schema, err := os.ReadFile("../importer/testdata/old_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile("../importer/testdata/old_seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	complete, err := os.ReadFile("../importer/testdata/complete.sql")
	if err != nil {
		t.Fatal(err)
	}
	old := testdb.Scratch(t, string(schema), string(seed), string(complete))
	target := testdb.Open(t)
	kr, err := keyring.Parse("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := importer.Run(ctx, importer.Options{SourceURL: old, Target: target.Owner, Out: io.Discard, Keys: kr}); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 12, 12, 0, 0, 0, time.UTC) }
	if err := calendar.Refresh(ctx, poolTx{target}, kr, calendar.FetcherFor(feed{t}), slog.New(slog.NewTextHandler(io.Discard, nil)), now); err != nil {
		t.Fatal(err)
	}
	var synced bool
	if err := target.Owner.QueryRow(ctx, `SELECT last_success_at IS NOT NULL FROM calendar_source WHERE id = '0190a000-0000-7000-8000-0000000000b1'`).Scan(&synced); err != nil || !synced {
		t.Fatalf("the imported link did not sync: %v", err)
	}

	// Each swimming lesson, with the assignment that applies to it: the occurrence's own if
	// there is one, otherwise the series'. Children, or "nobody".
	rows, err := target.Owner.Query(ctx, `SELECT e.recurrence_key,
			COALESCE(a.recurrence_key, '-'),
			COALESCE((SELECT string_agg(c.child_id::text, ',') FROM calendar_assignment_child c WHERE c.assignment_id = a.id), 'nobody')
		FROM calendar_event e
		JOIN calendar_series s ON s.id = e.series_id AND s.uid = 'swim@example.test'
		LEFT JOIN LATERAL (SELECT * FROM calendar_assignment x WHERE x.series_id = e.series_id AND x.recurrence_key IN (e.recurrence_key, '*')
			ORDER BY x.recurrence_key = '*' LIMIT 1) a ON true
		ORDER BY e.recurrence_key`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var key, by, who string
		if err := rows.Scan(&key, &by, &who); err != nil {
			t.Fatal(err)
		}
		got = append(got, key+" "+by+" "+who)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	const avery = "0190a000-0000-7000-8000-0000000000c1"
	want := []string{
		"20260107T163000Z * " + avery,
		"20260114T163000Z 20260114T163000Z nobody",
		"20260121T163000Z * " + avery, // the cleared occurrence assignment was left out
		"20260128T163000Z * " + avery,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("events and their assignments:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
