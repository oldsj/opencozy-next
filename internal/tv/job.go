package tv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/clock"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/jobs"
)

// SettleEvery is how often the settlement job runs. Reads never depend on it (they compute what
// has run out); it records ends in history and tells live clients soon after a session ends.
const SettleEvery = 15 * time.Second

// Txer runs a transaction; jobs.Fence is one (a test passes a plain pool).
type Txer interface {
	Tx(ctx context.Context, fn func(q *dbq.Queries) error) error
}

// Job settles every household with an open session or a pending prompt.
func (d Domain) Job(clk clock.Clock) jobs.Job {
	return jobs.Job{Name: "tv-settle", Every: SettleEvery, Run: func(ctx context.Context, f jobs.Fence) error {
		return d.SettleAll(ctx, f, clk)
	}}
}

// SettleAll settles each household with TV work, one transaction each, under its household lock
// (the order a command takes). One household failing doesn't hold up the others: its error is
// returned with theirs at the end. Losing the lease or the context stops the run. A nil clk is
// the database clock.
func (d Domain) SettleAll(ctx context.Context, f Txer, clk clock.Clock) error {
	if clk == nil {
		clk = clock.DB{}
	}
	var households []uuid.UUID
	if err := f.Tx(ctx, func(q *dbq.Queries) (err error) {
		households, err = q.ListTvWorkHouseholds(ctx)
		return err
	}); err != nil {
		return err
	}
	var failed []error
	for _, h := range households {
		if err := f.Tx(ctx, func(q *dbq.Queries) error {
			if _, err := q.LockHousehold(ctx, h); err != nil {
				return err
			}
			now, err := clk.Now(ctx, q)
			if err != nil {
				return err
			}
			return d.SettleHousehold(ctx, q, h, now)
		}); err != nil {
			if errors.Is(err, jobs.ErrLeaseLost) || ctx.Err() != nil {
				return err
			}
			failed = append(failed, fmt.Errorf("household %s: %w", h, err))
		}
	}
	return errors.Join(failed...)
}
