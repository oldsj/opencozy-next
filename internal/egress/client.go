package egress

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/oauth2"
)

var (
	// ErrRefused means the destination is not allowed: its address, port or scheme.
	ErrRefused = errors.New("egress: destination refused")
	// ErrRedirect means the server redirected and the client does not follow redirects.
	ErrRedirect = errors.New("egress: redirect refused")
	// ErrTooLarge means a response body went over the client's cap, counted after
	// decompression.
	ErrTooLarge = errors.New("egress: response too large")
)

// CalendarClient is the calendar feed client's name.
const CalendarClient = "ics"

// GoogleClient is the Google calendar client's name: Google's token and API hosts only.
const GoogleClient = "google"

// OIDCClient is the single sign-on provider client's name.
const OIDCClient = "oidc"

// FeedPorts are the ports a calendar feed may use (design §10).
var FeedPorts = []uint16{80, 443, 8080, 8443}

// Options configure one integration's client. Name, Timeout and MaxResponseBytes are required.
type Options struct {
	// Name is the integration, used in errors ("home-assistant", "oidc", "ics").
	Name string
	// PrivateCIDRs are the private ranges this integration may reach besides public addresses,
	// from ParsePrivateCIDRs. Nil means public only.
	PrivateCIDRs []netip.Prefix
	// Ports are the destination ports allowed. Nil means 443 only.
	Ports []uint16
	// Hosts, when set, are the only host names this client may reach (exact, lower case; an
	// IP literal never matches). Every address they resolve to is still checked.
	Hosts []string
	// Timeout bounds each request, from dial to the end of the body.
	Timeout time.Duration
	// MaxResponseBytes caps each response body after decompression.
	MaxResponseBytes int64
	// MaxRedirects is how many redirects to follow, each through the same checks. Zero refuses
	// every redirect with ErrRedirect.
	MaxRedirects int
	// HTTP2 negotiates HTTP/2 over TLS (APNs needs it). A transport with a custom dialer does
	// not attempt it otherwise.
	HTTP2 bool
	// TLS is an optional base TLS configuration (client certificates, extra roots). It is
	// cloned, and its minimum version raised to TLS 1.2. InsecureSkipVerify is refused.
	TLS *tls.Config
}

// New returns the client for one integration.
func New(o Options) (*http.Client, error) { return build(o, nil) }

// Context returns ctx carrying c for libraries that take their client from the context,
// x/oauth2 and coreos/go-oidc among them. Without it they fall back to http.DefaultClient.
func Context(ctx context.Context, c *http.Client) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c)
}

// build is New, with allowOverride replacing the address check in this package's tests only.
func build(o Options, allowOverride func(netip.Addr) bool) (*http.Client, error) {
	switch {
	case o.Name == "":
		return nil, errors.New("egress: Options.Name is required")
	case o.Timeout <= 0:
		return nil, fmt.Errorf("egress %s: Timeout is required", o.Name)
	case o.MaxResponseBytes <= 0:
		return nil, fmt.Errorf("egress %s: MaxResponseBytes is required", o.Name)
	case o.MaxRedirects < 0:
		return nil, fmt.Errorf("egress %s: MaxRedirects must not be negative", o.Name)
	}
	if o.TLS != nil && o.TLS.InsecureSkipVerify {
		// A self-signed server gets extra roots (TLS.RootCAs) or a pin, never no checks.
		return nil, fmt.Errorf("egress %s: TLS.InsecureSkipVerify is not allowed", o.Name)
	}
	for _, p := range o.PrivateCIDRs {
		if err := checkPrivatePrefix(p); err != nil {
			return nil, fmt.Errorf("egress %s: private CIDR %s: %w", o.Name, p, err)
		}
	}
	hosts := map[string]bool{}
	for _, h := range o.Hosts {
		if h == "" || h != strings.ToLower(h) || strings.ContainsAny(h, ":/ ") {
			return nil, fmt.Errorf("egress %s: host %q must be a lower-case name", o.Name, h)
		}
		if _, err := netip.ParseAddr(h); err == nil {
			return nil, fmt.Errorf("egress %s: host %q is an address, not a name", o.Name, h)
		}
		hosts[h] = true
	}
	ports := o.Ports
	if ports == nil {
		ports = []uint16{443}
	}
	d := &dialer{name: o.Name, private: o.PrivateCIDRs, ports: map[uint16]bool{}, hosts: hosts, resolver: net.DefaultResolver}
	for _, p := range ports {
		d.ports[p] = true
	}
	d.allow = d.allowed
	if allowOverride != nil {
		d.allow = allowOverride
	}
	d.net = net.Dialer{Timeout: 10 * time.Second, Control: d.control}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if o.TLS != nil {
		tlsConfig = o.TLS.Clone()
		if tlsConfig.MinVersion < tls.VersionTLS12 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	tr := &http.Transport{
		Proxy:                  nil,
		DialContext:            d.dial,
		TLSClientConfig:        tlsConfig,
		ForceAttemptHTTP2:      o.HTTP2,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  o.Timeout,
		MaxResponseHeaderBytes: 64 << 10,
		MaxIdleConns:           16,
		IdleConnTimeout:        90 * time.Second,
	}
	return &http.Client{
		Transport:     &capped{base: tr, name: o.Name, max: o.MaxResponseBytes},
		Timeout:       o.Timeout,
		CheckRedirect: checkRedirect(o.Name, o.MaxRedirects),
	}, nil
}

func checkRedirect(name string, max int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if max == 0 {
			return fmt.Errorf("%w (%s)", ErrRedirect, name)
		}
		if len(via) > max {
			return fmt.Errorf("%w (%s): more than %d", ErrRedirect, name, max)
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("%w (%s): https to %s", ErrRedirect, name, req.URL.Scheme)
		}
		return nil // the next hop dials through the same checks
	}
}

type dialer struct {
	name     string
	private  []netip.Prefix
	ports    map[uint16]bool
	hosts    map[string]bool // empty: any host
	resolver interface {
		LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	}
	allow func(netip.Addr) bool
	net   net.Dialer
}

func (d *dialer) allowed(a netip.Addr) bool {
	return IsPublic(a) || inAny(d.private, a) || e2eLoopback(d.name, a)
}

func (d *dialer) refuse(format string, args ...any) error {
	return fmt.Errorf("%w (%s): %s", ErrRefused, d.name, fmt.Sprintf(format, args...))
}

// dial resolves the host, checks every address, and dials only checked addresses. A literal
// host is checked as written, so an IPv4-mapped IPv6 literal is refused. Resolver answers are
// unmapped first: Go reports A records as IPv4-mapped addresses, and the unmapped address is
// the one dialled.
func (d *dialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, d.refuse("bad address %q", address)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || !d.ports[uint16(port)] {
		return nil, d.refuse("port %s is not allowed", portText)
	}
	if len(d.hosts) > 0 && !d.hosts[strings.ToLower(strings.TrimSuffix(host, "."))] {
		return nil, d.refuse("host %q is not one this client may reach", host)
	}
	var addrs []netip.Addr
	if a, ok := literalHost(host); ok {
		if !a.IsValid() {
			return nil, d.refuse("host %q is a non-standard IPv4 spelling", host)
		}
		addrs = []netip.Addr{a}
	} else {
		found, err := d.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("egress %s: resolve %s: %w", d.name, host, err)
		}
		for _, a := range found {
			addrs = append(addrs, a.Unmap())
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("egress %s: %s has no addresses", d.name, host)
	}
	// Every answer must pass, so a name that mixes public and private answers is refused.
	for _, a := range addrs {
		if !d.allow(a) {
			return nil, d.refuse("%s resolves to %s", host, a)
		}
	}
	var errs []error
	for _, a := range addrs {
		if (network == "tcp4" && !a.Is4()) || (network == "tcp6" && !a.Is6()) {
			continue
		}
		conn, err := d.net.DialContext(ctx, network, netip.AddrPortFrom(a, uint16(port)).String())
		if err == nil {
			return conn, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, d.refuse("%s has no %s address", host, network)
	}
	return nil, errors.Join(errs...)
}

// control checks the socket address again just before connect, so no path reaches connect
// with an address the dialler did not check.
func (d *dialer) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !d.allow(ap.Addr().Unmap()) || !d.ports[ap.Port()] {
		return d.refuse("connect to %s", address)
	}
	return nil
}

// capped lets the transport decode gzip itself, so the cap counts decompressed bytes, and
// refuses an encoding the transport did not decode.
type capped struct {
	base http.RoundTripper
	name string
	max  int64
}

func (c *capped) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
		return nil, fmt.Errorf("%w (%s): scheme %q", ErrRefused, c.name, req.URL.Scheme)
	}
	if req.Header.Get("Accept-Encoding") != "" {
		// A caller-set Accept-Encoding turns off the transport's decoding, and the cap would
		// then count compressed bytes.
		req = req.Clone(req.Context())
		req.Header.Del("Accept-Encoding")
	}
	resp, err := c.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("egress %s: undecoded Content-Encoding %q", c.name, enc)
	}
	if resp.ContentLength > c.max {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w (%s): %d bytes declared, cap %d", ErrTooLarge, c.name, resp.ContentLength, c.max)
	}
	resp.Body = &cappedBody{rc: resp.Body, left: c.max, name: c.name}
	return resp, nil
}

type cappedBody struct {
	rc   io.ReadCloser
	left int64
	name string
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.left < 0 {
		return 0, fmt.Errorf("%w (%s)", ErrTooLarge, b.name)
	}
	// Read one byte past the cap, so a body of exactly the cap ends cleanly.
	if int64(len(p)) > b.left+1 {
		p = p[:b.left+1]
	}
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	if b.left < 0 {
		return n + int(b.left), fmt.Errorf("%w (%s)", ErrTooLarge, b.name)
	}
	return n, err
}

func (b *cappedBody) Close() error { return b.rc.Close() }
