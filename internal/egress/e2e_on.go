//go:build e2e

package egress

import "net/netip"

var (
	e2eV4 = netip.MustParseAddr("127.0.0.1")
	e2eV6 = netip.MustParseAddr("::1")
)

// e2eLoopback, in an e2e build only (go build -tags e2e, never a release), lets the calendar
// client and the single sign-on client reach exactly 127.0.0.1 and ::1, so the browser tests can
// serve them a fixture feed and a fake OIDC provider. Each client's ports still apply, and every
// other client and address is checked as usual.
func e2eLoopback(name string, a netip.Addr) bool {
	return (name == CalendarClient || name == OIDCClient) && (a == e2eV4 || a == e2eV6)
}
