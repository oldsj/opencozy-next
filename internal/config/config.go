// Package config reads and validates the process environment. It is the only package that
// reads it (golangci-lint's forbidigo enforces this).
package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/oldsj/opencozy-next/internal/egress"
	"github.com/oldsj/opencozy-next/internal/keyring"
)

// Config is the server's configuration. Secrets have no defaults: a missing one stops startup.
type Config struct {
	// DatabaseURL is a libpq URL or DSN for the runtime role, which only reads and writes rows.
	// Secret.
	DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
	// MigrationDatabaseURL is the URL of the role that owns the schema and runs migrations.
	// Empty means DatabaseURL does both (development). Secret.
	MigrationDatabaseURL string `env:"MIGRATION_DATABASE_URL"`
	// DatabaseRuntimeRole is the runtime role's name. Migrations grant it row access to the
	// application's tables and nothing on goose's. Required with MigrationDatabaseURL.
	DatabaseRuntimeRole string `env:"DATABASE_RUNTIME_ROLE"`
	// ListenAddr is where the HTTP server listens.
	ListenAddr string `env:"LISTEN_ADDR" envDefault:":8080"`
	// LogLevel is debug, info, warn or error.
	LogLevel slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	// SessionTTL is how long a sign-in lasts.
	SessionTTL time.Duration `env:"SESSION_TTL" envDefault:"720h"`
	// TrustedProxies are the CIDRs of reverse proxies whose X-Forwarded-For is believed, for
	// the client address used by rate limiting. Empty means use the TCP peer address.
	TrustedProxies []netip.Prefix `env:"TRUSTED_PROXIES" envSeparator:","`
	// AuthRatePerMinute is the sign-in and password-confirm attempts allowed per client address.
	AuthRatePerMinute int `env:"AUTH_RATE_PER_MINUTE" envDefault:"10"`
	// RecentAuthWindow is how recent an adult's sign-in must be for pairing, passkeys and
	// issuing credentials. Shorter only for tests.
	RecentAuthWindow time.Duration `env:"RECENT_AUTH_WINDOW" envDefault:"10m"`
	// MigrateOnStart applies database migrations before serving.
	MigrateOnStart bool `env:"MIGRATE_ON_START" envDefault:"true"`

	// Slice 0c: secrets at rest and outbound requests (design §10, §11).

	// SecretKeys encrypts integration secrets: "id:base64key,…", the first key encrypts.
	// Secret. Unset means no integration can store a secret.
	SecretKeys *keyring.Keyring `env:"OPENCOZY_SECRET_KEYS"`
	// HomeAssistantAllowedPrivateCIDRs are the private ranges Home Assistant may be reached in
	// (RFC 1918 or ULA only; anything else stops startup).
	HomeAssistantAllowedPrivateCIDRs egress.PrivateCIDRs `env:"HOME_ASSISTANT_ALLOWED_PRIVATE_CIDRS"`
	// OIDCAllowedPrivateCIDRs are the private ranges an OIDC issuer may be reached in.
	OIDCAllowedPrivateCIDRs egress.PrivateCIDRs `env:"OIDC_ALLOWED_PRIVATE_CIDRS"`

	// PublicOrigin is the origin people reach OpenCozy at, e.g. https://cozy.example.test. Kid
	// devices are told it in the pairing QR code; it is never taken from the request's Host.
	// Without it, pairing a device works only on a loopback address (development).
	PublicOrigin string `env:"PUBLIC_ORIGIN"`

	// Slice 10: passkeys (design §6).
	// WebAuthnRPID is the relying party ID: the site's hostname, which must never change once
	// passkeys exist. Empty turns passkeys off.
	WebAuthnRPID string `env:"WEBAUTHN_RP_ID"`
	// WebAuthnOrigins are the origins passkeys are used from, e.g. https://cozy.example.test.
	// Required with WEBAUTHN_RP_ID.
	WebAuthnOrigins []string `env:"WEBAUTHN_ORIGINS" envSeparator:","`

	// Slice 11: the iOS app (design §8, §14).
	// AppleTeamID and AppleBundleID name the iOS app. Together they serve the app's
	// apple-app-site-association (passkeys in the app) and are the APNs topic.
	AppleTeamID   string `env:"APPLE_TEAM_ID"`
	AppleBundleID string `env:"APPLE_BUNDLE_ID"`
	// APNsKeyID and APNsKey turn push on: the token-auth .p8 key's ID and its PEM. Secret (the
	// key). Unset means no push is enqueued or sent.
	APNsKeyID string `env:"APNS_KEY_ID"`
	APNsKey   string `env:"APNS_KEY"`

	// Slice 10b: single sign-on with an OpenID Connect provider (design §6). Set the issuer,
	// client ID and client secret together, or none of them (single sign-on off). The provider's
	// redirect URI is PUBLIC_ORIGIN + OIDCCallbackPath, so PUBLIC_ORIGIN is required with them.

	// OIDCIssuer is the provider's issuer URL, exactly as its discovery document says it
	// (https). Changing it later unlinks everyone: links belong to an issuer.
	OIDCIssuer string `env:"OIDC_ISSUER"`
	// OIDCClientID is OpenCozy's client ID at the provider.
	OIDCClientID string `env:"OIDC_CLIENT_ID"`
	// OIDCClientSecret is the confidential client's secret. Secret.
	OIDCClientSecret string `env:"OIDC_CLIENT_SECRET"`
	// OIDCName is the button's text after "Sign in with".
	OIDCName string `env:"OIDC_NAME" envDefault:"single sign-on"`
}

// OIDCCallbackPath is where the provider sends the browser back: the old app's path, so the
// redirect URI registered at the provider keeps working.
const OIDCCallbackPath = "/api/auth/callback/oidc"

// OIDCEnabled reports whether single sign-on is set up.
func (c Config) OIDCEnabled() bool { return c.OIDCIssuer != "" }

// PushEnabled reports whether APNs is configured.
func (c Config) PushEnabled() bool { return c.APNsKeyID != "" }

// Load reads the environment and validates it.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.SessionTTL < time.Minute || c.SessionTTL > 90*24*time.Hour {
		errs = append(errs, errors.New("SESSION_TTL must be between 1m and 2160h"))
	}
	if c.RecentAuthWindow != 0 && (c.RecentAuthWindow < 5*time.Second || c.RecentAuthWindow > time.Hour) {
		errs = append(errs, errors.New("RECENT_AUTH_WINDOW must be between 5s and 1h"))
	}
	if c.AuthRatePerMinute < 1 {
		errs = append(errs, errors.New("AUTH_RATE_PER_MINUTE must be at least 1"))
	}
	if strings.Contains(c.DatabaseURL, "sslmode=disable") && !isLocalDatabase(c.DatabaseURL) {
		errs = append(errs, errors.New("DATABASE_URL: sslmode=disable is only allowed for localhost"))
	}
	if strings.Contains(c.MigrationDatabaseURL, "sslmode=disable") && !isLocalDatabase(c.MigrationDatabaseURL) {
		errs = append(errs, errors.New("MIGRATION_DATABASE_URL: sslmode=disable is only allowed for localhost"))
	}
	errs = append(errs, c.validateWebAuthn()...)
	errs = append(errs, c.validateApple()...)
	errs = append(errs, c.validateOIDC()...)
	if c.PublicOrigin != "" && !validOrigin(c.PublicOrigin) {
		errs = append(errs, fmt.Errorf("PUBLIC_ORIGIN: %q must be https://host[:port] (http only for localhost)", c.PublicOrigin))
	}
	if c.MigrationDatabaseURL != "" && c.DatabaseRuntimeRole == "" {
		errs = append(errs, errors.New("DATABASE_RUNTIME_ROLE is required with MIGRATION_DATABASE_URL"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return nil
}

func (c Config) validateWebAuthn() []error {
	if c.WebAuthnRPID == "" {
		if len(c.WebAuthnOrigins) > 0 {
			return []error{errors.New("WEBAUTHN_ORIGINS needs WEBAUTHN_RP_ID")}
		}
		return nil
	}
	if strings.ContainsAny(c.WebAuthnRPID, ":/ ") || c.WebAuthnRPID != strings.ToLower(c.WebAuthnRPID) {
		return []error{errors.New("WEBAUTHN_RP_ID must be a lower-case hostname, with no scheme or port")}
	}
	if len(c.WebAuthnOrigins) == 0 {
		return []error{errors.New("WEBAUTHN_ORIGINS is required with WEBAUTHN_RP_ID")}
	}
	var errs []error
	for _, o := range c.WebAuthnOrigins {
		u, err := url.Parse(o)
		host := ""
		if err == nil {
			host = u.Hostname()
		}
		local := u != nil && u.Scheme == "http" && host == "localhost"
		switch {
		case err != nil || u.Path != "" || u.RawQuery != "" || u.User != nil || (u.Scheme != "https" && !local):
			errs = append(errs, fmt.Errorf("WEBAUTHN_ORIGINS: %q must be https://host[:port] (http only for localhost)", o))
		case host != c.WebAuthnRPID && !strings.HasSuffix(host, "."+c.WebAuthnRPID):
			errs = append(errs, fmt.Errorf("WEBAUTHN_ORIGINS: %q is not on %s", o, c.WebAuthnRPID))
		}
	}
	return errs
}

var (
	appleID  = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	bundleID = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
)

func (c Config) validateApple() []error {
	var errs []error
	if (c.AppleTeamID == "") != (c.AppleBundleID == "") {
		errs = append(errs, errors.New("APPLE_TEAM_ID and APPLE_BUNDLE_ID go together"))
	}
	if c.AppleTeamID != "" && !appleID.MatchString(c.AppleTeamID) {
		errs = append(errs, errors.New("APPLE_TEAM_ID must be 10 upper-case letters or digits"))
	}
	if c.AppleBundleID != "" && (len(c.AppleBundleID) > 155 || !bundleID.MatchString(c.AppleBundleID)) {
		errs = append(errs, errors.New("APPLE_BUNDLE_ID must be a bundle ID such as org.example.opencozy"))
	}
	if c.APNsKeyID == "" && c.APNsKey == "" {
		return errs
	}
	if c.APNsKeyID == "" || c.APNsKey == "" || c.AppleTeamID == "" {
		return append(errs, errors.New("push needs APNS_KEY_ID, APNS_KEY, APPLE_TEAM_ID and APPLE_BUNDLE_ID together"))
	}
	if !appleID.MatchString(c.APNsKeyID) {
		errs = append(errs, errors.New("APNS_KEY_ID must be 10 upper-case letters or digits"))
	}
	if !validAPNsKey(c.APNsKey) {
		errs = append(errs, errors.New("APNS_KEY must be the .p8 key's PEM (a P-256 PKCS #8 key)")) // never echo the key
	}
	return errs
}

func (c Config) validateOIDC() []error {
	set := 0
	for _, v := range []string{c.OIDCIssuer, c.OIDCClientID, c.OIDCClientSecret} {
		if v != "" {
			set++
		}
	}
	switch set {
	case 0:
		return nil
	case 3:
	default:
		return []error{errors.New("OIDC_ISSUER, OIDC_CLIENT_ID and OIDC_CLIENT_SECRET go together: set all three, or none")}
	}
	var errs []error
	if !validIssuer(c.OIDCIssuer) {
		errs = append(errs, fmt.Errorf("OIDC_ISSUER: %q must be an https URL with no query, fragment or user", c.OIDCIssuer))
	}
	if len(c.OIDCClientID) > 255 || strings.TrimSpace(c.OIDCClientID) != c.OIDCClientID {
		errs = append(errs, errors.New("OIDC_CLIENT_ID must be at most 255 characters, with no surrounding spaces"))
	}
	if n := len([]rune(strings.TrimSpace(c.OIDCName))); n < 1 || n > 40 {
		errs = append(errs, errors.New("OIDC_NAME must be 1 to 40 characters"))
	}
	if c.PublicOrigin == "" {
		errs = append(errs, errors.New("PUBLIC_ORIGIN is required with OIDC_ISSUER: the provider's redirect URI is PUBLIC_ORIGIN"+OIDCCallbackPath))
	}
	return errs
}

// validAPNsKey reports whether k is a .p8 key: PEM, PKCS #8, P-256 (as push.ParseKey reads it).
func validAPNsKey(k string) bool {
	block, _ := pem.Decode([]byte(k))
	if block == nil {
		return false
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return false
	}
	ec, ok := key.(*ecdsa.PrivateKey)
	return ok && ec.Curve == elliptic.P256()
}

// validIssuer reports whether u is an issuer URL OpenCozy will use: https, with a host and no
// query, fragment or user info. Only an e2e build may use http on 127.0.0.1.
func validIssuer(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" || p.RawQuery != "" || p.ForceQuery || p.Fragment != "" || p.User != nil || p.Opaque != "" {
		return false
	}
	return p.Scheme == "https" || (E2EBuild && p.Scheme == "http" && p.Hostname() == "127.0.0.1")
}

func isLocalDatabase(url string) bool {
	return strings.Contains(url, "@localhost") || strings.Contains(url, "@127.0.0.1") ||
		strings.Contains(url, "host=localhost") || strings.Contains(url, "host=127.0.0.1")
}

// MigrationURL is the URL migrations run with.
func (c Config) MigrationURL() string {
	if c.MigrationDatabaseURL != "" {
		return c.MigrationDatabaseURL
	}
	return c.DatabaseURL
}

// TestDatabaseURL is the admin URL of a disposable PostgreSQL server for integration tests.
func TestDatabaseURL() string { return os.Getenv("TEST_DATABASE_URL") }

// InCI reports whether tests run in CI, where a missing test database is a failure, not a skip.
func InCI() bool { return os.Getenv("CI") == "true" }

// validOrigin reports whether o is a bare origin: https://host[:port], or http for localhost.
func validOrigin(o string) bool {
	u, err := url.Parse(o)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
}
