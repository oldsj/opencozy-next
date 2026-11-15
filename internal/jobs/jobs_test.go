package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// Two replicas, one job: only the lease holder runs it.
func TestOnlyTheHolderRuns(t *testing.T) {
	db := testdb.Open(t)
	var a, b atomic.Int32
	job := func(n *atomic.Int32) Job {
		return Job{Name: "tick", Every: 20 * time.Millisecond, Run: func(ctx context.Context, f Fence) error {
			return f.Tx(ctx, func(*dbq.Queries) error { n.Add(1); return nil })
		}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { Runner{Pool: db.Runtime, Log: quiet, Holder: "a", TTL: 3 * time.Second}.Run(ctx, job(&a)) })
	time.Sleep(100 * time.Millisecond) // a takes the lease first
	wg.Go(func() { Runner{Pool: db.Runtime, Log: quiet, Holder: "b", TTL: 3 * time.Second}.Run(ctx, job(&b)) })
	time.Sleep(400 * time.Millisecond)
	cancel()
	wg.Wait()
	if a.Load() < 1 || b.Load() != 0 {
		t.Fatalf("runs: a=%d b=%d; want only a", a.Load(), b.Load())
	}
	// Stopping releases the lease, so another replica takes over at once.
	var held int
	if err := db.Owner.QueryRow(context.Background(), "SELECT count(*) FROM job_lease").Scan(&held); err != nil || held != 0 {
		t.Fatalf("leases after stop: %d, %v", held, err)
	}
}

// A replica whose lease was taken over commits nothing.
func TestFenceRefusesALostLease(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	q := dbq.New(db.Runtime)
	r := Runner{Pool: db.Runtime, Log: quiet, Holder: "a", TTL: time.Minute}
	if held, err := r.take(ctx, q, "tick"); !held || err != nil {
		t.Fatalf("take: %v %v", held, err)
	}
	f := Fence{pool: db.Runtime, name: "tick", holder: "a"}
	ran := false
	if err := f.Tx(ctx, func(*dbq.Queries) error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("holder: ran=%v err=%v", ran, err)
	}

	// b cannot take a live lease.
	b := Runner{Pool: db.Runtime, Log: quiet, Holder: "b", TTL: time.Minute}
	if held, err := b.take(ctx, q, "tick"); held || err != nil {
		t.Fatalf("b took a live lease: %v %v", held, err)
	}
	// Once it expires, b takes it, and a's next transaction is refused before it runs.
	if _, err := db.Owner.Exec(ctx, "UPDATE job_lease SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if held, err := b.take(ctx, q, "tick"); !held || err != nil {
		t.Fatalf("b: %v %v", held, err)
	}
	ran = false
	if err := f.Tx(ctx, func(*dbq.Queries) error { ran = true; return nil }); !errors.Is(err, ErrLeaseLost) || ran {
		t.Fatalf("after takeover: ran=%v err=%v", ran, err)
	}
	// An expired lease is lost too, even before anyone takes it.
	if _, err := db.Owner.Exec(ctx, "UPDATE job_lease SET holder = 'a', expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := f.Tx(ctx, func(*dbq.Queries) error { return nil }); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired: %v", err)
	}
}

// While a fenced transaction is open, the lease cannot be taken over, even once it expires: the
// takeover waits for the transaction to end.
func TestFenceBlocksTakeover(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	q := dbq.New(db.Runtime)
	a := Runner{Pool: db.Runtime, Log: quiet, Holder: "a", TTL: 500 * time.Millisecond}
	if held, err := a.take(ctx, q, "tick"); !held || err != nil {
		t.Fatalf("take: %v %v", held, err)
	}
	inside, finish := make(chan struct{}), make(chan struct{})
	var ended atomic.Int64
	go func() {
		_ = Fence{pool: db.Runtime, name: "tick", holder: "a"}.Tx(ctx, func(*dbq.Queries) error {
			close(inside)
			<-finish
			ended.Store(time.Now().UnixNano())
			return nil
		})
	}()
	<-inside
	time.Sleep(700 * time.Millisecond) // the lease has expired
	took := make(chan time.Time)
	go func() {
		b := Runner{Pool: db.Runtime, Log: quiet, Holder: "b", TTL: time.Minute}
		held, err := b.take(ctx, q, "tick")
		if !held || err != nil {
			t.Errorf("b: %v %v", held, err)
		}
		took <- time.Now()
	}()
	select {
	case <-took:
		t.Fatal("b took the lease while a's fenced transaction was open")
	case <-time.After(300 * time.Millisecond):
	}
	close(finish)
	at := <-took
	if at.UnixNano() < ended.Load() {
		t.Fatal("b took the lease before a's transaction ended")
	}
}

func TestWakeRunsEarly(t *testing.T) {
	db := testdb.Open(t)
	wake := make(chan struct{}, 1)
	runs := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Runner{Pool: db.Runtime, Log: quiet}.Run(ctx, Job{Name: "woken", Every: time.Hour, Wake: wake,
		Run: func(context.Context, Fence) error { runs <- struct{}{}; return nil }})
	<-runs // the first run is immediate
	wake <- struct{}{}
	select {
	case <-runs:
	case <-time.After(5 * time.Second):
		t.Fatal("a wake did not start a run")
	}
}
