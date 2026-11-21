// Package db opens the PostgreSQL pool and applies migrations. Queries live in queries/*.sql and
// are generated into dbq by sqlc: SQL is always parameterized, never built from strings.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open connects a pool and checks the connection.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("db: invalid DATABASE_URL") // the error would echo the password
	}
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration, as the schema owner, then grants runtimeRole (when
// set) row access. A Postgres advisory lock lets several replicas start at once.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, runtimeRole string) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, dir, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("db: migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	for _, r := range results {
		log.InfoContext(ctx, "migration applied", "version", r.Source.Version, "path", r.Source.Path, "duration", r.Duration)
	}
	if runtimeRole != "" {
		return Grant(ctx, pool, runtimeRole)
	}
	return nil
}

// appendOnly lists tables the runtime role may only read and insert into.
var appendOnly = []string{"point_ledger", "learning_award"}

// Grant gives role row access to the application's tables and nothing on goose's bookkeeping.
// It runs after every migration, so new tables are covered. It also grants CONNECT on the
// database: a deployment that revokes it from PUBLIC (as the CNPG bootstraps do) would otherwise
// refuse every connection as role.
func Grant(ctx context.Context, pool *pgxpool.Pool, role string) error {
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return fmt.Errorf("db: grant: %w", err)
	}
	r := pgx.Identifier{role}.Sanitize()
	stmts := []string{
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{database}.Sanitize() + " TO " + r,
		"GRANT USAGE ON SCHEMA public TO " + r,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + r,
		"REVOKE ALL ON TABLE goose_db_version FROM " + r,
	}
	for _, t := range appendOnly {
		stmts = append(stmts, "REVOKE UPDATE, DELETE, TRUNCATE ON TABLE "+pgx.Identifier{t}.Sanitize()+" FROM "+r)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("db: grant: %w", err)
		}
	}
	return tx.Commit(ctx)
}
