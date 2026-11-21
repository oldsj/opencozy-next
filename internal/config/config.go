// Package config reads and validates the process environment. It is the only package that
// reads it (golangci-lint's forbidigo enforces this).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
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

	// Slice 12: MCPToken is the assistant credential (oca_) `app mcp` acts as over stdio. Only
	// that command reads it; the server never does. Secret: no default.
	MCPToken string `env:"OPENCOZY_MCP_TOKEN"`
}

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
