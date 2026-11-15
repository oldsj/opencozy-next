package egress

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var loopback = netip.MustParseAddr("127.0.0.1")

// testClient is New with 127.0.0.1 standing in for a public address, so httptest servers can
// play the integration. It counts the connections that passed the check.
func testClient(t *testing.T, o Options) (*http.Client, *atomic.Int32) {
	t.Helper()
	if o.Name == "" {
		o.Name = "test"
	}
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Second
	}
	if o.MaxResponseBytes == 0 {
		o.MaxResponseBytes = 1 << 20
	}
	var dials atomic.Int32
	c, err := build(o, func(a netip.Addr) bool {
		if a == loopback {
			dials.Add(1)
			return true
		}
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, &dials
}

func serverPort(t *testing.T, s *httptest.Server) uint16 {
	t.Helper()
	u, _ := url.Parse(s.URL)
	p, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(p)
}

func get(t *testing.T, c *http.Client, rawURL string, header ...string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	return c.Do(req)
}

// try makes a GET and only reports its error.
func try(t *testing.T, c *http.Client, rawURL string) error {
	t.Helper()
	resp, err := get(t, c, rawURL)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestNewRefusesLoopbackAndPrivate(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a refused destination was reached")
	}))
	defer s.Close()
	port := serverPort(t, s)
	c, err := New(Options{Name: "test", Timeout: 5 * time.Second, MaxResponseBytes: 1024, Ports: []uint16{port}})
	if err != nil {
		t.Fatal(err)
	}
	p := strconv.Itoa(int(port))
	for _, host := range []string{
		"127.0.0.1", "localhost", "[::1]", "[::ffff:127.0.0.1]", "[::ffff:7f00:1]", "[::7f00:1]",
		"2130706433", "0x7f000001", "0177.0.0.1", "127.1", "10.0.0.1", "169.254.169.254", "[fe80::1]",
	} {
		err := try(t, c, "http://"+host+":"+p+"/")
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want ErrRefused", host, err)
		}
	}
}

func TestNewChecksOptions(t *testing.T) {
	ok := Options{Name: "x", Timeout: time.Second, MaxResponseBytes: 1}
	for name, o := range map[string]Options{
		"no name":     {Timeout: time.Second, MaxResponseBytes: 1},
		"no timeout":  {Name: "x", MaxResponseBytes: 1},
		"no cap":      {Name: "x", Timeout: time.Second},
		"bad private": {Name: "x", Timeout: time.Second, MaxResponseBytes: 1, PrivateCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}},
		"wide":        {Name: "x", Timeout: time.Second, MaxResponseBytes: 1, PrivateCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/7")}},
		"insecure":    {Name: "x", Timeout: time.Second, MaxResponseBytes: 1, TLS: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // the case under test
	} {
		if _, err := New(o); err == nil {
			t.Errorf("%s: New accepted %+v", name, o)
		}
	}
	if _, err := New(ok); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateAllowlist(t *testing.T) {
	d := &dialer{private: []netip.Prefix{netip.MustParsePrefix("192.168.40.0/24")}}
	for addr, want := range map[string]bool{
		"192.168.40.7": true, "192.168.41.7": false, "10.0.0.1": false, "8.8.8.8": true, "127.0.0.1": false,
		"::ffff:192.168.40.7": false, // a mapped literal is not the allowlisted address
	} {
		if got := d.allowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("allowed(%s) = %v, want %v", addr, got, want)
		}
	}
}

type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if a, ok := f[host]; ok {
		return a, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestDialChecksEveryResolvedAddress(t *testing.T) {
	m := netip.MustParseAddr
	d := &dialer{name: "test", ports: map[uint16]bool{443: true}, resolver: fakeResolver{
		"mixed.example.test":  {m("8.8.8.8"), m("10.0.0.1")},
		"mapped.example.test": {m("::ffff:169.254.169.254")}, // how Go reports an A record
		"meta.example.test":   {m("169.254.169.254")},
		"6to4.example.test":   {m("2002:a9fe:a9fe::1")},
		"nat64.example.test":  {m("64:ff9b::a9fe:a9fe")},
	}}
	d.allow = d.allowed
	for _, host := range []string{"mixed", "mapped", "meta", "6to4", "nat64"} {
		_, err := d.dial(t.Context(), "tcp", host+".example.test:443")
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want ErrRefused", host, err)
		}
	}
	if _, err := d.dial(t.Context(), "tcp", "public.example.test:8443"); !errors.Is(err, ErrRefused) {
		t.Errorf("port 8443 not in the list: err = %v", err)
	}
}

func TestPorts(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer s.Close()
	c, _ := testClient(t, Options{}) // nil Ports: 443 only
	if err := try(t, c, s.URL); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused for a port outside the list", err)
	}
	c, _ = testClient(t, Options{Ports: []uint16{serverPort(t, s)}})
	if err := try(t, c, s.URL); err != nil {
		t.Fatal(err)
	}
}

func TestIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example.test:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.test:3128")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer s.Close()
	c, dials := testClient(t, Options{Ports: []uint16{serverPort(t, s)}})
	if err := try(t, c, s.URL); err != nil {
		t.Fatal(err)
	}
	if dials.Load() == 0 {
		t.Fatal("no direct connection")
	}
}

func TestRedirects(t *testing.T) {
	var target string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hop":
			http.Redirect(w, r, "/done", http.StatusFound)
		case "/away":
			http.Redirect(w, r, target, http.StatusFound)
		}
	}))
	defer s.Close()
	port := serverPort(t, s)
	target = "http://10.0.0.1:" + strconv.Itoa(int(port)) + "/"

	c, _ := testClient(t, Options{Ports: []uint16{port}})
	if err := try(t, c, s.URL+"/hop"); !errors.Is(err, ErrRedirect) {
		t.Errorf("default: err = %v, want ErrRedirect", err)
	}
	c, _ = testClient(t, Options{Ports: []uint16{port}, MaxRedirects: 2})
	resp, err := get(t, c, s.URL+"/hop")
	if err != nil {
		t.Fatalf("followed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.Request.URL.Path != "/done" {
		t.Errorf("followed to %s", resp.Request.URL.Path)
	}
	if err := try(t, c, s.URL+"/away"); !errors.Is(err, ErrRefused) {
		t.Errorf("redirect to a private address: err = %v, want ErrRefused", err)
	}
}

func TestSizeCapAfterDecompression(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(bytes.Repeat([]byte("a"), 64<<10)) // 64 KiB that compresses to well under 1 KiB
	_ = zw.Close()
	if gz.Len() >= 1024 {
		t.Fatalf("fixture compressed to %d bytes", gz.Len())
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gzip":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gz.Bytes())
		case "/br":
			w.Header().Set("Content-Encoding", "br")
			_, _ = w.Write([]byte("not really brotli"))
		case "/exact":
			_, _ = w.Write(bytes.Repeat([]byte("b"), 1024))
		case "/declared":
			w.Header().Set("Content-Length", "4096")
			_, _ = w.Write(bytes.Repeat([]byte("c"), 4096))
		}
	}))
	defer s.Close()
	c, _ := testClient(t, Options{Ports: []uint16{serverPort(t, s)}, MaxResponseBytes: 1024})

	// Whether or not the caller asks for gzip itself, the cap counts decompressed bytes.
	for _, header := range [][]string{nil, {"Accept-Encoding", "gzip"}} {
		resp, err := get(t, c, s.URL+"/gzip", header...)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("gzip %v: err = %v, want ErrTooLarge", header, err)
		}
	}
	if err := try(t, c, s.URL+"/br"); err == nil || !strings.Contains(err.Error(), "undecoded") {
		t.Errorf("br: err = %v, want an undecoded-encoding error", err)
	}
	resp, err := get(t, c, s.URL+"/exact")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if b, err := io.ReadAll(resp.Body); err != nil || len(b) != 1024 {
		t.Errorf("exactly the cap: %d bytes, %v", len(b), err)
	}
	if err := try(t, c, s.URL+"/declared"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("declared length: err = %v, want ErrTooLarge", err)
	}
}

func TestHTTP2(t *testing.T) {
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	s.EnableHTTP2 = true
	s.StartTLS()
	defer s.Close()
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	for _, want := range []int{1, 2} {
		c, _ := testClient(t, Options{Ports: []uint16{serverPort(t, s)}, HTTP2: want == 2, TLS: &tls.Config{RootCAs: roots}})
		resp, err := get(t, c, s.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.ProtoMajor != want {
			t.Errorf("HTTP2=%v: negotiated %s", want == 2, resp.Proto)
		}
	}
}

// failingTransport stands in for http.DefaultTransport, so a library that falls back to
// http.DefaultClient fails the test instead of quietly bypassing egress.
type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("request to %s bypassed egress", r.URL)
	return nil, errors.New("bypassed egress")
}

// Discovery and a token exchange through coreos/go-oidc and x/oauth2 (design §10, "Library
// clients"). Not parallel: it replaces http.DefaultTransport.
func TestOIDCAndOAuth2UseEgress(t *testing.T) {
	saved := http.DefaultTransport
	http.DefaultTransport = failingTransport{t}
	t.Cleanup(func() { http.DefaultTransport = saved })

	mux := http.NewServeMux()
	s := httptest.NewServer(mux)
	defer s.Close()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": s.URL, "authorization_endpoint": s.URL + "/authorize", "token_endpoint": s.URL + "/token",
			"jwks_uri": s.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("code") != "fictional-code" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fictional-access","token_type":"Bearer","expires_in":60}`))
	})

	c, dials := testClient(t, Options{Name: "oidc", Ports: []uint16{serverPort(t, s)}})
	ctx := Context(t.Context(), c)
	provider, err := oidc.NewProvider(ctx, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	afterDiscovery := dials.Load()
	if afterDiscovery == 0 {
		t.Fatal("discovery did not go through egress")
	}
	conf := oauth2.Config{ClientID: "fictional-client", ClientSecret: "fictional-secret", Endpoint: provider.Endpoint(), RedirectURL: "https://app.example.test/api/auth/callback/oidc"}
	tok, err := conf.Exchange(ctx, "fictional-code", oauth2.VerifierOption("fictional-verifier-0123456789-0123456789-0123"))
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "fictional-access" {
		t.Fatalf("token %q", tok.AccessToken)
	}
	// The exchange may reuse discovery's connection, so it adds no dial; that it succeeded with
	// http.DefaultTransport failing every request shows it used the egress client.
}

// Certificates are always checked: a server whose root the client does not trust is refused,
// and there is no way to turn the check off (TestNewChecksOptions, "insecure").
func TestVerifiesCertificates(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer s.Close()
	c, _ := testClient(t, Options{Ports: []uint16{serverPort(t, s)}})
	var unknown x509.UnknownAuthorityError
	if err := try(t, c, s.URL); !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want an unknown-authority error", err)
	}
}
