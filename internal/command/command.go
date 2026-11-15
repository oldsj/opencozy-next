// Package command runs state-changing requests (operations marked x-command) the same way
// every time:
//
//  1. The Idempotency-Key header is required. A retry of a recorded command gets the recorded
//     answer. The key is checked against a hash of the actor, the operation and the body, so the
//     same key for a different request is refused.
//  2. One READ COMMITTED transaction.
//  3. Locks in a fixed order: the household row, then children by id (LockChildren), then other
//     rows.
//  4. Right after the household lock the actor's own credential is checked again (Spec.Recheck),
//     so a revocation that committed while the command waited stops it. The handler then
//     re-validates its rows under the locks and takes the time from Now, which reads the
//     database clock after the locks are held.
//  5. The handler runs inside a SAVEPOINT.
//  6. A 2xx commits. A 404 or 409 rolls back to the savepoint, so nothing changes, and is
//     recorded. Anything else rolls back the whole transaction and is not recorded. An operation
//     whose response holds a secret records only that it succeeded (Spec.RecordResponse).
//  7. Recorded answers expire after 30 days (DeleteExpired).
package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/httpx"
)

// KeyHeader carries the client's idempotency key.
const KeyHeader = "Idempotency-Key"

// Cmd is the running command, reachable from the handler's context.
type Cmd struct {
	Q           *dbq.Queries
	householdID uuid.UUID
	locked      []uuid.UUID
	now         time.Time
}

type key struct{}

// From returns the running command. Handlers of x-command operations always have one.
func From(ctx context.Context) (*Cmd, bool) {
	c, ok := ctx.Value(key{}).(*Cmd)
	return c, ok
}

// ErrNoChild means a child to lock is not a live child of the household.
var ErrNoChild = errors.New("command: no such child")

// LockChildren locks live children of the household, in id order, so every command takes child
// locks in the same order. Call it at most once, before Now.
func (c *Cmd) LockChildren(ctx context.Context, ids ...uuid.UUID) error {
	if !c.now.IsZero() {
		return errors.New("command: lock taken after Now")
	}
	ids = slices.Clone(ids)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	ids = slices.Compact(ids)
	for _, id := range ids {
		if _, err := c.Q.LockLiveChild(ctx, dbq.LockLiveChildParams{HouseholdID: c.householdID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNoChild
		} else if err != nil {
			return err
		}
	}
	c.locked = append(c.locked, ids...)
	return nil
}

// Now is the command's time: the database clock, read once, after the locks are held.
func (c *Cmd) Now(ctx context.Context) (time.Time, error) {
	if c.now.IsZero() {
		t, err := c.Q.Now(ctx)
		if err != nil {
			return time.Time{}, err
		}
		c.now = t
	}
	return c.now, nil
}

// Spec is how one operation runs as a command.
type Spec struct {
	Op string
	// RecordResponse is false when the response holds a secret: a success is recorded as a
	// 409 telling a retry that the request already succeeded, never with the body.
	RecordResponse bool
	// Recheck validates the actor again inside the transaction, after the household lock, and
	// returns its current state. When the actor is no longer allowed it writes the refusal and
	// returns false.
	Recheck func(ctx context.Context, q *dbq.Queries, w http.ResponseWriter, a actor.Actor) (actor.Actor, bool)
}

// Runner runs commands.
type Runner struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
}

// Wrap runs next as the command spec. The actor must already be on the context: this runs after
// authorization.
func (rn Runner) Wrap(spec Spec, next http.Handler, w http.ResponseWriter, r *http.Request) {
	op := spec.Op
	a, ok := actor.From(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
		return
	}
	idem := r.Header.Get(KeyHeader)
	if idem == "" || len(idem) > 128 {
		httpx.WriteError(w, http.StatusBadRequest, "idempotency_key_required", "Send an Idempotency-Key header of 1 to 128 characters with every change.")
		return
	}
	body, err := io.ReadAll(r.Body) // already capped by the operation's body limit
	if err != nil {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "The request body is too large.")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	hash := requestHash(a, op, r, body)

	ctx := r.Context()
	fail := func(err error) {
		rn.Log.ErrorContext(ctx, "command failed", "op", op, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	tx, err := rn.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		fail(err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := dbq.New(tx)
	if _, err := q.LockHousehold(ctx, a.HouseholdID); err != nil {
		fail(err)
		return
	}
	if spec.Recheck != nil {
		fresh, ok := spec.Recheck(ctx, q, w, a)
		if !ok {
			return // refused; the deferred rollback ends the transaction
		}
		a = fresh
		ctx = actor.With(ctx, a)
	}
	{
		prev, err := q.GetCommand(ctx, dbq.GetCommandParams{HouseholdID: a.HouseholdID, IdempotencyKey: idem})
		switch {
		case err == nil && subtle.ConstantTimeCompare(prev.RequestHash, hash) == 1:
			w.Header().Set("Content-Type", prev.ContentType)
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(int(prev.Status))
			_, _ = w.Write(prev.Body)
			return
		case err == nil:
			httpx.WriteError(w, http.StatusUnprocessableEntity, "idempotency_key_reused", "This Idempotency-Key was used for a different request.")
			return
		case !errors.Is(err, pgx.ErrNoRows):
			fail(err)
			return
		}
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT command"); err != nil {
		fail(err)
		return
	}

	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(rec, r.WithContext(context.WithValue(ctx, key{}, &Cmd{Q: q, householdID: a.HouseholdID})))

	switch {
	case rec.status >= 200 && rec.status < 300:
	case rec.status == http.StatusNotFound || rec.status == http.StatusConflict:
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT command"); err != nil {
			fail(err)
			return
		}
	default:
		_ = tx.Rollback(ctx)
		rec.flush(w)
		return
	}
	// A 204 has no body; pgx would send a nil slice as NULL.
	status, contentType, body := rec.status, rec.header.Get("Content-Type"), append([]byte{}, rec.body.Bytes()...)
	if !spec.RecordResponse && status >= 200 && status < 300 {
		status, contentType = http.StatusConflict, "application/json"
		body = []byte(`{"error":{"code":"already_done","message":"This was already done. Its secret is shown only once: revoke it and create another if it was lost."}}` + "\n")
	}
	if err := q.RecordCommand(ctx, dbq.RecordCommandParams{
		HouseholdID: a.HouseholdID, IdempotencyKey: idem, RequestHash: hash,
		Status: int32(status), ContentType: contentType, Body: body, //nolint:gosec // an HTTP status
	}); err != nil {
		fail(err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		fail(err)
		return
	}
	rec.flush(w)
}

// requestHash binds a recorded answer to who asked, what they asked for and the exact body.
func requestHash(a actor.Actor, op string, r *http.Request, body []byte) []byte {
	h := sha256.New()
	field := func(b []byte) {
		_ = binary.Write(h, binary.BigEndian, uint32(len(b))) //nolint:gosec // bounded by the body cap
		h.Write(b)
	}
	field([]byte(a.Kind))
	field(a.AdultID[:])
	field(a.CredentialID[:])
	field(a.TokenHash)
	field([]byte(op))
	field([]byte(r.URL.Path))
	field([]byte(r.URL.RawQuery))
	field(body)
	return h.Sum(nil)
}

// recorder holds the handler's response until the transaction's outcome is known.
type recorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(s int) {
	if !r.wrote {
		r.status, r.wrote = s, true
	}
}
func (r *recorder) Write(b []byte) (int, error) { r.WriteHeader(http.StatusOK); return r.body.Write(b) }

func (r *recorder) flush(w http.ResponseWriter) {
	for k, v := range r.header {
		w.Header()[k] = v
	}
	w.WriteHeader(r.status)
	_, _ = w.Write(r.body.Bytes())
}

// DeleteExpired removes recorded answers older than 30 days, every hour until ctx ends.
func DeleteExpired(ctx context.Context, q *dbq.Queries, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if _, err := q.DeleteExpiredCommands(ctx); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "command log sweep", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
