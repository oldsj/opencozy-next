package importer

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5"
)

// Status is an old table's state in the reconciliation.
type Status string

// Statuses.
const (
	OK         Status = "ok"
	Mismatch   Status = "MISMATCH"
	Pending    Status = "pending"
	NotCarried Status = "not carried"
)

// Line is one old table, or one extra check.
type Line struct {
	Old        string
	SourceRows int64
	New        string
	TargetRows int64
	Status     Status
	Notes      []string
}

// Report is the reconciliation. It holds counts only.
type Report struct {
	Lines  []Line
	Checks []Line
	// columns are the old columns each mapped table reads or drops on purpose.
	columns map[string][]string
}

func (r *Report) line(old string) *Line {
	for i := range r.Lines {
		if r.Lines[i].Old == old {
			return &r.Lines[i]
		}
	}
	r.Lines = append(r.Lines, Line{Old: old, Status: OK})
	return &r.Lines[len(r.Lines)-1]
}

// table records a carried old table: its rows, the target table, and the rows expected there
// and found there.
func (r *Report) table(old string, sourceRows int64, target string, want, got int64) {
	l := r.line(old)
	l.SourceRows, l.New, l.TargetRows = sourceRows, target, got
	if want != got {
		l.Status = Mismatch
		l.Notes = append(l.Notes, fmt.Sprintf("want %d rows in %s", want, target))
	}
}

// note adds a remark to an old table's line without changing its status.
func (r *Report) note(old, msg string) { l := r.line(old); l.Notes = append(l.Notes, msg) }

// pending marks part of an old table as waiting on a later slice.
func (r *Report) pending(old, msg string) {
	l := r.line(old)
	if l.Status == OK {
		l.Status = Pending
	}
	l.Notes = append(l.Notes, msg)
}

// columnsRead declares the old columns a mapping reads, plus those it drops on purpose. Any
// other column of that table makes it pending: a new column is never lost silently.
func (r *Report) columnsRead(old string, cols ...string) {
	if r.columns == nil {
		r.columns = map[string][]string{}
	}
	r.columns[old] = append(r.columns[old], cols...)
}

// check records a reconciliation check that is not a row count of one table.
func (r *Report) check(name string, want, got int64) {
	l := Line{Old: name, SourceRows: want, TargetRows: got, Status: OK}
	if want != got {
		l.Status = Mismatch
	}
	r.Checks = append(r.Checks, l)
}

// Failed reports whether any line or check mismatched.
func (r *Report) Failed() bool {
	return slices.ContainsFunc(append(slices.Clone(r.Lines), r.Checks...), func(l Line) bool { return l.Status == Mismatch })
}

// Pending reports whether anything waits on a later slice.
func (r *Report) Pending() bool {
	return slices.ContainsFunc(r.Lines, func(l Line) bool { return l.Status == Pending })
}

// Print writes the reconciliation.
func (r *Report) Print(w io.Writer) {
	sort.SliceStable(r.Lines, func(i, j int) bool { return r.Lines[i].Old < r.Lines[j].Old })
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "OLD TABLE\tROWS\tNEW TABLE\tROWS\tSTATUS\tNOTES")
	for _, l := range r.Lines {
		newRows := ""
		if l.New != "" {
			newRows = fmt.Sprint(l.TargetRows)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\n", l.Old, l.SourceRows, l.New, newRows, l.Status, strings.Join(l.Notes, "; "))
	}
	if len(r.Checks) > 0 {
		_, _ = fmt.Fprintln(tw, "\nCHECK\tWANT\t\tGOT\tSTATUS\t")
	}
	for _, c := range r.Checks {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t\t%d\t%s\t\n", c.Old, c.SourceRows, c.TargetRows, c.Status)
	}
	_ = tw.Flush()
}

// notCarried are old tables that are deliberately not imported: short-lived security state
// and logs. Everything else is carried or pending.
var notCarried = map[string]string{
	"auth_session":       "web sessions: adults sign in again",
	"auth_verification":  "short-lived verification tokens",
	"webauthn_challenge": "short-lived challenges",
	"device_nonce":       "replay-protection nonces",
	"command":            "the command log is dropped",
	"tv_prompt":          "pending TV prompts",
	"display_pairing":    "pairing offers",
	"device_web_session": "device web sessions",
}

// coverage adds every old table no mapping mentioned, and checks that each mapped table's
// columns are all accounted for.
func coverage(ctx context.Context, src pgx.Tx, r *Report) error {
	rows, err := src.Query(ctx, `SELECT table_name::text, column_name::text FROM information_schema.columns
		WHERE table_schema = $1 AND table_name <> 'schema_migrations' ORDER BY table_name, ordinal_position`, oldSchema)
	if err != nil {
		return safe(err)
	}
	type col struct{ table, column string }
	cols, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (col, error) {
		var c col
		return c, row.Scan(&c.table, &c.column)
	})
	if err != nil {
		return safe(err)
	}
	byTable := map[string][]string{}
	var tables []string
	for _, c := range cols {
		if _, seen := byTable[c.table]; !seen {
			tables = append(tables, c.table)
		}
		byTable[c.table] = append(byTable[c.table], c.column)
	}
	mentioned := map[string]bool{}
	for _, l := range r.Lines {
		mentioned[l.Old] = true
	}
	for _, t := range tables {
		if mentioned[t] {
			var missing []string
			for _, c := range byTable[t] {
				if !slices.Contains(r.columns[t], c) {
					missing = append(missing, c)
				}
			}
			if len(missing) > 0 {
				r.pending(t, "columns not mapped yet: "+strings.Join(missing, ", "))
			}
			continue
		}
		n, err := count(ctx, src, t)
		if err != nil {
			return err
		}
		l := r.line(t)
		l.SourceRows = n
		if reason, ok := notCarried[t]; ok {
			l.Status, l.Notes = NotCarried, []string{reason}
		} else if n > 0 {
			l.Status, l.Notes = Pending, []string{"no mapping yet"}
		} else {
			l.Notes = []string{"empty; no mapping yet"}
		}
	}
	return nil
}

// count returns the rows of an old table.
func count(ctx context.Context, src pgx.Tx, table string) (int64, error) {
	var n int64
	err := src.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{oldSchema, table}.Sanitize()).Scan(&n)
	return n, safe(err)
}

// countNew returns the rows of a target table.
func countNew(ctx context.Context, dst pgx.Tx, table string) (int64, error) {
	var n int64
	err := dst.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&n)
	return n, safe(err)
}
