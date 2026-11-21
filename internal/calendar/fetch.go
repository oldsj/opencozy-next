package calendar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oldsj/opencozy-next/internal/egress"
)

// Sync error codes, shown to adults as one sentence each (the web's calendarErrorText).
const (
	CodeTimeout         = "timeout"
	CodeNotFound        = "not_found"
	CodeAccessDenied    = "access_denied"
	CodeTooLarge        = "too_large"
	CodeInvalidCalendar = "invalid_calendar"
	CodeTooManyEvents   = "too_many_events"
	CodeRateLimited     = "rate_limited"
)

// Feed fetch limits.
const (
	MaxFeedBytes  = 10 << 20
	fetchTimeout  = 30 * time.Second
	maxRedirects  = 5
	maxURLLength  = 2048
	maxHeaderSave = 200
)

// SyncError is a failed refresh, with the code adults see.
type SyncError struct {
	Code string
	err  error
}

func (e *SyncError) Error() string { return "calendar: " + e.Code + ": " + e.err.Error() }
func (e *SyncError) Unwrap() error { return e.err }

// fail wraps a refresh error. The cause is unwrapped from any *url.Error first: Go's HTTP client
// puts the full request (or redirect) URL, a feed's private token included, in that error's text.
func fail(code string, err error) error { return &SyncError{Code: code, err: withoutURL(err)} }

func withoutURL(err error) error {
	for {
		ue, ok := errors.AsType[*url.Error](err)
		if !ok {
			return err
		}
		if ue.Err == nil {
			return errors.New(ue.Op + " failed")
		}
		err = ue.Err
	}
}

// Scrub removes a feed's URL, and its host, path and query, from s: a last guard before an error
// about that feed is logged.
func Scrub(s, feedURL string) string {
	parts := []string{feedURL}
	if u, err := url.Parse(feedURL); err == nil {
		parts = append(parts, u.Host, u.Hostname(), u.EscapedPath(), u.RawQuery, u.Path)
	}
	for _, p := range parts {
		if len(p) > 1 {
			s = strings.ReplaceAll(s, p, "[feed]")
		}
	}
	return s
}

// CodeOf is the adult-facing code for a refresh error.
func CodeOf(err error) string {
	if se, ok := errors.AsType[*SyncError](err); ok {
		return se.Code
	}
	return CodeInvalidCalendar
}

// ErrBadURL means a feed URL an adult entered is not one this app fetches.
var ErrBadURL = errors.New("calendar: not a calendar link")

// NormalizeURL checks a feed URL an adult entered: http, https or webcal (fetched as https),
// a host, no user name or password, no fragment, at most 2048 bytes.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLLength || strings.ContainsFunc(raw, func(r rune) bool { return r < 0x21 || r == 0x7f }) {
		return "", ErrBadURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" {
		return "", ErrBadURL
	}
	switch strings.ToLower(u.Scheme) {
	case "webcal", "webcals":
		u.Scheme = "https"
	case "http", "https":
		u.Scheme = strings.ToLower(u.Scheme)
	default:
		return "", ErrBadURL
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), nil
}

// doer is an HTTP client: in production always the egress one from NewFetcher.
type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Fetcher fetches feeds through an egress client, and Google calendars when WithGoogle set one.
type Fetcher struct {
	client doer
	google *Google
}

// WithGoogle returns f, which then also refreshes Google calendars through g.
func (f Fetcher) WithGoogle(g *Google) Fetcher {
	f.google = g
	return f
}

// NewFetcher builds the "ics" egress client: public addresses only, feed ports, 30 s, 10 MiB,
// up to 5 redirects (each through the same checks).
func NewFetcher() (Fetcher, error) {
	c, err := egress.New(egress.Options{
		Name: egress.CalendarClient, Ports: egress.FeedPorts, Timeout: fetchTimeout,
		MaxResponseBytes: MaxFeedBytes, MaxRedirects: maxRedirects,
	})
	return Fetcher{client: c}, err
}

// Fetched is a fetch's outcome. NotModified means the cached copy is current.
type Fetched struct {
	NotModified        bool
	Body               []byte
	ETag, LastModified string
}

// Fetch GETs a feed, conditionally on the cached validators. Errors are *SyncError.
func (f Fetcher) Fetch(ctx context.Context, feedURL, etag, lastModified string) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return Fetched{}, fail(CodeInvalidCalendar, err)
	}
	req.Header.Set("Accept", "text/calendar, */*;q=0.5")
	req.Header.Set("User-Agent", "OpenCozy-calendar/1")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return Fetched{}, transportError(err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only body
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return Fetched{NotModified: true, ETag: etag, LastModified: lastModified}, nil
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return Fetched{}, fail(CodeNotFound, fmt.Errorf("status %d", resp.StatusCode))
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return Fetched{}, fail(CodeAccessDenied, fmt.Errorf("status %d", resp.StatusCode))
	case resp.StatusCode == http.StatusTooManyRequests:
		return Fetched{}, fail(CodeRateLimited, fmt.Errorf("status %d", resp.StatusCode))
	case resp.StatusCode >= 500:
		return Fetched{}, fail(CodeTimeout, fmt.Errorf("status %d", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return Fetched{}, fail(CodeInvalidCalendar, fmt.Errorf("status %d", resp.StatusCode))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Fetched{}, transportError(err)
	}
	return Fetched{Body: body, ETag: header(resp, "ETag"), LastModified: header(resp, "Last-Modified")}, nil
}

// header keeps a cache validator only when it is short and printable.
func header(resp *http.Response, name string) string {
	v := resp.Header.Get(name)
	if v == "" || len(v) > maxHeaderSave || strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r > 0x7e }) {
		return ""
	}
	return v
}

func transportError(err error) error {
	var dns *net.DNSError
	var ne net.Error
	switch {
	case errors.Is(err, egress.ErrTooLarge):
		return fail(CodeTooLarge, err)
	case errors.Is(err, egress.ErrRefused), errors.Is(err, egress.ErrRedirect):
		return fail(CodeInvalidCalendar, err)
	case errors.As(err, &dns) && dns.IsNotFound:
		return fail(CodeNotFound, err)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return fail(CodeTimeout, err)
	}
	return fail(CodeTimeout, err) // connection refused or reset: worth retrying later
}
