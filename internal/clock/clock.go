// Package clock is where the application's time comes from: the database's clock, so replicas
// cannot disagree by their skew. Unit tests replace it with a Fake.
//
// A command reads the time once, after taking its locks (internal/command); a read that needs the
// time asks the same Clock.
package clock

import (
	"context"
	"sync"
	"time"
)

// Source reads the database clock (clock_timestamp()). *dbq.Queries is one, inside or outside a
// transaction.
type Source interface {
	Now(ctx context.Context) (time.Time, error)
}

// Clock gives the current time.
type Clock interface {
	Now(ctx context.Context, db Source) (time.Time, error)
}

// DB is the real clock: the database's.
type DB struct{}

// Now reads the database clock.
func (DB) Now(ctx context.Context, db Source) (time.Time, error) { return db.Now(ctx) }

// Fake is a clock that tests set and move. It never reads the database.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

// NewFake returns a Fake set to t.
func NewFake(t time.Time) *Fake { return &Fake{t: t} }

// Now returns the fake time.
func (f *Fake) Now(context.Context, Source) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t, nil
}

// Set moves the fake time to t.
func (f *Fake) Set(t time.Time) { f.mu.Lock(); f.t = t; f.mu.Unlock() }

// Advance moves the fake time forward by d.
func (f *Fake) Advance(d time.Duration) { f.mu.Lock(); f.t = f.t.Add(d); f.mu.Unlock() }
