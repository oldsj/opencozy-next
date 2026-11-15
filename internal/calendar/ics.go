// Package calendar follows the calendars a household subscribes to (slice 13a: ICS feeds). A
// refresh fetches a feed through internal/egress, parses it here, expands recurrences inside a
// window around today, and replaces the source's stored series and events.
//
// Feeds are untrusted input. Parsing is bounded: the body is capped by the egress client, a
// recurrence is expanded within an iteration budget, an event count cap stops a huge feed, and
// a panic in the decoder is turned into ErrInvalid.
package calendar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	ical "github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"

	"github.com/oldsj/opencozy-next/internal/day"
)

// Limits on what one feed can produce.
const (
	// MaxEvents is the most events one source stores; past it the feed is marked too_many_events.
	MaxEvents = 5000
	// expandBudget is how many recurrence instances one series may step through, inside the
	// window or not, before it is cut off.
	expandBudget = 50000
	// feedBudget is how many recurrence instances a whole feed may step through: past it the
	// feed is refused (ErrTooComplex) and the stored copy is kept.
	feedBudget  = 500000
	maxTitle    = 500
	maxLocation = 500
	maxDesc     = 4000
	maxUID      = 1024
)

// WindowPast and WindowFuture bound the stored events around the household's today.
const (
	WindowPast   = 31
	WindowFuture = 366
)

var (
	// ErrInvalid means the body is not a calendar this package can read.
	ErrInvalid = errors.New("calendar: not a readable calendar")
	// ErrTooComplex means expanding the feed took more work than one feed may (feedBudget), or
	// ran out of time.
	ErrTooComplex = errors.New("calendar: feed too complex to expand")
)

// Feed is a parsed feed: every series it has (by UID), each with its events inside the window.
// A series with none in the window is still listed, so its row and assignments are kept.
type Feed struct {
	Series []Series
	// Skipped counts components that could not be read (no UID, a bad date, an unsupported rule).
	Skipped int
	// TooMany is set when the feed had more than MaxEvents events in the window.
	TooMany bool
}

// Series is one UID: a single event, or a recurring one with its instances.
type Series struct {
	UID       string
	Recurring bool
	Events    []Event
}

// Event is one stored occurrence.
type Event struct {
	// Key is "single", or the original start of a recurrence: YYYYMMDD for all-day,
	// YYYYMMDDTHHMMSSZ (UTC) otherwise.
	Key    string
	AllDay bool
	// Start and End for a timed event.
	Start, End time.Time
	// StartDate and EndDate (exclusive) for an all-day event.
	StartDate, EndDate day.Date
	Title, Location    string // "" when missing
	Description        string
	Tentative          bool
}

// Window is the span of days stored: [From, To).
type Window struct{ From, To day.Date }

// WindowAround is the stored window for a household whose today is t.
func WindowAround(today day.Date) Window {
	return Window{From: today.AddDays(-WindowPast), To: today.AddDays(WindowFuture)}
}

// Parse reads an iCalendar body and expands it within w, in the household zone (used for
// floating times and unknown TZIDs).
// ctx bounds the expansion: when it ends, Parse returns ErrTooComplex.
func Parse(ctx context.Context, body []byte, zone *time.Location, w Window) (feed Feed, err error) {
	defer func() {
		if r := recover(); r != nil {
			feed, err = Feed{}, ErrInvalid
		}
	}()
	if !bytes.Contains(body[:min(len(body), 4096)], []byte("BEGIN:VCALENDAR")) {
		return Feed{}, ErrInvalid
	}
	var comps []*ical.Component
	dec := ical.NewDecoder(bytes.NewReader(body))
	for {
		cal, err := dec.Decode()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if len(comps) == 0 {
				return Feed{}, ErrInvalid
			}
			break // keep what was read before the damage
		}
		for _, c := range cal.Children {
			if c.Name == ical.CompEvent {
				comps = append(comps, c)
			}
		}
	}
	p := parser{ctx: ctx, zone: zone, w: w, winStart: w.From.Start(zone), winEnd: w.To.Start(zone), budget: feedBudget}
	feed = p.build(comps)
	if p.exhausted {
		return Feed{}, ErrTooComplex
	}
	return feed, nil
}

type parser struct {
	ctx              context.Context
	budget           int  // recurrence steps left for the whole feed
	exhausted        bool // the budget or ctx ran out
	zone             *time.Location
	w                Window
	winStart, winEnd time.Time
	skipped          int
	count            int
	tooMany          bool
}

// vevent is one VEVENT read into plain values.
type vevent struct {
	uid       string
	recurID   string // the key it overrides, or ""
	allDay    bool
	start     time.Time // for all-day: midnight UTC of the date
	dur       time.Duration
	days      int // all-day length
	title     string
	location  string
	desc      string
	tentative bool
	cancelled bool
	comp      *ical.Component
}

func (p *parser) build(comps []*ical.Component) Feed {
	masters := map[string]*vevent{}
	overrides := map[string]map[string]*vevent{}
	var order []string
	for _, c := range comps {
		v, ok := p.read(c)
		if !ok {
			p.skipped++
			continue
		}
		if v.recurID != "" {
			if overrides[v.uid] == nil {
				overrides[v.uid] = map[string]*vevent{}
			}
			overrides[v.uid][v.recurID] = v
			if masters[v.uid] == nil && !contains(order, v.uid) {
				order = append(order, v.uid)
			}
			continue
		}
		if masters[v.uid] != nil {
			p.skipped++ // a repeated UID: the first wins
			continue
		}
		masters[v.uid] = v
		if !contains(order, v.uid) {
			order = append(order, v.uid)
		}
	}
	var feed Feed
	for _, uid := range order {
		if p.exhausted {
			return Feed{}
		}
		feed.Series = append(feed.Series, p.series(uid, masters[uid], overrides[uid]))
	}
	feed.Skipped, feed.TooMany = p.skipped, p.tooMany
	return feed
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func (p *parser) series(uid string, m *vevent, over map[string]*vevent) Series {
	s := Series{UID: storedUID(uid)}
	seen := map[string]bool{}
	add := func(v *vevent, key string, start time.Time) {
		seen[key] = true
		if v.cancelled {
			return
		}
		if e, ok := p.event(v, key, start); ok {
			s.Events = append(s.Events, e)
		}
	}
	if m != nil && m.cancelled {
		return s
	}
	if m != nil && m.comp.Props.Get(ical.PropRecurrenceRule) == nil && len(m.comp.Props[ical.PropRecurrenceDates]) == 0 {
		add(m, "single", m.start)
	} else if m != nil {
		s.Recurring = true
		starts, ok := p.expand(m)
		if !ok {
			p.skipped++
			return s // an unsupported rule: listed, so its assignments stay, with no events
		}
		for _, t := range starts {
			key := keyOf(t, m.allDay)
			if o := over[key]; o != nil {
				add(o, key, o.start)
			} else {
				add(m, key, t)
			}
		}
	}
	// Overrides the expansion did not reach (or with no master) still count on their own.
	keys := make([]string, 0, len(over))
	for k := range over {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !seen[k] {
			s.Recurring = true
			add(over[k], k, over[k].start)
		}
	}
	sort.Slice(s.Events, func(i, j int) bool { return s.Events[i].Key < s.Events[j].Key })
	return s
}

// event places v at start, if it overlaps the window and the cap allows.
func (p *parser) event(v *vevent, key string, start time.Time) (Event, bool) {
	e := Event{Key: key, AllDay: v.allDay, Title: v.title, Location: v.location, Description: v.desc, Tentative: v.tentative}
	if v.allDay {
		e.StartDate = day.Date{Year: start.Year(), Month: start.Month(), Day: start.Day()}
		e.EndDate = e.StartDate.AddDays(max(v.days, 1))
		if e.StartDate.Compare(p.w.To) >= 0 || e.EndDate.Compare(p.w.From) <= 0 {
			return Event{}, false
		}
	} else {
		e.Start, e.End = start, start.Add(v.dur)
		// Overlaps [winStart, winEnd); a zero-length event counts at its instant.
		if !e.Start.Before(p.winEnd) || !e.End.After(p.winStart) && (e.End.After(e.Start) || e.Start.Before(p.winStart)) {
			return Event{}, false
		}
	}
	if p.count >= MaxEvents {
		p.tooMany = true
		return Event{}, false
	}
	p.count++
	return e, true
}

// expand lists the instance starts of a recurring master that can overlap the window.
func (p *parser) expand(m *vevent) ([]time.Time, bool) {
	set := &rrule.Set{}
	if rp := m.comp.Props.Get(ical.PropRecurrenceRule); rp != nil {
		opt, err := rrule.StrToROption(rp.Value)
		if err != nil || opt.Freq == rrule.SECONDLY || opt.Freq == rrule.MINUTELY {
			return nil, false
		}
		opt.Dtstart = m.start
		if !opt.Until.IsZero() && m.allDay {
			// A date UNTIL is inclusive of that whole day.
			opt.Until = time.Date(opt.Until.Year(), opt.Until.Month(), opt.Until.Day(), 23, 59, 59, 0, time.UTC)
		}
		r, err := rrule.NewRRule(*opt)
		if err != nil {
			return nil, false
		}
		set.RRule(r)
	}
	set.DTStart(m.start)
	if m.comp.Props.Get(ical.PropRecurrenceRule) == nil {
		set.RDate(m.start) // RDATE alone: DTSTART is the first instance
	}
	for _, t := range p.times(m.comp.Props[ical.PropRecurrenceDates], m.allDay) {
		set.RDate(t)
	}
	for _, t := range p.times(m.comp.Props[ical.PropExceptionDates], m.allDay) {
		set.ExDate(t)
	}
	// An instance can start before the window and still overlap it.
	reach := m.dur + time.Duration(m.days)*24*time.Hour + 48*time.Hour
	var out []time.Time
	next := set.Iterator()
	for i := 0; i < expandBudget; i++ {
		if p.budget--; p.budget < 0 || i%1024 == 0 && p.ctx.Err() != nil {
			p.exhausted = true
			return nil, true
		}
		t, ok := next()
		if !ok || !t.Before(p.winEnd.Add(48*time.Hour)) {
			return out, true
		}
		if t.Add(reach).After(p.winStart) {
			out = append(out, t)
			if len(out) > MaxEvents {
				return out, true
			}
		}
	}
	return out, true // budget spent: keep what was reached
}

// times reads RDATE or EXDATE values, which may be comma-separated lists. PERIOD values are
// skipped.
func (p *parser) times(props []ical.Prop, allDay bool) []time.Time {
	var out []time.Time
	for _, prop := range props {
		if strings.EqualFold(prop.Params.Get(ical.ParamValue), "PERIOD") {
			continue
		}
		for _, v := range strings.Split(prop.Value, ",") {
			t, isDate, err := p.parseTime(strings.TrimSpace(v), prop.Params.Get(ical.ParamTimezoneID), prop.Params.Get(ical.ParamValue))
			if err != nil {
				continue
			}
			if allDay && !isDate {
				t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			}
			out = append(out, t)
		}
	}
	return out
}

func (p *parser) read(c *ical.Component) (*vevent, bool) {
	v := &vevent{comp: c}
	uid := c.Props.Get(ical.PropUID)
	if uid == nil || strings.TrimSpace(uid.Value) == "" {
		return nil, false
	}
	v.uid = strings.TrimSpace(uid.Value)
	sp := c.Props.Get(ical.PropDateTimeStart)
	if sp == nil {
		return nil, false
	}
	start, isDate, err := p.parseTime(sp.Value, sp.Params.Get(ical.ParamTimezoneID), sp.Params.Get(ical.ParamValue))
	if err != nil {
		return nil, false
	}
	v.allDay, v.start = isDate, start
	if ep := c.Props.Get(ical.PropDateTimeEnd); ep != nil {
		end, endDate, err := p.parseTime(ep.Value, ep.Params.Get(ical.ParamTimezoneID), ep.Params.Get(ical.ParamValue))
		if err != nil {
			return nil, false
		}
		if isDate {
			if endDate {
				v.days = int(end.Sub(start).Hours() / 24)
			}
		} else {
			v.dur = end.Sub(start)
		}
	} else if dp := c.Props.Get(ical.PropDuration); dp != nil {
		d, err := dp.Duration()
		if err != nil {
			return nil, false
		}
		if isDate {
			v.days = int(d.Hours() / 24)
		} else {
			v.dur = d
		}
	}
	if isDate && v.days < 1 {
		v.days = 1
	}
	if v.dur < 0 {
		v.dur = 0
	}
	if v.dur > 366*24*time.Hour || v.days > 366 {
		return nil, false
	}
	if rp := c.Props.Get(ical.PropRecurrenceID); rp != nil {
		t, d, err := p.parseTime(rp.Value, rp.Params.Get(ical.ParamTimezoneID), rp.Params.Get(ical.ParamValue))
		if err != nil {
			return nil, false
		}
		v.recurID = keyOf(t, d)
	}
	v.title = text(c, ical.PropSummary, maxTitle, false)
	v.location = text(c, ical.PropLocation, maxLocation, false)
	v.desc = text(c, ical.PropDescription, maxDesc, true)
	if st := c.Props.Get(ical.PropStatus); st != nil {
		switch strings.ToUpper(strings.TrimSpace(st.Value)) {
		case "CANCELLED":
			v.cancelled = true
		case "TENTATIVE":
			v.tentative = true
		}
	}
	return v, true
}

// parseTime reads a DATE or DATE-TIME. Dates come back as midnight UTC. A local time uses its
// TZID when Go knows it, else the household zone.
func (p *parser) parseTime(value, tzid, valueType string) (time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(valueType, "DATE") || len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, time.UTC)
		return t, true, err
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC)
		return t, false, err
	}
	t, err := time.ParseInLocation("20060102T150405", value, zoneFor(tzid, p.zone))
	return t, false, err
}

// zoneFor resolves a TZID: an IANA name, or one with a vendor prefix ("/mozilla.org/…/Europe/
// Paris"). Anything else (a Windows zone name) falls back to the household zone.
func zoneFor(tzid string, fallback *time.Location) *time.Location {
	tzid = strings.Trim(strings.TrimSpace(tzid), `"`)
	if tzid == "" || len(tzid) > 200 {
		return fallback
	}
	parts := strings.Split(strings.Trim(tzid, "/"), "/")
	for i := range parts {
		name := strings.Join(parts[i:], "/")
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		if loc, err := day.Zone(name); err == nil {
			return loc
		}
	}
	return fallback
}

func keyOf(t time.Time, allDay bool) string {
	if allDay {
		return t.Format("20060102")
	}
	return t.UTC().Format("20060102T150405Z")
}

// storedUID keeps a UID that fits, and hashes a longer one.
func storedUID(uid string) string {
	if len(uid) <= maxUID {
		return uid
	}
	sum := sha256.Sum256([]byte(uid))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// text reads a TEXT property: control characters dropped (newlines kept when multiline),
// trimmed, cut to max characters. "" when missing.
func text(c *ical.Component, name string, max int, multiline bool) string {
	s, err := c.Props.Text(name)
	if err != nil || s == "" {
		return ""
	}
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' && multiline:
			return r
		case r == '\n', r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		s = strings.TrimSpace(string([]rune(s)[:max]))
	}
	return s
}

// Hash is the feed body's identity, to skip rebuilding an unchanged feed.
func Hash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (e Event) String() string {
	if e.AllDay {
		return fmt.Sprintf("%s %s..%s %q", e.Key, e.StartDate, e.EndDate, e.Title)
	}
	return fmt.Sprintf("%s %s..%s %q", e.Key, e.Start.Format(time.RFC3339), e.End.Format(time.RFC3339), e.Title)
}
