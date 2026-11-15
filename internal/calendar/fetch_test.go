package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/oldsj/opencozy-next/internal/egress"
)

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"webcal://cal.example.test/a.ics":           "https://cal.example.test/a.ics",
		" HTTPS://cal.example.test/a.ics?t=x#frag ": "https://cal.example.test/a.ics?t=x",
		"http://cal.example.test:8080/a":            "http://cal.example.test:8080/a",
	} {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "ftp://cal.example.test/a", "https://user:pw@cal.example.test/a", "https:///a",
		"file:///etc/passwd", "https://cal.example.test/a b", "mailto:a@example.test", "https://cal.example.test/a\nb"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// The egress client is tested in internal/egress; here a plain client checks the status mapping
// and the cache validators.
func TestFetchStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Mon, 14 Sep 2026 10:00:00 GMT")
			_, _ = w.Write([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/private":
			w.WriteHeader(http.StatusForbidden)
		case "/slow-down":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/broken":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()
	f := Fetcher{client: srv.Client()}
	got, err := f.Fetch(t.Context(), srv.URL+"/ok", "", "")
	if err != nil || got.ETag != `"v1"` || got.LastModified == "" || len(got.Body) == 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := f.Fetch(t.Context(), srv.URL+"/ok", `"v1"`, got.LastModified); err != nil || !got.NotModified || got.ETag != `"v1"` {
		t.Fatalf("conditional: %+v %v", got, err)
	}
	for path, code := range map[string]string{"/gone": CodeNotFound, "/private": CodeAccessDenied, "/slow-down": CodeRateLimited,
		"/broken": CodeTimeout, "/other": CodeInvalidCalendar} {
		if _, err := f.Fetch(t.Context(), srv.URL+path, "", ""); CodeOf(err) != code {
			t.Errorf("%s: %v, want %s", path, err, code)
		}
	}
}

// Go's client errors carry the request or redirect URL; a SyncError never does.
func TestSyncErrorHasNoURL(t *testing.T) {
	secret := "https://calendar.example.test/feed.ics?token=s3cr3t"
	for _, cause := range []error{
		fmt.Errorf("%w (ics): more than 5", egress.ErrRedirect),
		context.DeadlineExceeded,
		errors.New("connection reset by peer"),
	} {
		err := transportError(&url.Error{Op: "Get", URL: secret, Err: cause})
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%v", err)
		}
	}
	if got := Scrub("lookup calendar.example.test: no such host; GET /feed.ics?token=s3cr3t", secret); strings.Contains(got, "s3cr3t") || strings.Contains(got, "calendar.example.test") {
		t.Errorf("scrub: %s", got)
	}
}
