package command_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/clock"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/testdb"
)

// A recorded answer never changes; a row goes only once it has expired.
func TestCommandLogIsWriteOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := t.Context()
	var house uuid.UUID
	if err := db.Owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Fictional', 'UTC') RETURNING id`).Scan(&house); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		key string
		age string
	}{{"fresh", "1 day"}, {"old", "31 days"}} {
		if _, err := db.Owner.Exec(ctx, `INSERT INTO command_log (household_id, idempotency_key, request_hash, status, content_type, body, created_at)
			VALUES ($1, $2, $3, 201, 'application/json', '\x7b7d', clock_timestamp() - $4::interval)`, house, k.key, make([]byte, 32), k.age); err != nil {
			t.Fatal(err)
		}
	}
	for _, sql := range []string{
		`UPDATE command_log SET status = 200`,
		`DELETE FROM command_log WHERE idempotency_key = 'fresh'`,
		`TRUNCATE command_log`,
	} {
		if _, err := db.Runtime.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "command_log") {
			t.Errorf("runtime ran %q: %v", sql, err)
		}
	}
	n, err := dbq.New(db.Runtime).DeleteExpiredCommands(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expired rows deleted: %d, %v", n, err)
	}
	var left []string
	rows, _ := db.Owner.Query(ctx, `SELECT idempotency_key FROM command_log`)
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		left = append(left, k)
	}
	if rows.Err() != nil || len(left) != 1 || left[0] != "fresh" {
		t.Fatalf("left %v, %v", left, rows.Err())
	}
}

// A job takes the same locks as a request, and its time comes from the injected clock, read
// once.
func TestLockForJobs(t *testing.T) {
	db := testdb.Open(t)
	ctx := t.Context()
	var house, child uuid.UUID
	if err := db.Owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Fictional', 'UTC') RETURNING id`).Scan(&house); err != nil {
		t.Fatal(err)
	}
	if err := db.Owner.QueryRow(ctx, `INSERT INTO child (household_id, name) VALUES ($1, 'Ash') RETURNING id`, house).Scan(&child); err != nil {
		t.Fatal(err)
	}
	fake := clock.NewFake(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC))
	tx, err := db.Runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // test cleanup
	c, err := command.Lock(ctx, tx, house, fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.LockChildren(ctx, child, child); err != nil || !c.Holds(child) || c.Holds(uuid.New()) {
		t.Fatalf("lock children: %v", err)
	}
	if err := c.LockChildren(ctx, uuid.New()); !errors.Is(err, command.ErrNoChild) {
		t.Fatalf("lock a stranger: %v", err)
	}
	now, _ := c.Now(ctx)
	fake.Advance(time.Hour)
	again, _ := c.Now(ctx)
	if !now.Equal(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)) || !again.Equal(now) {
		t.Fatalf("now %v then %v", now, again)
	}
	if err := c.LockChildren(ctx, child); err == nil {
		t.Fatal("a lock after Now was allowed")
	}
	// Another transaction waits for the household while the job holds it.
	other, err := db.Runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback(ctx) //nolint:errcheck // test cleanup
	if _, err := other.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Lock(ctx, other, house, nil); err == nil || !strings.Contains(err.Error(), "lock timeout") {
		t.Fatalf("second lock: %v", err)
	}
}

// AfterCommit hooks run once the command committed with a 2xx answer, after the response, and
// never when it rolled back, was refused or was replayed.
func TestAfterCommit(t *testing.T) {
	db := testdb.Open(t)
	ctx := t.Context()
	var house uuid.UUID
	if err := db.Owner.QueryRow(ctx, `INSERT INTO household (name, time_zone) VALUES ('Fictional', 'UTC') RETURNING id`).Scan(&house); err != nil {
		t.Fatal(err)
	}
	rn := command.Runner{Pool: db.Runtime, Log: slog.New(slog.DiscardHandler)}
	ran := 0
	adult := uuid.New()
	handler := func(status int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cmd, _ := command.From(r.Context())
			cmd.AfterCommit(func(ctx context.Context) {
				// The household lock is free again: another transaction takes it at once.
				tx, err := db.Runtime.Begin(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				defer tx.Rollback(ctx) //nolint:errcheck // test cleanup
				if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '1s'`); err != nil {
					t.Error(err)
					return
				}
				if _, err := tx.Exec(ctx, `SELECT id FROM household WHERE id = $1 FOR UPDATE`, house); err != nil {
					t.Errorf("household still locked: %v", err)
				}
				ran++
			})
			w.WriteHeader(status)
		})
	}
	send := func(key string, status int) int {
		r := httptest.NewRequest(http.MethodPost, "/api/x", nil).WithContext(actor.With(ctx, actor.Actor{Kind: actor.Adult, HouseholdID: house, AdultID: adult}))
		r.Header.Set(command.KeyHeader, key)
		w := httptest.NewRecorder()
		rn.Wrap(command.Spec{Op: "x", RecordResponse: true}, handler(status), w, r)
		return w.Code
	}
	if code := send("k-fail", http.StatusInternalServerError); code != 500 || ran != 0 {
		t.Fatalf("rolled back: %d, hooks ran %d", code, ran)
	}
	if code := send("k-conflict", http.StatusConflict); code != 409 || ran != 0 {
		t.Fatalf("refused: %d, hooks ran %d", code, ran)
	}
	if code := send("k-ok", http.StatusNoContent); code != 204 || ran != 1 {
		t.Fatalf("committed: %d, hooks ran %d", code, ran)
	}
	if code := send("k-ok", http.StatusNoContent); code != 204 || ran != 1 {
		t.Fatalf("replayed: %d, hooks ran %d", code, ran)
	}
}
