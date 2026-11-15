//go:build !e2e

package egress

import (
	"errors"
	"testing"
	"time"
)

// The calendar client of the app households run never reaches loopback, on any feed port. In an
// e2e build, e2e_on_test.go checks the narrow allowance instead.
func TestCalendarClientRefusesLoopback(t *testing.T) {
	c, err := New(Options{Name: CalendarClient, Ports: FeedPorts, Timeout: 5 * time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "[::1]", "localhost"} {
		for _, port := range []string{"80", "8080", "8443"} {
			if err := try(t, c, "http://"+host+":"+port+"/"); !errors.Is(err, ErrRefused) {
				t.Errorf("%s:%s: err = %v, want ErrRefused", host, port, err)
			}
		}
	}
}
