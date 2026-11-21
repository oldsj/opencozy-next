// Package importer copies the frozen old app (commit d7fead8, migration 0045) into a
// new, empty installation, in one transaction (design §12, §13).
//
//   - The source is opened read-only: default_transaction_read_only=on and one REPEATABLE READ
//     READ ONLY snapshot, with SELECT only. Nothing is ever written back.
//   - The target must be migrated and empty. The import runs as the schema owner with every
//     trigger and constraint on, so the new schema checks every row.
//   - Dry run does the whole import, prints the reconciliation and rolls back.
//   - The reconciliation lists every old table: its rows, the rows it became, and whether it is
//     carried, not carried, or still waiting on a later slice (pending). Any mismatch fails the
//     run. A real import also refuses while anything is pending, so a partial import can never
//     be committed.
//   - Output is counts only. Errors name the table and the constraint, never row contents.
//
// Each domain adds its mapping in its own file (households.go, adults.go, credentials.go, …)
// and registers it in domains.
package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/importer/oldsecret"
	"github.com/oldsj/opencozy-next/internal/keyring"
)

// oldSchema is where the old app keeps its tables.
const oldSchema = "opencozy"

type oldMigration struct{ version, checksum string }

// Options configure a run.
type Options struct {
	// SourceURL is the old database: a restored backup, never the live one.
	SourceURL string
	// Target is the new database as its schema owner (MIGRATION_DATABASE_URL), already migrated.
	Target *pgxpool.Pool
	DryRun bool
	Out    io.Writer
	// Keys seals imported calendar links (OPENCOZY_SECRET_KEYS); needed only when there are any.
	Keys *keyring.Keyring
	// OldSecret opens the old app's integration secrets (--old-auth-secret-file) so they can be
	// sealed under Keys. Without it, those integrations import as "reconnect needed".
	OldSecret *oldsecret.Secret
}

// Errors.
var (
	ErrMismatch = errors.New("import: the reconciliation found mismatches; nothing was written")
	ErrPending  = errors.New("import: some old data has no mapping yet; nothing was written (a dry run shows what)")
)

// Run imports, prints the reconciliation to o.Out, and commits only for a real run that is
// complete and reconciled.
func Run(ctx context.Context, o Options) error {
	src, err := openSource(ctx, o.SourceURL)
	if err != nil {
		return err
	}
	defer src.close(ctx)
	if err := checkVersion(ctx, src.tx); err != nil {
		return err
	}

	tx, err := o.Target.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	// Nothing may create a household (bootstrap-owner) while the import runs.
	if _, err := tx.Exec(ctx, "LOCK TABLE household IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return safe(err)
	}
	if err := checkEmpty(ctx, tx); err != nil {
		return err
	}

	r, err := importAll(ctx, o, src.tx, tx)
	if err != nil {
		return err
	}
	r.Print(o.Out)

	switch {
	case r.Failed():
		return ErrMismatch
	case o.DryRun:
		_, _ = fmt.Fprintln(o.Out, "Dry run: everything above was written and checked, then rolled back.")
		return nil
	case r.Pending():
		return ErrPending
	}
	if err := tx.Commit(ctx); err != nil {
		return safe(err)
	}
	_, _ = fmt.Fprintln(o.Out, "Imported and committed.")
	return nil
}

// importAll runs every domain from src into dst and reconciles; it neither prints nor commits.
func importAll(ctx context.Context, o Options, src, dst pgx.Tx) (*Report, error) {
	r := &Report{}
	run := &run{src: src, dst: dst, r: r, adults: map[string]adultRef{}, keys: o.Keys, old: o.OldSecret, carried: map[string]bool{}}
	for _, d := range domains {
		if err := d(ctx, run); err != nil {
			return nil, err
		}
	}
	run.remarkOldSecret()
	if err := coverage(ctx, src, r); err != nil {
		return nil, err
	}
	return r, nil
}

// run is one import's state.
type run struct {
	src pgx.Tx
	dst pgx.Tx
	r   *Report
	// adults maps old Better Auth user IDs to the new adults.
	adults map[string]adultRef
	keys   *keyring.Keyring
	old    *oldsecret.Secret
	// carried lists the ledger source kinds whose rows this import has written.
	carried map[string]bool
}

// remarkOldSecret says in the report whether old integration secrets could be opened.
func (x *run) remarkOldSecret() {
	if x.old == nil {
		x.r.Remarks = append(x.r.Remarks, "Old integration secrets: no --old-auth-secret-file, so Google, iCloud and Home Assistant import as \"reconnect needed\".")
		return
	}
	x.r.Remarks = append(x.r.Remarks, "Old integration secrets: opened with --old-auth-secret-file and sealed with OPENCOZY_SECRET_KEYS.")
}

// domains run in order; later ones use the IDs earlier ones mapped.
var domains = []func(context.Context, *run) error{
	importHouseholds,
	importChildren,
	importAdults,
	importScreenTime,
	importCredentials,
	importLearning,
	importLedger,
	importCalendars,
	importRecipes,
	importMeals,
	importTV,
	checkLedgerSources, // last: every source kind has been imported
}

type source struct {
	conn *pgx.Conn
	tx   pgx.Tx
}

func openSource(ctx context.Context, url string) (*source, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, errors.New("import: invalid --source-url") // the error would echo the password
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["application_name"] = "opencozy-import"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("import: source: %w", safe(err))
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("import: source: %w", safe(err))
	}
	var ro string
	if err := tx.QueryRow(ctx, "SHOW transaction_read_only").Scan(&ro); err != nil || ro != "on" {
		_ = conn.Close(ctx)
		return nil, errors.New("import: the source transaction is not read-only")
	}
	return &source{conn: conn, tx: tx}, nil
}

func (s *source) close(ctx context.Context) {
	_ = s.tx.Rollback(ctx)
	_ = s.conn.Close(ctx)
}

// checkVersion refuses any source other than the old app at d7fead8.
func checkVersion(ctx context.Context, src pgx.Tx) error {
	rows, err := src.Query(ctx, "SELECT version, checksum FROM opencozy.schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("import: the source is not the old app (it has no old migration history): %w", safe(err))
	}
	got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (oldMigration, error) {
		var m oldMigration
		return m, r.Scan(&m.version, &m.checksum)
	})
	if err != nil {
		return safe(err)
	}
	if !slices.Equal(got, oldMigrations) {
		last := "none"
		if len(got) > 0 {
			last = got[len(got)-1].version
		}
		return fmt.Errorf("import: the source is not the old app at d7fead8 (it has %d migrations, the last %s; want %d ending %s, with matching checksums)",
			len(got), last, len(oldMigrations), oldMigrations[len(oldMigrations)-1].version)
	}
	return nil
}

// checkEmpty refuses a target that holds any application rows.
func checkEmpty(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'
		AND tablename NOT IN ('goose_db_version', 'job_lease') ORDER BY tablename`)
	if err != nil {
		return safe(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return safe(err)
	}
	for _, t := range tables {
		var any bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+pgx.Identifier{t}.Sanitize()+")").Scan(&any); err != nil {
			return safe(err)
		}
		if any {
			return fmt.Errorf("import: the target is not empty (%s has rows); import into a fresh installation", t)
		}
	}
	return nil
}

// safe keeps a PostgreSQL error from printing row contents. Integrity errors (class 23) keep
// their message, which names the table and constraint but no values; the table and constraint
// are added. Any other error keeps only its code, since messages such as invalid input syntax
// quote the value. Detail and Where, which can hold the failing row, are always dropped.
func safe(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	var b strings.Builder
	if strings.HasPrefix(pg.Code, "23") {
		b.WriteString(pg.Message)
	} else {
		b.WriteString("database error")
	}
	if pg.TableName != "" {
		fmt.Fprintf(&b, " (table %s", pg.TableName)
		if pg.ConstraintName != "" {
			fmt.Fprintf(&b, ", constraint %s", pg.ConstraintName)
		}
		b.WriteString(")")
	}
	fmt.Fprintf(&b, " [SQLSTATE %s]", pg.Code)
	return errors.New(b.String())
}
