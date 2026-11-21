package importer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/calendar"
)

// ErrNoKeys means there are calendar links to import but no keyring to seal them with.
var ErrNoKeys = errors.New("import: calendar links are stored encrypted; set OPENCOZY_SECRET_KEYS")

// importColors are given to imported calendars in turn (the old app had no calendar colours).
var importColors = []string{"sky", "sage", "peach", "lilac", "butter", "teal", "rose", "sand"}

// importCalendars carries ICS calendar links with their IDs, names, display choice and version;
// the link is normalized as the app does and sealed with the keyring. Sync state is not carried:
// the refresh job fetches each feed on its first run and rebuilds the events. Series keep their
// IDs (a series the old app had marked removed keeps its removal time, so it is pruned after 30
// days as usual), and so do active assignments with their children. Google calendars are
// carried with their accounts (importGoogleConnections): the calendar's Google ID, no sync token,
// so the first refresh fetches the whole window.
func importCalendars(ctx context.Context, x *run) error {
	googleCarried, err := importGoogleConnections(ctx, x)
	if err != nil {
		return err
	}
	x.r.columnsRead("calendar_source", "household_id", "id", "provider", "name", "feed_url", "show_on_display", "version", "created_at",
		"google_connection_id", "google_calendar_id",
		// Sync state, rebuilt by the first refresh:
		"sync_generation", "last_attempt_at", "last_success_at", "last_changed_at", "last_error_at", "last_error_code",
		"consecutive_failures", "skipped_components", "refresh_lease_id", "refresh_lease_until", "http_etag", "http_last_modified",
		"feed_hash", "applied_window_key", "google_sync_token", "google_window_start_at", "google_window_end_at")
	rows, err := x.src.Query(ctx, `SELECT household_id, id, provider, name, feed_url, show_on_display, version, created_at,
		google_connection_id, google_calendar_id
		FROM opencozy.calendar_source ORDER BY household_id, created_at, id`)
	if err != nil {
		return safe(err)
	}
	type source struct {
		household, id uuid.UUID
		provider      string
		name          string
		feedURL       *string
		show          bool
		version       int32
		created       time.Time
		googleConn    *uuid.UUID
		googleID      *string
	}
	var all []source
	for rows.Next() {
		var s source
		if err := rows.Scan(&s.household, &s.id, &s.provider, &s.name, &s.feedURL, &s.show, &s.version, &s.created, &s.googleConn, &s.googleID); err != nil {
			return safe(err)
		}
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	carried := map[uuid.UUID]bool{}
	// A household can't have the same link twice. The old app could (two spellings of one
	// address): the first is carried and the others are left out, since its events are theirs.
	type householdLink struct {
		household uuid.UUID
		link      string
	}
	seen := map[householdLink]bool{}
	duplicate := map[uuid.UUID]bool{}
	var n, orphans, dupes int64
	colorIndex := map[uuid.UUID]int{}
	nextColor := func(h uuid.UUID) string {
		c := importColors[colorIndex[h]%len(importColors)]
		colorIndex[h]++
		return c
	}
	for _, s := range all {
		if s.provider == "google" {
			// The old app had no foreign key from a calendar to its account.
			if s.googleConn == nil || s.googleID == nil || !googleCarried[*s.googleConn] {
				orphans++
				continue
			}
			if _, err := x.dst.Exec(ctx, `INSERT INTO calendar_source (household_id, id, provider, name, color, show_on_display,
				google_connection_id, google_calendar_id, version, created_at) VALUES ($1, $2, 'google', $3, $4, $5, $6, $7, $8, $9)`,
				s.household, s.id, s.name, nextColor(s.household), s.show, *s.googleConn, *s.googleID, s.version, s.created); err != nil {
				return fmt.Errorf("import: calendar source %s: %w", s.id, safe(err))
			}
			carried[s.id] = true
			n++
			continue
		}
		if s.feedURL == nil {
			return fmt.Errorf("import: calendar source %s has no link", s.id)
		}
		if x.keys == nil {
			return ErrNoKeys
		}
		// Never echo the link: it often carries a private token.
		link, err := calendar.NormalizeURL(*s.feedURL)
		if err != nil {
			return fmt.Errorf("import: calendar source %s has a link the app would not accept", s.id)
		}
		if seen[householdLink{s.household, link}] {
			duplicate[s.id] = true
			dupes++
			continue
		}
		seen[householdLink{s.household, link}] = true
		sealed, mac, err := calendar.SealFeedURL(x.keys, s.household, s.id, link)
		if err != nil {
			return err
		}
		color := nextColor(s.household)
		if _, err := x.dst.Exec(ctx, `INSERT INTO calendar_source (household_id, id, provider, name, color, show_on_display, feed_url, feed_url_mac, version, created_at)
			VALUES ($1, $2, 'ics', $3, $4, $5, $6, $7, $8, $9)`, s.household, s.id, s.name, color, s.show, sealed, mac, s.version, s.created); err != nil {
			return fmt.Errorf("import: calendar source %s: %w", s.id, safe(err))
		}
		carried[s.id] = true
		n++
	}
	if err := reconcileKind(ctx, x, "calendar_source", n, "calendar_source", 0, ""); err != nil {
		return err
	}
	if orphans > 0 {
		x.r.note("calendar_source", fmt.Sprintf("%d Google calendars left out: their account was gone", orphans))
	}
	if dupes > 0 {
		x.r.note("calendar_source", fmt.Sprintf("%d left out: the same link as another calendar of the household (sign-off: check which name to keep)", dupes))
	}
	if n > 0 {
		x.r.note("calendar_source", "colours assigned in turn; the old app had none (sign-off)")
	}

	// Series of carried sources.
	x.r.columnsRead("calendar_series", "household_id", "id", "source_id", "provider_uid", "recurring", "removed_at",
		// Rebuilt from the feed:
		"summary", "all_day", "time_zone", "status", "revision", "content_hash", "first_seen_at", "updated_at")
	rows, err = x.src.Query(ctx, `SELECT household_id, id, source_id, provider_uid, recurring, removed_at
		FROM opencozy.calendar_series ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	type series struct {
		household, id, source uuid.UUID
		uid                   string
		recurring             bool
		removed               *time.Time
	}
	var allSeries []series
	for rows.Next() {
		var s series
		if err := rows.Scan(&s.household, &s.id, &s.source, &s.uid, &s.recurring, &s.removed); err != nil {
			return safe(err)
		}
		allSeries = append(allSeries, s)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	seriesCarried := map[uuid.UUID]bool{}
	seriesOfDuplicate := map[uuid.UUID]bool{}
	var ns, orphanSeries, dupSeries int64
	for _, s := range allSeries {
		if duplicate[s.source] || !carried[s.source] {
			seriesOfDuplicate[s.id] = true
			if duplicate[s.source] {
				dupSeries++
			} else {
				orphanSeries++
			}
			continue
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO calendar_series (household_id, id, source_id, uid, recurring, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, COALESCE($6, now()))`, s.household, s.id, s.source, s.uid, s.recurring, s.removed); err != nil {
			return safe(err)
		}
		seriesCarried[s.id] = true
		ns++
	}
	if err := reconcileKind(ctx, x, "calendar_series", ns, "calendar_series", 0, ""); err != nil {
		return err
	}
	if orphanSeries > 0 {
		x.r.note("calendar_series", fmt.Sprintf("%d left out with their Google calendars", orphanSeries))
	}
	if dupSeries > 0 {
		x.r.note("calendar_series", fmt.Sprintf("%d left out with their duplicate links", dupSeries))
	}

	// Active assignments of carried series: the series' own (no occurrence key) or one
	// occurrence's. An inactive one counted as absent in the old app, so it is not carried.
	x.r.columnsRead("calendar_assignment", "household_id", "id", "series_id", "occurrence_key", "active",
		// History of the old app's edits, not carried:
		"version", "cleared_from_version", "created_at", "updated_at")
	rows, err = x.src.Query(ctx, `SELECT household_id, id, series_id, occurrence_key, active FROM opencozy.calendar_assignment ORDER BY household_id, id`)
	if err != nil {
		return safe(err)
	}
	type assignment struct {
		household, id, series uuid.UUID
		key                   *string
		active                bool
	}
	var allAssignments []assignment
	for rows.Next() {
		var a assignment
		if err := rows.Scan(&a.household, &a.id, &a.series, &a.key, &a.active); err != nil {
			return safe(err)
		}
		allAssignments = append(allAssignments, a)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	assignCarried := map[uuid.UUID]bool{}
	var na, inactive, assignDup int64
	for _, a := range allAssignments {
		switch {
		case seriesOfDuplicate[a.series], !seriesCarried[a.series]:
			assignDup++
			continue
		case !a.active:
			inactive++
			continue
		}
		key := "*"
		if a.key != nil {
			key = *a.key
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO calendar_assignment (household_id, id, series_id, recurrence_key) VALUES ($1, $2, $3, $4)`,
			a.household, a.id, a.series, key); err != nil {
			return safe(err)
		}
		assignCarried[a.id] = true
		na++
	}
	src, err := count(ctx, x.src, "calendar_assignment")
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, "calendar_assignment")
	if err != nil {
		return err
	}
	x.r.table("calendar_assignment", src, "calendar_assignment", na, got)
	x.r.check("calendar_assignment rows read", src, na+inactive+assignDup)
	if inactive > 0 {
		x.r.note("calendar_assignment", fmt.Sprintf("%d cleared ones left out (the old app ignored them)", inactive))
	}
	if assignDup > 0 {
		x.r.note("calendar_assignment", fmt.Sprintf("%d left out with their duplicate links or missing Google accounts (sign-off: who those events were for)", assignDup))
	}

	x.r.columnsRead("calendar_assignment_person", "household_id", "assignment_id", "child_id")
	rows, err = x.src.Query(ctx, `SELECT household_id, assignment_id, child_id FROM opencozy.calendar_assignment_person ORDER BY assignment_id, child_id`)
	if err != nil {
		return safe(err)
	}
	type person struct{ household, assignment, child uuid.UUID }
	var people []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.household, &p.assignment, &p.child); err != nil {
			return safe(err)
		}
		people = append(people, p)
	}
	if err := rows.Err(); err != nil {
		return safe(err)
	}
	var np, dropped int64
	for _, p := range people {
		if !assignCarried[p.assignment] {
			dropped++
			continue
		}
		if _, err := x.dst.Exec(ctx, `INSERT INTO calendar_assignment_child (household_id, assignment_id, child_id) VALUES ($1, $2, $3)`,
			p.household, p.assignment, p.child); err != nil {
			return safe(err)
		}
		np++
	}
	if err := reconcileKind(ctx, x, "calendar_assignment_person", np, "calendar_assignment_child", 0, ""); err != nil {
		return err
	}
	if dropped > 0 {
		x.r.note("calendar_assignment_person", fmt.Sprintf("%d rows of assignments left out", dropped))
	}
	return nil
}

// reconcileKind records an old table carried in part: rows read = carried + waiting (pending,
// with why) + rows of parents not carried (counted by the caller's notes).
func reconcileKind(ctx context.Context, x *run, old string, carried int64, target string, waiting int64, why string) error {
	src, err := count(ctx, x.src, old)
	if err != nil {
		return err
	}
	got, err := countNew(ctx, x.dst, target)
	if err != nil {
		return err
	}
	x.r.table(old, src, target, carried, got)
	if waiting > 0 {
		x.r.pending(old, fmt.Sprintf("%d %s", waiting, why))
	}
	return nil
}
