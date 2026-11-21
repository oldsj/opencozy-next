//go:build e2e

package egress

import (
	"errors"
	"net/netip"
	"testing"
	"time"
)

// An e2e build opens exactly 127.0.0.1 and ::1, to the calendar and single sign-on clients only,
// on their own ports only.
func TestE2EBuildOpensOnlyCalendarAndOIDCLoopback(t *testing.T) {
	for _, tc := range []struct {
		name, addr string
		want       bool
	}{
		{CalendarClient, "127.0.0.1", true},
		{CalendarClient, "::1", true},
		{CalendarClient, "127.0.0.2", false},
		{CalendarClient, "::ffff:127.0.0.1", false},
		{CalendarClient, "10.0.0.1", false},
		{CalendarClient, "169.254.169.254", false},
		{"home-assistant", "127.0.0.1", false},
		{OIDCClient, "127.0.0.1", true},
		{OIDCClient, "::1", true},
		{OIDCClient, "127.0.0.2", false},
		{OIDCClient, "10.0.0.1", false},
	} {
		if got := e2eLoopback(tc.name, netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("%s %s: %v, want %v", tc.name, tc.addr, got, tc.want)
		}
	}
	c, err := New(Options{Name: CalendarClient, Ports: FeedPorts, Timeout: 5 * time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if err := try(t, c, "http://127.0.0.1:9999/"); !errors.Is(err, ErrRefused) {
		t.Errorf("a port that isn't a feed port: err = %v, want ErrRefused", err)
	}
	other, err := New(Options{Name: "home-assistant", Ports: FeedPorts, Timeout: 5 * time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if err := try(t, other, "http://127.0.0.1:8080/"); !errors.Is(err, ErrRefused) {
		t.Errorf("another client: err = %v, want ErrRefused", err)
	}
}
