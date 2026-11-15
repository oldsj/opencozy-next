package keyring

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RekeyFunc re-encrypts one column's values under the first key (Keyring.Rekey) and, for feed
// URLs, recomputes the MAC column (FeedMAC), returning how many rows changed. It must be safe
// to run again after a partial run: rows already under the first key are left alone.
type RekeyFunc func(ctx context.Context, db *pgxpool.Pool, kr *Keyring) (changed int, err error)

var (
	mu      sync.Mutex
	columns []column
)

type column struct {
	name string
	run  RekeyFunc
}

// Register adds a column to `app rekey`. Each slice that stores an encrypted column registers
// it from an init function, named "table.column".
func Register(name string, run RekeyFunc) {
	mu.Lock()
	defer mu.Unlock()
	for _, c := range columns {
		if c.name == name {
			panic("keyring: column " + name + " registered twice")
		}
	}
	columns = append(columns, column{name: name, run: run})
}

// RekeyAll runs every registered column, for `app rekey`. It stops at the first failure, so a
// rotation is finished only when it returns nil; then the old keys can be dropped.
func RekeyAll(ctx context.Context, db *pgxpool.Pool, kr *Keyring, log *slog.Logger) error {
	if _, err := kr.current(); err != nil {
		return err
	}
	mu.Lock()
	cols := append([]column(nil), columns...)
	mu.Unlock()
	log.Info("rekey: start", "keyring", kr, "columns", len(cols))
	for _, c := range cols {
		n, err := c.run(ctx, db, kr)
		if err != nil {
			return fmt.Errorf("rekey %s: %w", c.name, err)
		}
		log.Info("rekey: column done", "column", c.name, "changed", n)
	}
	if len(cols) == 0 {
		log.Info("rekey: no encrypted columns yet")
	}
	return nil
}
