package push

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/jobs"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func household(t *testing.T, owner *pgxpool.Pool) (h, adult uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Example', 'Europe/Amsterdam') RETURNING id`).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `INSERT INTO adult (household_id, email, name, webauthn_user_handle)
		VALUES ($1, 'alex@example.test', 'Alex', '\x01') RETURNING id`, h).Scan(&adult); err != nil {
		t.Fatal(err)
	}
	return h, adult
}

func rows(t *testing.T, pool *pgxpool.Pool) []dbq.PushOutbox {
	t.Helper()
	r, err := pool.Query(context.Background(), "SELECT * FROM push_outbox ORDER BY dedupe_key")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var out []dbq.PushOutbox
	for r.Next() {
		var p dbq.PushOutbox
		if err := r.Scan(&p.ID, &p.HouseholdID, &p.AdultID, &p.ChildID, &p.DedupeKey, &p.Payload, &p.Attempts, &p.NextAttemptAt, &p.CreatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestEnqueue(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h, adult := household(t, db.Owner)
	q := dbq.New(db.Runtime)
	m := Message{HouseholdID: h, AdultID: &adult, DedupeKey: "approval/1", Payload: map[string]any{"title": "Feed the cat"}}

	// Without a push service nothing is written.
	if err := (Outbox{}).Enqueue(ctx, q, m); err != nil || len(rows(t, db.Runtime)) != 0 {
		t.Fatalf("disabled: %v", err)
	}
	on := Outbox{Enabled: true}
	for range 2 { // the same dedupe key twice is one row
		if err := on.Enqueue(ctx, q, m); err != nil {
			t.Fatal(err)
		}
	}
	if got := rows(t, db.Runtime); len(got) != 1 {
		t.Fatalf("rows: %d", len(got))
	}
	for name, bad := range map[string]Message{
		"no recipient":   {HouseholdID: h, DedupeKey: "k", Payload: map[string]any{}},
		"two recipients": {HouseholdID: h, AdultID: &adult, ChildID: &adult, DedupeKey: "k", Payload: map[string]any{}},
		"no key":         {HouseholdID: h, AdultID: &adult, Payload: map[string]any{}},
		"no payload":     {HouseholdID: h, AdultID: &adult, DedupeKey: "k"},
		"too large":      {HouseholdID: h, AdultID: &adult, DedupeKey: "k", Payload: map[string]any{"x": string(make([]byte, MaxPayload))}},
	} {
		if err := on.Enqueue(ctx, q, bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A push enqueued in a transaction that rolls back is never sent.
func TestEnqueueFollowsTheTransaction(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h, adult := household(t, db.Owner)
	tx, err := db.Runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Outbox{Enabled: true}).Enqueue(ctx, dbq.New(tx), Message{HouseholdID: h, AdultID: &adult, DedupeKey: "k", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	if len(rows(t, db.Runtime)) != 0 {
		t.Fatal("a rolled-back push was kept")
	}
}

type fakeSender struct {
	mu     sync.Mutex
	answer map[string]error
	sent   []string
}

func (f *fakeSender) Send(_ context.Context, row dbq.PushOutbox) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, row.DedupeKey)
	return f.answer[row.DedupeKey]
}

func TestSenderJob(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	h, adult := household(t, db.Owner)
	q := dbq.New(db.Runtime)
	on := Outbox{Enabled: true}
	for _, k := range []string{"ok", "refused", "flaky", "last-try"} {
		if err := on.Enqueue(ctx, q, Message{HouseholdID: h, AdultID: &adult, DedupeKey: k, Payload: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Owner.Exec(ctx, "UPDATE push_outbox SET attempts = $1 WHERE dedupe_key = 'last-try'", MaxAttempts-1); err != nil {
		t.Fatal(err)
	}
	transient := errors.New("apns: 503")
	s := &fakeSender{answer: map[string]error{"refused": ErrRefused, "flaky": transient, "last-try": transient}}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { jobs.Runner{Pool: db.Runtime, Log: quiet}.Run(runCtx, Job(s, nil, quiet)); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		left := rows(t, db.Runtime)
		if len(left) == 1 && left[0].DedupeKey == "flaky" && left[0].Attempts == 1 {
			if !left[0].NextAttemptAt.After(time.Now()) {
				t.Fatal("a failed push was not rescheduled")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox never settled: %+v", left)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
}
