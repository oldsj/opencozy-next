// Package push is the push outbox (design §8). A push is written to push_outbox in the same
// transaction as the event it reports, with a dedupe key, and sent after commit by the sender
// job: at least once. Rows hold names and task titles, so each row is deleted as soon as it is
// sent or finally refused. Nothing is enqueued when no push service (APNs) is configured.
//
// A row goes to one adult (all of their signed-in phones) or one child (all of their paired
// devices); the sender looks the phones and devices up when it sends, so a phone signed out in
// between gets nothing.
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/jobs"
	"github.com/oldsj/opencozy-next/internal/listen"
)

// Channel is the NOTIFY channel that wakes the sender.
const Channel = "push_outbox"

// MaxPayload caps a payload's JSON, as the table does.
const MaxPayload = 4096

// Message is one push to enqueue: to an adult's phones or to a child's devices.
type Message struct {
	HouseholdID uuid.UUID
	AdultID     *uuid.UUID
	ChildID     *uuid.UUID
	// DedupeKey makes a retried enqueue a no-op, such as "approval.pending/<id>/<asked at>".
	DedupeKey    string
	Notification Notification
}

// Outbox enqueues pushes. The zero value enqueues nothing (no push service configured).
type Outbox struct {
	Enabled bool
}

// Enqueue writes m with q, which must be the event's own transaction (command.Cmd.Q), and
// wakes the sender when that transaction commits.
func (o Outbox) Enqueue(ctx context.Context, q *dbq.Queries, m Message) error {
	if !o.Enabled {
		return nil
	}
	if (m.AdultID == nil) == (m.ChildID == nil) {
		return errors.New("push: a message goes to exactly one adult or one child")
	}
	if m.DedupeKey == "" || len(m.DedupeKey) > 200 {
		return errors.New("push: the dedupe key must be 1 to 200 bytes")
	}
	if m.Notification.Type == "" || m.Notification.Collapse == "" || len(m.Notification.Collapse) > 64 {
		return errors.New("push: a notification needs a type and a collapse ID of at most 64 bytes")
	}
	payload, err := json.Marshal(m.Notification)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if len(payload) > MaxPayload {
		return errors.New("push: the notification must be at most 4096 bytes of JSON")
	}
	if err := q.EnqueuePush(ctx, dbq.EnqueuePushParams{HouseholdID: m.HouseholdID, AdultID: m.AdultID, ChildID: m.ChildID, DedupeKey: m.DedupeKey, Payload: payload}); err != nil {
		return err
	}
	return q.NotifyPushOutbox(ctx)
}

// ErrRefused means the push service refused the message for good (an invalid token, a bad
// payload). The row is deleted and not retried. A Sender clears an invalid token itself.
var ErrRefused = errors.New("push: refused")

// Sender delivers one row, looking its recipients up with q (the job's fenced transaction).
// Any error other than ErrRefused is retried with a backoff.
type Sender interface {
	Send(ctx context.Context, q *dbq.Queries, row dbq.PushOutbox) error
}

// MaxAttempts is how many times a row is tried before it is dropped.
const MaxAttempts = 8

// batch is how many rows one transaction claims.
const batch = 50

// Job is the sender: on each NOTIFY push_outbox and every 5 s it claims due rows (SKIP LOCKED),
// sends them, and deletes or reschedules each, all inside the job's fenced transaction.
func Job(s Sender, notes <-chan listen.Note, log *slog.Logger) jobs.Job {
	wake := make(chan struct{}, 1)
	go func() {
		for range notes {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}()
	return jobs.Job{Name: "push-outbox", Every: 5 * time.Second, Timeout: time.Minute, Wake: wake,
		Run: func(ctx context.Context, f jobs.Fence) error { return send(ctx, f, s, log) }}
}

func send(ctx context.Context, f jobs.Fence, s Sender, log *slog.Logger) error {
	for {
		n := 0
		err := f.Tx(ctx, func(q *dbq.Queries) error {
			rows, err := q.ClaimPushes(ctx, batch)
			if err != nil {
				return err
			}
			n = len(rows)
			for _, row := range rows {
				sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := s.Send(sendCtx, q, row)
				cancel()
				switch {
				case err == nil, errors.Is(err, ErrRefused):
					if err != nil {
						log.WarnContext(ctx, "push refused", "id", row.ID)
					}
					err = q.DeletePush(ctx, row.ID)
				case row.Attempts+1 >= MaxAttempts:
					log.WarnContext(ctx, "push dropped after retries", "id", row.ID, "err", err)
					err = q.DeletePush(ctx, row.ID)
				default:
					// 10 s, 20 s, 40 s … up to an hour.
					delay := min(10*time.Second<<row.Attempts, time.Hour)
					err = q.RetryPush(ctx, dbq.RetryPushParams{ID: row.ID, DelaySeconds: delay.Seconds()})
				}
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || n < batch {
			return err
		}
	}
}
