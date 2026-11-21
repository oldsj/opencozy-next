// Command app is the whole service:
//
//	app serve            the server (the default)
//	app migrate          apply migrations as the schema owner and grant the runtime role
//	app bootstrap-owner  create the first household and its first adult (once only)
//	app add-adult        add an adult to a household
//	app rekey            re-encrypt stored secrets under the first OPENCOZY_SECRET_KEYS key
//	app mcp              MCP over stdio, as the assistant credential in OPENCOZY_MCP_TOKEN
//
// bootstrap-owner and add-adult read the password from stdin, so it stays out of the process
// list and shell history.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/calendar"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/db"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/jobs"
	"github.com/oldsj/opencozy-next/internal/keyring"
	"github.com/oldsj/opencozy-next/internal/push"
	"github.com/oldsj/opencozy-next/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		slog.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if err := config.CheckE2EBuild(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logTo := io.Writer(os.Stdout)
	if cmd == "mcp" {
		logTo = os.Stderr // stdout carries the protocol
	}
	log := slog.New(slog.NewJSONHandler(logTo, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	if config.E2EBuild {
		log.Warn("e2e build: the calendar and single sign-on clients may fetch from 127.0.0.1; for browser tests only, never a household")
	}

	switch cmd {
	case "migrate":
		return migrate(ctx, cfg, log)
	case "serve":
		if cfg.MigrateOnStart {
			if err := migrate(ctx, cfg, log); err != nil {
				return err
			}
		}
		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		srv := server.New(cfg, log, pool)
		var google *calendar.Google
		if cfg.GoogleEnabled() {
			if google, err = calendar.NewGoogle(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL()); err != nil {
				return err
			}
			srv.UseGoogle(google)
		}
		h, err := srv.Handler()
		if err != nil {
			return err
		}
		go srv.SweepSessions(ctx)
		srv.StartLive(ctx)
		go command.DeleteExpired(ctx, dbq.New(pool), log)
		background := []jobs.Job{srv.TVJob()}
		if cfg.SecretKeys != nil { // calendar links need the keyring
			fetcher, err := calendar.NewFetcher()
			if err != nil {
				return err
			}
			background = append(background, calendar.Job(cfg.SecretKeys, fetcher.WithGoogle(google), log, srv.CalendarWake()))
		}
		if cfg.PushEnabled() {
			apns, err := push.NewAPNs(push.APNsConfig{TeamID: cfg.AppleTeamID, Topic: cfg.AppleBundleID, KeyID: cfg.APNsKeyID, Key: cfg.APNsKey}, log)
			if err != nil {
				return err
			}
			background = append(background, push.Job(apns, srv.PushNotes(), log))
		}
		go jobs.Runner{Pool: pool, Log: log}.Run(ctx, background...)
		return server.Serve(ctx, log, cfg.ListenAddr, h, srv.Drain)
	case "mcp": // the assistant's tools over stdio (design §9)
		if cfg.MCPToken == "" {
			return errors.New("mcp: set OPENCOZY_MCP_TOKEN to an assistant credential's secret (oca_...)")
		}
		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		return server.New(cfg, log, pool).ServeMCP(ctx, cfg.MCPToken, &mcp.StdioTransport{})
	case "import": // copy the old app's data into this empty installation
		return runImport(ctx, cfg, log, args, stdout)
	case "bootstrap-owner", "add-adult", "rekey":
		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		switch cmd {
		case "rekey": // re-encrypt stored secrets under the first OPENCOZY_SECRET_KEYS key
			return keyring.RekeyAll(ctx, pool, cfg.SecretKeys, log)
		case "bootstrap-owner":
			return bootstrapOwner(ctx, pool, args, stdin, stdout)
		}
		return addAdult(ctx, pool, args, stdin, stdout)
	default:
		return fmt.Errorf("unknown command %q (want serve, migrate, bootstrap-owner, add-adult, rekey, mcp or import)", cmd)
	}
}

func migrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := db.Open(ctx, cfg.MigrationURL())
	if err != nil {
		return err
	}
	defer pool.Close()
	return db.Migrate(ctx, pool, log, cfg.DatabaseRuntimeRole)
}

// bootstrapOwner creates the first household and its first adult. It refuses once any household
// exists, so it cannot be used to slip an adult into a running installation.
func bootstrapOwner(ctx context.Context, pool *pgxpool.Pool, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("bootstrap-owner", flag.ContinueOnError)
	household := fs.String("household", "", "household name")
	tz := fs.String("time-zone", "", "IANA time zone, such as Europe/Amsterdam")
	email := fs.String("email", "", "the adult's email")
	name := fs.String("name", "", "the adult's name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *household == "" || *tz == "" || *email == "" || *name == "" {
		return errors.New("bootstrap-owner: -household, -time-zone, -email and -name are required")
	}
	if _, err := time.LoadLocation(*tz); err != nil {
		return fmt.Errorf("bootstrap-owner: unknown time zone %q", *tz)
	}
	hash, err := readPassword(stdin)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	// Serialize concurrent bootstraps; the count below is then reliable.
	if _, err := tx.Exec(ctx, "LOCK TABLE household IN EXCLUSIVE MODE"); err != nil {
		return err
	}
	q := dbq.New(tx)
	n, err := q.CountHouseholds(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return errors.New("bootstrap-owner: a household already exists; use add-adult")
	}
	h, err := q.CreateHousehold(ctx, dbq.CreateHouseholdParams{Name: *household, TimeZone: *tz})
	if err != nil {
		return err
	}
	a, err := createAdult(ctx, q, h.ID, *email, *name, hash)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "household %s\nadult %s\n", h.ID, a.ID)
	return err
}

func addAdult(ctx context.Context, pool *pgxpool.Pool, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("add-adult", flag.ContinueOnError)
	household := fs.String("household-id", "", "household id")
	email := fs.String("email", "", "the adult's email")
	name := fs.String("name", "", "the adult's name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	hid, err := uuid.Parse(*household)
	if err != nil || *email == "" || *name == "" {
		return errors.New("add-adult: -household-id (a uuid), -email and -name are required")
	}
	hash, err := readPassword(stdin)
	if err != nil {
		return err
	}
	q := dbq.New(pool)
	if _, err := q.GetHousehold(ctx, hid); err != nil {
		return fmt.Errorf("add-adult: no such household")
	}
	a, err := createAdult(ctx, q, hid, *email, *name, hash)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "adult %s\n", a.ID)
	return err
}

func createAdult(ctx context.Context, q *dbq.Queries, household uuid.UUID, email, name, hash string) (dbq.Adult, error) {
	handle, err := auth.NewUserHandle()
	if err != nil {
		return dbq.Adult{}, err
	}
	scheme := auth.SchemeArgon2id
	return q.CreateAdult(ctx, dbq.CreateAdultParams{
		HouseholdID: household, Email: strings.ToLower(strings.TrimSpace(email)), Name: name,
		PasswordHash: &hash, PasswordScheme: &scheme, WebauthnUserHandle: handle,
	})
}

// readPassword reads one line from stdin and hashes it.
func readPassword(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(stdin, 1024)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	pw := strings.TrimRight(line, "\r\n")
	if len(pw) < 12 || len(pw) > 256 {
		return "", errors.New("the password (on stdin) must be 12 to 256 characters")
	}
	return auth.HashPassword(pw)
}
