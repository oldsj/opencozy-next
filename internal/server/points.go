package server

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
)

var (
	errNotAnAward      = apiError("not_an_award", "Only points a child earned can be taken back.")
	errAlreadyReversed = apiError("already_reversed", "Those points were already taken back.")
	errPointsSpent     = apiError("points_spent", "Those points are already spent, so they can't be taken back.")
)

var historyKinds = map[string]api.HistoryEntryKind{
	ledger.KindAward:      api.HistoryEntryKindEarned,
	ledger.KindReversal:   api.HistoryEntryKindReversed,
	ledger.KindRedemption: api.HistoryEntryKindRedeemed,
	ledger.KindRefund:     api.HistoryEntryKindRefunded,
}

// historyLimit is the most entries a history holds (ChildHistory's and ChildScreenTimeEvents' limit).
const historyLimit = 200

// screenTimeEventTitles names each shield event in a child's history.
var screenTimeEventTitles = map[string]string{"one_more_minute": "Asked for one more minute"}

func toHistoryEntry(r dbq.ChildHistoryRow) api.HistoryEntry {
	return api.HistoryEntry{
		Id: r.ID, At: r.CreatedAt, Kind: historyKinds[r.Kind], Title: r.Title, Points: int(r.Delta),
		Reason: r.Note, ReversedBy: r.ReversedBy, Reverses: r.ReversesID, By: r.AdultName,
	}
}

// GetPointsHistory is a child's balance and newest entries.
func (s *Server) GetPointsHistory(ctx context.Context, req api.GetPointsHistoryRequestObject) (api.GetPointsHistoryResponseObject, error) {
	h := mustActor(ctx).HouseholdID
	if _, err := s.q.GetChild(ctx, dbq.GetChildParams{HouseholdID: h, ID: req.ChildId}); errors.Is(err, pgx.ErrNoRows) {
		return api.GetPointsHistory404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	bal, err := ledger.Balance(ctx, s.q, h, req.ChildId)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ChildHistory(ctx, dbq.ChildHistoryParams{HouseholdID: h, ChildID: req.ChildId})
	if err != nil {
		return nil, err
	}
	events, err := s.q.ChildScreenTimeEvents(ctx, dbq.ChildScreenTimeEventsParams{HouseholdID: h, ChildID: req.ChildId})
	if err != nil {
		return nil, err
	}
	out := api.History{Balance: bal, Entries: make([]api.HistoryEntry, 0, len(rows)+len(events))}
	for _, r := range rows {
		out.Entries = append(out.Entries, toHistoryEntry(r))
	}
	// Shield events aren't ledger entries: they join the newest 200 by time, worth no points.
	for _, e := range events {
		out.Entries = append(out.Entries, api.HistoryEntry{
			Id: e.ID, At: e.OccurredAt, Kind: api.HistoryEntryKindOneMoreMinute, Title: screenTimeEventTitles[e.Kind], Device: e.DeviceName,
		})
	}
	slices.SortStableFunc(out.Entries, func(a, b api.HistoryEntry) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		return bytes.Compare(b.Id[:], a.Id[:])
	})
	out.Entries = out.Entries[:min(len(out.Entries), historyLimit)]
	return api.GetPointsHistory200JSONResponse(out), nil
}

// ReversePoints takes back one award. A chore or routine it paid for is open again, so it can
// be done (and approved) again.
func (s *Server) ReversePoints(ctx context.Context, req api.ReversePointsRequestObject) (api.ReversePointsResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	conflict := func(e api.Error) api.ReversePointsResponseObject {
		return api.ReversePoints409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(e)}
	}
	entry, err := cmd.Q.GetLedgerEntry(ctx, dbq.GetLedgerEntryParams{HouseholdID: a.HouseholdID, ID: req.EntryId})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !a.MayActFor(entry.ChildID)) {
		return api.ReversePoints404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	if entry.Kind != ledger.KindAward {
		return conflict(errNotAnAward), nil
	}
	if err := cmd.LockChildren(ctx, entry.ChildID); errors.Is(err, command.ErrNoChild) {
		return conflict(errChildArchived), nil
	} else if err != nil {
		return nil, err
	}
	// Reverse undoes the source's current award; make sure that is this entry, not a later one.
	active, err := cmd.Q.GetActiveAward(ctx, dbq.GetActiveAwardParams{HouseholdID: a.HouseholdID, SourceKind: entry.SourceKind, SourceID: entry.SourceID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && active != entry.ID) {
		return conflict(errAlreadyReversed), nil
	} else if err != nil {
		return nil, err
	}
	var reason *string
	if req.Body != nil {
		reason = req.Body.Reason
	}
	row, err := ledger.Reverse(ctx, cmd, ledger.Entry{
		Child: entry.ChildID, SourceKind: entry.SourceKind, SourceID: entry.SourceID, AdultID: &a.AdultID, Note: reason,
	})
	switch {
	case errors.Is(err, ledger.ErrInsufficient):
		return conflict(errPointsSpent), nil
	case errors.Is(err, ledger.ErrNotAwarded):
		return conflict(errAlreadyReversed), nil
	case err != nil:
		return nil, err
	}
	if entry.SourceKind == SourceOccurrence {
		occ, err := cmd.Q.LockOccurrence(ctx, dbq.LockOccurrenceParams{HouseholdID: a.HouseholdID, ID: entry.SourceID})
		if err != nil {
			return nil, err
		}
		if _, err := cmd.Q.UpdateOccurrence(ctx, dbq.UpdateOccurrenceParams{
			HouseholdID: a.HouseholdID, ID: occ.ID, State: "open", CheckedSteps: []api.Id{}, Points: occ.Points,
		}); err != nil {
			return nil, err
		}
	}
	rows, err := cmd.Q.ChildHistory(ctx, dbq.ChildHistoryParams{HouseholdID: a.HouseholdID, ChildID: entry.ChildID})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ID == row.ID {
			return api.ReversePoints200JSONResponse(toHistoryEntry(r)), nil
		}
	}
	return nil, errors.New("reverse: the new entry is not in the history")
}
