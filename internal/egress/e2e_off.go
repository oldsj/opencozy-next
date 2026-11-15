//go:build !e2e

package egress

import "net/netip"

// e2eLoopback is always false outside an e2e build (e2e_on.go), so no setting can open
// loopback in the app that households run.
func e2eLoopback(string, netip.Addr) bool { return false }
