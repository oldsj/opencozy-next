package egress

import (
	"net/netip"
	"strings"
	"testing"
)

func TestIsPublic(t *testing.T) {
	refused := []string{
		// IPv4 special ranges, first and last address of each.
		"0.0.0.0", "0.255.255.255", "10.0.0.1", "10.255.255.255", "100.64.0.1", "100.127.255.255",
		"127.0.0.1", "127.255.255.254", "169.254.169.254", "172.16.0.1", "172.31.255.255",
		"192.0.0.1", "192.0.2.1", "192.88.99.1", "192.168.1.1", "198.18.0.1", "198.19.255.255",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "239.255.255.255", "240.0.0.1", "255.255.255.255",
		// IPv6 outside 2000::/3.
		"::", "::1", "fe80::1", "fc00::1", "fd12:3456::1", "ff02::1",
		// Every form that embeds an IPv4 address (design §10).
		"::7f00:1", "::a9fe:a9fe", "::ffff:7f00:1", "::ffff:a9fe:a9fe", "::ffff:0:a9fe:a9fe",
		"0:0:0:0:0:ffff:a00:1", "::ffff:8.8.8.8", "64:ff9b::a9fe:a9fe", "64:ff9b::808:808",
		"64:ff9b:1::a00:1", "2002:a9fe:a9fe::1", "2002:808:808::1",
		// Excluded parts of 2000::/3.
		"2001::1", "2001:0:4136:e378::1", "2001:1ff:ffff::1", "2001:db8::1", "3fff::1", "3fff:fff::1",
		// A zone is never public.
		"2606:4700::1111%eth0",
	}
	allowed := []string{
		"1.1.1.1", "8.8.8.8", "9.255.255.255", "11.0.0.1", "100.63.255.255", "100.128.0.1",
		"169.253.255.255", "172.15.255.255", "172.32.0.1", "192.0.1.1", "192.88.98.1", "192.169.0.1",
		"198.17.255.255", "198.20.0.1", "223.255.255.255",
		"2606:4700::1111", "2001:200::1", "2001:4860:4860::8888", "2003::1", "3ffe::1", "3fff:1000::1",
	}
	for _, s := range refused {
		if IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("IsPublic(%s) = true, want false", s)
		}
	}
	for _, s := range allowed {
		if !IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("IsPublic(%s) = false, want true", s)
		}
	}
	if IsPublic(netip.Addr{}) {
		t.Error("the zero Addr is public")
	}
}

func TestLiteralHost(t *testing.T) {
	// Legacy IPv4 spellings: refused as literals, never resolved.
	for _, host := range []string{
		"2130706433", "0x7f000001", "0x7f.1", "0177.0.0.1", "017700000001", "127.1", "127.0.1",
		"0xa9.0xfe.0xa9.0xfe", "0251.0376.0251.0376", "2852039166", "10.1", "0", "0x0", "127.0.0.1.",
	} {
		a, ok := literalHost(host)
		if !ok || (a.IsValid() && IsPublic(a)) {
			t.Errorf("literalHost(%q) = %v, %v; want refused", host, a, ok)
		}
	}
	// Names, including ones made of hex letters, are left to the resolver.
	for _, host := range []string{"example.test", "cafe.example.test", "add.bed", "1.example.test", "0x.example"} {
		if _, ok := literalHost(host); ok {
			t.Errorf("literalHost(%q) treated a name as an address", host)
		}
	}
}

func TestParsePrivateCIDRs(t *testing.T) {
	got, err := ParsePrivateCIDRs(" 192.168.77.0/24, 10.20.0.0/16,172.16.5.0/24 ,fd00:77::/64,")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].String() != "192.168.77.0/24" || got[3].String() != "fd00:77::/64" {
		t.Fatalf("got %v", got)
	}
	if got, err := ParsePrivateCIDRs(""); err != nil || got != nil {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for in, want := range map[string]string{
		"127.0.0.0/8":         "only ranges inside",
		"169.254.169.254/32":  "only ranges inside",
		"100.64.0.0/10":       "only ranges inside",
		"224.0.0.0/4":         "only ranges inside",
		"0.0.0.0/0":           "only ranges inside",
		"8.8.8.0/24":          "only ranges inside",
		"10.0.0.0/7":          "only ranges inside", // wider than 10/8
		"172.0.0.0/8":         "only ranges inside", // wider than 172.16/12
		"192.168.0.0/15":      "only ranges inside",
		"::1/128":             "only ranges inside",
		"fe80::/10":           "only ranges inside",
		"fc00::/6":            "only ranges inside",
		"::/0":                "only ranges inside",
		"::ffff:10.0.0.0/104": "IPv4 form",
		"192.168.77.1/24":     "host bits",
		"192.168.77.1":        "not a CIDR",
		"10.0.0.0/8,lan":      "not a CIDR",
	} {
		if _, err := ParsePrivateCIDRs(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParsePrivateCIDRs(%q) = %v, want an error containing %q", in, err, want)
		}
	}
}
