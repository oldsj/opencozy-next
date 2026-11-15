// Package listen holds one PostgreSQL connection per replica that LISTENs on a fixed set of
// channels and hands notifications to Go channels. NOTIFY is delivered only to sessions that were
// listening when the sender committed, so after a reconnect each subscriber gets a Resync
// notification: it may have missed some, and must catch up from the tables.
package listen

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Note is one notification.
type Note struct {
	Payload string
	// Resync means notifications may have been missed (first connect or a reconnect).
	Resync bool
}

// Listener fans notifications out to subscribers.
type Listener struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	subs map[string]chan Note
}

// New makes a Listener for channels. Each channel gets a buffered Go channel; when a subscriber
// falls behind, notes are dropped and a Resync is queued in their place.
func New(pool *pgxpool.Pool, log *slog.Logger, channels ...string) *Listener {
	l := &Listener{pool: pool, log: log, subs: map[string]chan Note{}}
	for _, c := range channels {
		l.subs[c] = make(chan Note, 256)
	}
	return l
}

// C returns the notes for channel. It is nil for a channel the Listener was not made with.
func (l *Listener) C(channel string) <-chan Note { return l.subs[channel] }

// Run listens until ctx ends, reconnecting with a backoff of up to 30 s.
func (l *Listener) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := l.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		l.log.WarnContext(ctx, "listen: connection lost", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (l *Listener) listen(ctx context.Context) error {
	pc, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection has session state (LISTEN), so it never goes back to the pool.
	conn := pc.Hijack()
	defer conn.Close(context.WithoutCancel(ctx)) //nolint:errcheck // closing a dead connection
	for c := range l.subs {
		if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{c}.Sanitize()); err != nil {
			return err
		}
	}
	for _, ch := range l.subs {
		l.send(ch, Note{Resync: true})
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if ch, ok := l.subs[n.Channel]; ok {
			l.send(ch, Note{Payload: n.Payload})
		}
	}
}

// send never blocks: a full buffer is replaced by one Resync.
func (l *Listener) send(ch chan Note, n Note) {
	select {
	case ch <- n:
		return
	default:
	}
drain:
	for {
		select {
		case <-ch:
		default:
			break drain
		}
	}
	select {
	case ch <- Note{Resync: true}:
	default:
	}
}
