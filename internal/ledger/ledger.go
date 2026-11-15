// Package ledger writes points. Every change to a child's points is a row in point_ledger,
// written in the same transaction as the state change it pays for, under the child's lock:
//
//   - Award pays for a source (a chore occurrence, say) once. active_award holds the current
//     award, so a second award for the same source is refused until the first is reversed.
//   - Reverse undoes the current award for a source; the source can then be awarded again.
//   - Redeem spends points; Refund gives a redemption back.
//
// A balance never goes below zero: Redeem and Reverse check it under the child's lock. The ledger
// is append-only (database triggers), so a mistake is corrected with a new row, never an edit.
package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Kinds of ledger row.
const (
	KindAward      = "award"
	KindReversal   = "reversal"
	KindRedemption = "redemption"
	KindRefund     = "refund"
)

var (
	// ErrNotLocked means the command does not hold the child's lock (command.Cmd.LockChildren).
	ErrNotLocked = errors.New("ledger: child not locked")
	// ErrAlreadyAwarded means the source has an award that has not been reversed.
	ErrAlreadyAwarded = errors.New("ledger: already awarded")
	// ErrNotAwarded means the source has no award to reverse.
	ErrNotAwarded = errors.New("ledger: not awarded")
	// ErrAlreadyRefunded means the redemption was already refunded.
	ErrAlreadyRefunded = errors.New("ledger: already refunded")
	// ErrInsufficient means the change would take the child's balance below zero.
	ErrInsufficient = errors.New("ledger: not enough points")
	// ErrAmount means the amount is not a positive number of points.
	ErrAmount = errors.New("ledger: amount must be positive")
)

// MaxAmount is the most points one row may move.
const MaxAmount = 1_000_000

// Entry is one change of points.
type Entry struct {
	Child      uuid.UUID
	Points     int32 // always positive; the kind sets the sign
	SourceKind string
	SourceID   uuid.UUID
	// Who caused it; both nil for a job.
	AdultID      *uuid.UUID
	CredentialID *uuid.UUID
	Note         *string
}

// Balance is the child's balance. Read it under the child's lock when deciding on a change.
func Balance(ctx context.Context, q *dbq.Queries, householdID, child uuid.UUID) (int64, error) {
	return q.ChildBalance(ctx, dbq.ChildBalanceParams{HouseholdID: householdID, ChildID: child})
}

// Award pays e.Points for the source, once.
func Award(ctx context.Context, c *command.Cmd, e Entry) (dbq.PointLedger, error) {
	if err := check(c, e); err != nil {
		return dbq.PointLedger{}, err
	}
	_, err := c.Q.GetActiveAward(ctx, dbq.GetActiveAwardParams{HouseholdID: c.HouseholdID(), SourceKind: e.SourceKind, SourceID: e.SourceID})
	switch {
	case err == nil:
		return dbq.PointLedger{}, ErrAlreadyAwarded
	case !errors.Is(err, pgx.ErrNoRows):
		return dbq.PointLedger{}, err
	}
	row, err := insert(ctx, c, e, KindAward, e.Points, nil)
	if err != nil {
		return dbq.PointLedger{}, err
	}
	// The child's lock serializes awards for its sources; the key is the backstop.
	if err := c.Q.InsertActiveAward(ctx, dbq.InsertActiveAwardParams{
		HouseholdID: c.HouseholdID(), SourceKind: e.SourceKind, SourceID: e.SourceID, LedgerID: row.ID,
	}); err != nil {
		return dbq.PointLedger{}, fmt.Errorf("ledger: active award: %w", err)
	}
	return row, nil
}

// Reverse undoes the source's current award. e.Points is ignored: a reversal takes back exactly
// what was awarded. It is refused when the child has already spent those points.
func Reverse(ctx context.Context, c *command.Cmd, e Entry) (dbq.PointLedger, error) {
	if !c.Holds(e.Child) {
		return dbq.PointLedger{}, ErrNotLocked
	}
	id, err := c.Q.DeleteActiveAward(ctx, dbq.DeleteActiveAwardParams{HouseholdID: c.HouseholdID(), SourceKind: e.SourceKind, SourceID: e.SourceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.PointLedger{}, ErrNotAwarded
	} else if err != nil {
		return dbq.PointLedger{}, err
	}
	award, err := c.Q.GetLedgerEntry(ctx, dbq.GetLedgerEntryParams{HouseholdID: c.HouseholdID(), ID: id})
	if err != nil {
		return dbq.PointLedger{}, err
	}
	if award.ChildID != e.Child {
		return dbq.PointLedger{}, ErrNotAwarded
	}
	if err := cover(ctx, c, e.Child, award.Delta); err != nil {
		return dbq.PointLedger{}, err
	}
	return insert(ctx, c, e, KindReversal, -award.Delta, &award.ID)
}

// Redeem spends e.Points, if the child has them.
func Redeem(ctx context.Context, c *command.Cmd, e Entry) (dbq.PointLedger, error) {
	if err := check(c, e); err != nil {
		return dbq.PointLedger{}, err
	}
	if err := cover(ctx, c, e.Child, e.Points); err != nil {
		return dbq.PointLedger{}, err
	}
	return insert(ctx, c, e, KindRedemption, -e.Points, nil)
}

// Refund gives back the redemption row redemptionID, once.
func Refund(ctx context.Context, c *command.Cmd, redemptionID uuid.UUID, by Entry) (dbq.PointLedger, error) {
	red, err := c.Q.GetLedgerEntry(ctx, dbq.GetLedgerEntryParams{HouseholdID: c.HouseholdID(), ID: redemptionID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && red.Kind != KindRedemption) {
		return dbq.PointLedger{}, fmt.Errorf("ledger: %s is not a redemption", redemptionID)
	} else if err != nil {
		return dbq.PointLedger{}, err
	}
	if !c.Holds(red.ChildID) {
		return dbq.PointLedger{}, ErrNotLocked
	}
	if _, err := c.Q.GetRefund(ctx, dbq.GetRefundParams{HouseholdID: c.HouseholdID(), ReversesID: &red.ID}); err == nil {
		return dbq.PointLedger{}, ErrAlreadyRefunded
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return dbq.PointLedger{}, err
	}
	e := by
	e.Child, e.SourceKind, e.SourceID = red.ChildID, red.SourceKind, red.SourceID
	return insert(ctx, c, e, KindRefund, -red.Delta, &red.ID)
}

func check(c *command.Cmd, e Entry) error {
	if e.Points <= 0 || e.Points > MaxAmount {
		return ErrAmount
	}
	if !c.Holds(e.Child) {
		return ErrNotLocked
	}
	return nil
}

// cover checks, under the child's lock, that the child can give up points.
func cover(ctx context.Context, c *command.Cmd, child uuid.UUID, points int32) error {
	bal, err := Balance(ctx, c.Q, c.HouseholdID(), child)
	if err != nil {
		return err
	}
	if bal < int64(points) {
		return ErrInsufficient
	}
	return nil
}

func insert(ctx context.Context, c *command.Cmd, e Entry, kind string, delta int32, reverses *uuid.UUID) (dbq.PointLedger, error) {
	return c.Q.InsertLedgerEntry(ctx, dbq.InsertLedgerEntryParams{
		HouseholdID: c.HouseholdID(), ChildID: e.Child, Delta: delta, Kind: kind,
		SourceKind: e.SourceKind, SourceID: e.SourceID, ReversesID: reverses,
		AdultID: e.AdultID, CredentialID: e.CredentialID, Note: e.Note,
	})
}
