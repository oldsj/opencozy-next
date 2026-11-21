package tv

import "time"

// AllowanceCap is the most one router answer allows (design §3 rule 6): a router cut off from
// the server closes the TV within this time.
const AllowanceCap = 300 * time.Second

// EarlyCut is how long before a session ends the router cuts the TV's network: streaming apps
// keep playing from their buffer for a while after the network goes. Children and parents still
// see the real time left.
const EarlyCut = 60 * time.Second

// RouterSeconds is what the router is given for a session with remaining time left: capped, and
// cut early. Zero leaves the TV's MACs out.
func RouterSeconds(remaining time.Duration) int {
	left := remaining - EarlyCut
	if left <= 0 {
		return 0
	}
	return int(min(left, AllowanceCap) / time.Second)
}

// OpenSeconds is what the router is given for a TV that is open until closesAt: capped, with no
// early cut (nothing is being spent).
func OpenSeconds(closesAt, now time.Time) int {
	return int(min(closesAt.Sub(now), AllowanceCap) / time.Second)
}
