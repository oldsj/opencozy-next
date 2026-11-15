// Package jobs runs background work on one replica at a time (design §8).
//
// Each job holds a lease, a row in job_lease(name, holder, expires_at). The runner takes it when
// it is free or expired, and renews it at a third of its life. Leases are fenced: a job writes
// only inside Fence.Tx, which first re-reads the lease as this holder, unexpired, FOR SHARE. A
// replica that lost its lease (a long pause, a network split) therefore commits nothing, and
// while a fenced transaction is open no other replica can take the lease over. Every job must
// still be idempotent: a run can be cut off after its work committed but before it finished.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// ErrLeaseLost means this replica no longer holds the job's lease. The job should stop.
var ErrLeaseLost = errors.New("jobs: lease lost")

// DefaultTTL is how long a lease lasts without renewal.
const DefaultTTL = 30 * time.Second

// Job is one kind of background work.
type Job struct {
	// Name is the lease's key: lowercase letters, digits and dashes.
	Name string
	// Every is the time between runs.
	Every time.Duration
	// Wake, when set, starts a run early (for example on a NOTIFY).
	Wake <-chan struct{}
	// Timeout caps one run. Zero means Every.
	Timeout time.Duration
	// Run does the work. It writes only through f.Tx.
	Run func(ctx context.Context, f Fence) error
}

// Fence runs transactions that hold the job's lease.
type Fence struct {
	pool   *pgxpool.Pool
	name   string
	holder string
}

// Tx runs fn in a transaction that first checks the lease. It returns ErrLeaseLost, without
// calling fn, when the lease has expired or another replica holds it.
func (f Fence) Tx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	return pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		if _, err := q.FenceLease(ctx, dbq.FenceLeaseParams{Name: f.name, Holder: f.holder}); errors.Is(err, pgx.ErrNoRows) {
			return ErrLeaseLost
		} else if err != nil {
			return err
		}
		return fn(q)
	})
}

// Runner runs jobs.
type Runner struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
	// Holder names this replica in job_lease. Empty means the host name plus a random suffix.
	Holder string
	// TTL is the lease's life. Zero means DefaultTTL.
	TTL time.Duration
}

// NewHolder returns a holder name unique to this process: the host (the pod's name) and a random
// suffix, so a restarted pod with the same name is a different holder.
func NewHolder() string {
	host, _ := os.Hostname()
	if len(host) > 100 {
		host = host[:100]
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return host + "-" + hex.EncodeToString(b)
}

// Run runs every job until ctx ends, then releases the leases it holds.
func (r Runner) Run(ctx context.Context, jobs ...Job) {
	if r.Holder == "" {
		r.Holder = NewHolder()
	}
	if r.TTL == 0 {
		r.TTL = DefaultTTL
	}
	done := make(chan struct{})
	for _, j := range jobs {
		go func() { r.loop(ctx, j); done <- struct{}{} }()
	}
	for range jobs {
		<-done
	}
}

func (r Runner) loop(ctx context.Context, j Job) {
	log := r.Log.With("job", j.Name)
	f := Fence{pool: r.Pool, name: j.Name, holder: r.Holder}
	q := dbq.New(r.Pool)
	next := time.Now()
	held := false
	for {
		var err error
		if held, err = r.take(ctx, q, j.Name); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "job lease", "err", err)
		}
		if held && !time.Now().Before(next) {
			timeout := j.Timeout
			if timeout == 0 {
				timeout = j.Every
			}
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			if err := j.Run(runCtx, f); err != nil && !errors.Is(err, ErrLeaseLost) && ctx.Err() == nil {
				log.ErrorContext(ctx, "job failed", "err", err)
			}
			cancel()
			next = time.Now().Add(j.Every)
		}
		wait := r.TTL / 3
		if held {
			wait = min(wait, max(time.Until(next), 0))
		}
		select {
		case <-ctx.Done():
			// Always release: a renewal cut off by the cancel may still have committed. The
			// delete matches only this holder's row.
			release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = q.ReleaseLease(release, dbq.ReleaseLeaseParams{Name: j.Name, Holder: r.Holder})
			cancel()
			return
		case <-j.Wake:
			next = time.Now()
		case <-time.After(wait):
		}
	}
}

// take takes or renews the lease and reports whether this replica holds it.
func (r Runner) take(ctx context.Context, q *dbq.Queries, name string) (bool, error) {
	_, err := q.TakeLease(ctx, dbq.TakeLeaseParams{Name: name, Holder: r.Holder, TtlSeconds: r.TTL.Seconds()})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("take %s: %w", name, err)
	}
	return true, nil
}
