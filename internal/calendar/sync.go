package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/jobs"
	"github.com/oldsj/opencozy-next/internal/keyring"
	"github.com/oldsj/opencozy-next/internal/live"
)

// feedAAD binds a feed URL's ciphertext to its row.
func feedAAD(householdID, sourceID uuid.UUID) keyring.AAD {
	return keyring.AAD{HouseholdID: householdID.String(), Table: "calendar_source", RowID: sourceID.String(), Purpose: "feed_url"}
}

// SealFeedURL encrypts a feed URL for its row and computes its MAC.
func SealFeedURL(kr *keyring.Keyring, householdID, sourceID uuid.UUID, feedURL string) (sealed, mac string, err error) {
	if sealed, err = kr.Encrypt(feedAAD(householdID, sourceID), []byte(feedURL)); err != nil {
		return "", "", err
	}
	mac, err = kr.FeedMAC(householdID.String(), []byte(feedURL))
	return sealed, mac, err
}

func init() {
	keyring.Register("calendar_source.feed_url", rekeyFeedURLs)
}

func rekeyFeedURLs(ctx context.Context, pool *pgxpool.Pool, kr *keyring.Keyring) (int, error) {
	q := dbq.New(pool)
	rows, err := q.ListFeedURLs(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, r := range rows {
		aad := feedAAD(r.HouseholdID, r.ID)
		sealed, did, err := kr.Rekey(aad, *r.FeedUrl)
		if err != nil {
			return changed, fmt.Errorf("calendar_source %s: %w", r.ID, err)
		}
		plain, err := kr.Decrypt(aad, sealed)
		if err != nil {
			return changed, err
		}
		mac, err := kr.FeedMAC(r.HouseholdID.String(), plain)
		if err != nil {
			return changed, err
		}
		if !did && r.FeedUrlMac != nil && *r.FeedUrlMac == mac {
			continue
		}
		if err := q.SetFeedURL(ctx, dbq.SetFeedURLParams{ID: r.ID, FeedUrl: &sealed, FeedUrlMac: &mac}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// Refresh limits.
const (
	claimBatch   = 10
	batchesInRun = 5
)

// Job refreshes due ICS feeds every minute (each feed every 15 minutes once it succeeds, backing
// off up to 6 hours while it fails). wake starts a run early, after a feed is added.
func Job(kr *keyring.Keyring, f Fetcher, log *slog.Logger, wake <-chan struct{}) jobs.Job {
	return jobs.Job{Name: "calendar-refresh", Every: time.Minute, Timeout: 5 * time.Minute, Wake: wake,
		Run: func(ctx context.Context, fence jobs.Fence) error {
			return Refresh(ctx, fence, kr, f, log, time.Now)
		}}
}

// Txer runs a transaction; jobs.Fence is one (a test passes a plain pool).
type Txer interface {
	Tx(ctx context.Context, fn func(q *dbq.Queries) error) error
}

// Refresh runs one pass over the due feeds.
func Refresh(ctx context.Context, fence Txer, kr *keyring.Keyring, f Fetcher, log *slog.Logger, now func() time.Time) error {
	for range batchesInRun {
		var due []dbq.ClaimDueFeedsRow
		if err := fence.Tx(ctx, func(q *dbq.Queries) (err error) {
			due, err = q.ClaimDueFeeds(ctx, claimBatch)
			return err
		}); err != nil {
			return err
		}
		if len(due) == 0 {
			return nil
		}
		for _, row := range due {
			if err := syncOne(ctx, fence, kr, f, log, now(), row); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncOne fetches and stores one feed. Fetch and parse failures are recorded on the source;
// only database and lease errors are returned.
func syncOne(ctx context.Context, fence Txer, kr *keyring.Keyring, f Fetcher, log *slog.Logger, now time.Time, row dbq.ClaimDueFeedsRow) error {
	failed := func(code string, err error) error {
		log.InfoContext(ctx, "calendar refresh failed", "source", row.ID, "code", code, "err", err)
		return fence.Tx(ctx, func(q *dbq.Queries) error {
			if err := q.FeedFailed(ctx, dbq.FeedFailedParams{ID: row.ID, ErrorCode: &code}); err != nil {
				return err
			}
			return live.Notify(ctx, q, row.HouseholdID, live.Event{Topic: live.Calendar})
		})
	}
	zone, err := day.Zone(row.TimeZone)
	if err != nil {
		return failed(CodeInvalidCalendar, err)
	}
	w := WindowAround(day.Of(now, zone))
	if kr == nil || row.FeedUrl == nil {
		return failed(CodeInvalidCalendar, errors.New("no keyring or feed URL"))
	}
	plain, err := kr.Decrypt(feedAAD(row.HouseholdID, row.ID), *row.FeedUrl)
	if err != nil {
		log.ErrorContext(ctx, "calendar feed URL does not decrypt", "source", row.ID, "err", err)
		return failed(CodeInvalidCalendar, err)
	}
	sameWindow := row.WindowStart.Valid && dateOf(row.WindowStart) == w.From
	var etag, lastMod string
	if sameWindow { // a 304 is only enough when the stored window is still the right one
		etag, lastMod = deref(row.HttpEtag), deref(row.HttpLastModified)
	}
	got, err := f.Fetch(ctx, string(plain), etag, lastMod)
	if err != nil {
		return failed(CodeOf(err), err)
	}
	hash := ""
	if !got.NotModified {
		hash = Hash(got.Body)
	}
	if got.NotModified || sameWindow && hash == deref(row.FeedHash) {
		return fence.Tx(ctx, func(q *dbq.Queries) error {
			return q.FeedUnchanged(ctx, dbq.FeedUnchangedParams{ID: row.ID, Etag: nonEmpty(got.ETag), LastModified: nonEmpty(got.LastModified)})
		})
	}
	feed, err := Parse(got.Body, zone, w)
	if err != nil {
		return failed(CodeInvalidCalendar, err)
	}
	return fence.Tx(ctx, func(q *dbq.Queries) error {
		return store(ctx, q, row, feed, got, hash, w)
	})
}

// store replaces the source's series and events with the feed's, under the household lock (the
// order commands use) and the source row's.
func store(ctx context.Context, q *dbq.Queries, row dbq.ClaimDueFeedsRow, feed Feed, got Fetched, hash string, w Window) error {
	if _, err := q.LockHousehold(ctx, row.HouseholdID); err != nil {
		return err
	}
	if _, err := q.LockCalendarSource(ctx, dbq.LockCalendarSourceParams{HouseholdID: row.HouseholdID, ID: row.ID}); errors.Is(err, pgx.ErrNoRows) {
		return nil // removed meanwhile
	} else if err != nil {
		return err
	}
	uids := make([]string, len(feed.Series))
	recurring := make([]bool, len(feed.Series))
	for i, s := range feed.Series {
		uids[i], recurring[i] = s.UID, s.Recurring
	}
	series, err := q.UpsertSeries(ctx, dbq.UpsertSeriesParams{HouseholdID: row.HouseholdID, SourceID: row.ID, Uids: uids, Recurring: recurring})
	if err != nil {
		return err
	}
	ids := make(map[string]uuid.UUID, len(series))
	keep := make([]uuid.UUID, 0, len(series))
	for _, s := range series {
		ids[s.Uid] = s.ID
		keep = append(keep, s.ID)
	}
	if err := q.DeleteSeriesNotIn(ctx, dbq.DeleteSeriesNotInParams{SourceID: row.ID, Keep: keep}); err != nil {
		return err
	}
	p := dbq.UpsertEventsParams{HouseholdID: row.HouseholdID, SourceID: row.ID}
	for _, s := range feed.Series {
		for _, e := range s.Events {
			p.SeriesIds = append(p.SeriesIds, ids[s.UID])
			p.Keys = append(p.Keys, e.Key)
			p.AllDay = append(p.AllDay, e.AllDay)
			start, end, sd, ed := e.Start, e.End, "", ""
			if e.AllDay {
				start, end, sd, ed = time.Unix(0, 0), time.Unix(0, 0), e.StartDate.String(), e.EndDate.String()
			}
			p.StartsAt, p.EndsAt = append(p.StartsAt, start), append(p.EndsAt, end)
			p.StartDates, p.EndDates = append(p.StartDates, sd), append(p.EndDates, ed)
			p.Titles, p.Locations, p.Descriptions = append(p.Titles, e.Title), append(p.Locations, e.Location), append(p.Descriptions, e.Description)
			status := "confirmed"
			if e.Tentative {
				status = "tentative"
			}
			p.Statuses = append(p.Statuses, status)
		}
	}
	events, err := q.UpsertEvents(ctx, p)
	if err != nil {
		return err
	}
	if err := q.DeleteEventsNotIn(ctx, dbq.DeleteEventsNotInParams{SourceID: row.ID, Keep: events}); err != nil {
		return err
	}
	var code *string
	if feed.TooMany {
		c := CodeTooManyEvents
		code = &c
	}
	if err := q.FeedSynced(ctx, dbq.FeedSyncedParams{ID: row.ID, ErrorCode: code, Etag: nonEmpty(got.ETag),
		LastModified: nonEmpty(got.LastModified), FeedHash: &hash, WindowStart: pgDate(w.From)}); err != nil {
		return err
	}
	return live.Notify(ctx, q, row.HouseholdID, live.Event{Topic: live.Calendar})
}

func dateOf(d pgtype.Date) day.Date {
	return day.Date{Year: d.Time.Year(), Month: d.Time.Month(), Day: d.Time.Day()}
}

func pgDate(d day.Date) pgtype.Date {
	return pgtype.Date{Time: time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC), Valid: true}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
