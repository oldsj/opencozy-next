// Package egress is the only way OpenCozy makes outbound HTTP requests (design §10). It builds
// the one kind of http.Transport the app uses: names are resolved and every address checked in
// the dialer, so a DNS answer cannot change between the check and the connection; there is no
// proxy; redirects are refused unless a client opts in, and then each hop is checked again; and
// response bodies are capped after decompression.
package egress

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// The IPv4 ranges that are not global unicast (design §10). Everything outside them is public.
var notPublic4 = mustPrefixes(
	"0.0.0.0/8",       // "this network"
	"10.0.0.0/8",      // RFC 1918
	"100.64.0.0/10",   // shared address space (CGNAT)
	"127.0.0.0/8",     // loopback
	"169.254.0.0/16",  // link-local, including cloud metadata
	"172.16.0.0/12",   // RFC 1918
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // TEST-NET-1
	"192.88.99.0/24",  // 6to4 relay anycast
	"192.168.0.0/16",  // RFC 1918
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved, including broadcast
)

// IPv6 is public only inside 2000::/3, minus these. Every form that embeds an IPv4 address
// (::/96, ::ffff:0:0/96, ::ffff:0:0:0/96, 64:ff9b::/96) lies outside 2000::/3, except 6to4,
// which is listed.
var (
	global6    = netip.MustParsePrefix("2000::/3")
	notPublic6 = mustPrefixes(
		"2001::/23",     // IETF protocol assignments, including Teredo (2001::/32)
		"2001:db8::/32", // documentation
		"2002::/16",     // 6to4, which embeds an IPv4 address
		"3fff::/20",     // documentation
	)
)

// The only ranges a private allowlist may draw from.
var privateRanges = mustPrefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7")

func mustPrefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, p := range s {
		out[i] = netip.MustParsePrefix(p)
	}
	return out
}

// IsPublic reports whether addr is a public unicast address that any integration may reach.
// It takes the address as written: an IPv4-mapped IPv6 address is not public.
func IsPublic(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	if addr.Is4() {
		return !inAny(notPublic4, addr)
	}
	return global6.Contains(addr) && !inAny(notPublic6, addr)
}

func inAny(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ParsePrivateCIDRs parses and validates a private allowlist, a comma-separated list such as
// "192.168.40.0/24,fd00:40::/64". Each entry must be a canonical prefix (no host bits set)
// inside 10/8, 172.16/12, 192.168/16 or fc00::/7, so an allowlist can never open loopback,
// link-local (cloud metadata), CGNAT, multicast or a public range. An empty string is an empty
// list.
func ParsePrivateCIDRs(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		p, err := netip.ParsePrefix(field)
		if err != nil {
			return nil, fmt.Errorf("%q is not a CIDR prefix", field)
		}
		if err := checkPrivatePrefix(p); err != nil {
			return nil, fmt.Errorf("%q: %w", field, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func checkPrivatePrefix(p netip.Prefix) error {
	switch {
	case p.Addr().Zone() != "":
		return errors.New("zones are not allowed")
	case p.Addr().Is4In6():
		return errors.New("write IPv4 ranges in IPv4 form")
	case p != p.Masked():
		return fmt.Errorf("host bits are set; did you mean %s?", p.Masked())
	}
	for _, r := range privateRanges {
		// Inside r: same family, at least as long, and r contains its first address.
		if r.Addr().BitLen() == p.Addr().BitLen() && p.Bits() >= r.Bits() && r.Contains(p.Addr()) {
			return nil
		}
	}
	return errors.New("only ranges inside 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 or fc00::/7 are allowed")
}

// literalHost reports whether host is an address literal, and returns it. Besides the forms
// netip parses, it catches the legacy IPv4 spellings that some resolvers and libraries accept
// (an integer, hex, octal, or fewer than four parts, such as "2130706433", "0x7f.1" or
// "0177.0.0.1"); those come back with ok and an invalid address, so the caller refuses them.
func literalHost(host string) (addr netip.Addr, ok bool) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a, true
	}
	if legacyIPv4(host) {
		return netip.Addr{}, true
	}
	return netip.Addr{}, false
}

// legacyIPv4 reports whether host parses as an IPv4 address under inet_aton's rules: one to
// four dot-separated parts, each decimal, 0x-prefixed hex or 0-prefixed octal. A real DNS name
// never does, because its last label is never all digits or a 0x number.
func legacyIPv4(host string) bool {
	parts := strings.Split(strings.TrimSuffix(host, "."), ".")
	if len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		base, digits := 10, part
		switch {
		case len(part) > 1 && (part[:2] == "0x" || part[:2] == "0X"):
			base, digits = 16, part[2:]
		case len(part) > 1 && part[0] == '0':
			base, digits = 8, part[1:]
		}
		if digits == "" && base == 16 {
			return false
		}
		if digits == "" {
			continue // a lone "0x" fails above; "0" is decimal zero
		}
		if _, err := strconv.ParseUint(digits, base, 32); err != nil {
			return false
		}
	}
	return true
}

// PrivateCIDRs is a validated private allowlist, for internal/config to parse straight from
// the environment: a bad entry stops startup.
type PrivateCIDRs []netip.Prefix

// UnmarshalText parses with ParsePrivateCIDRs.
func (p *PrivateCIDRs) UnmarshalText(text []byte) error {
	parsed, err := ParsePrivateCIDRs(string(text))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
