package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
)

// SourceOccurrence is the ledger source kind for points from a chore or routine done on a day.
const SourceOccurrence = "occurrence"

var (
	errAlreadyDecided = apiError("already_decided", "Someone already decided this one.")
	errChildArchived  = apiError("child_archived", "That child is archived. Restore them first.")
)

// GetInbox lists what waits for an adult's decision.
func (s *Server) GetInbox(ctx context.Context, _ api.GetInboxRequestObject) (api.GetInboxResponseObject, error) {
	rows, err := s.q.ListPendingApprovals(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := api.Inbox{Items: make([]api.InboxItem, len(rows))}
	for i, r := range rows {
		it := api.InboxItem{Id: r.ID, Kind: api.InboxItemKind(r.Kind), Title: r.Title, SubmittedAt: r.CreatedAt}
		it.Child.Id, it.Child.Name, it.Child.Color = r.ChildID, r.ChildName, api.ProfileColor(r.ChildColor)
		if r.Points > 0 {
			it.Points = ptr(int(r.Points))
		}
		out.Items[i] = it
	}
	return api.GetInbox200JSONResponse(out), nil
}

// award pays an occurrence's points to its child, decided by the adult. Zero-point chores pay nothing.
func award(ctx context.Context, cmd *command.Cmd, adult api.Id, occ dbq.Occurrence) error {
	if occ.Points <= 0 {
		return nil
	}
	_, err := ledger.Award(ctx, cmd, ledger.Entry{
		Child: occ.ChildID, Points: occ.Points, SourceKind: SourceOccurrence, SourceID: occ.ID, AdultID: &adult,
	})
	return err
}

// SourceRedemption is the ledger source kind for points spent on a reward.
const SourceRedemption = "redemption"

// decision is a pending approval and its subject (an occurrence or a redemption), locked
// child first, ready to decide.
type decision struct {
	approval dbq.Approval
	occ      *dbq.Occurrence
	red      *dbq.Redemption
}

// lockPending finds the approval, locks its child, then the approval and its subject. It
// returns a refusal when there is no such approval, its child is archived, or it is decided.
func lockPending(ctx context.Context, cmd *command.Cmd, id api.Id) (*decision, *api.Error, int, error) {
	h := cmd.HouseholdID()
	a, err := cmd.Q.GetApproval(ctx, dbq.GetApprovalParams{HouseholdID: h, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &errNotFound, 404, nil
	} else if err != nil {
		return nil, nil, 0, err
	}
	if err := cmd.LockChildren(ctx, a.ChildID); errors.Is(err, command.ErrNoChild) {
		return nil, &errChildArchived, 409, nil
	} else if err != nil {
		return nil, nil, 0, err
	}
	if a, err = cmd.Q.LockApproval(ctx, dbq.LockApprovalParams{HouseholdID: h, ID: id}); err != nil {
		return nil, nil, 0, err
	}
	if a.State != "pending" {
		return nil, &errAlreadyDecided, 409, nil
	}
	d := &decision{approval: a}
	switch {
	case a.RedemptionID != nil:
		red, err := cmd.Q.LockRedemption(ctx, dbq.LockRedemptionParams{HouseholdID: h, ID: *a.RedemptionID})
		if err != nil {
			return nil, nil, 0, err
		}
		if red.State != "pending" || red.LedgerID == nil {
			return nil, nil, 0, fmt.Errorf("approval %s: redemption %s is %s", a.ID, red.ID, red.State)
		}
		d.red = &red
	case a.OccurrenceID != nil:
		occ, err := cmd.Q.LockOccurrence(ctx, dbq.LockOccurrenceParams{HouseholdID: h, ID: *a.OccurrenceID})
		if err != nil {
			return nil, nil, 0, err
		}
		if occ.State != "pending" {
			return nil, nil, 0, fmt.Errorf("approval %s: occurrence %s is %s", a.ID, occ.ID, occ.State)
		}
		d.occ = &occ
	default:
		return nil, nil, 0, fmt.Errorf("approval %s has no subject", a.ID)
	}
	return d, nil, 0, nil
}

// ApproveInboxItem says yes: a completion's points land; a redemption's points stay spent.
func (s *Server) ApproveInboxItem(ctx context.Context, req api.ApproveInboxItemRequestObject) (api.ApproveInboxItemResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	d, refusal, status, err := lockPending(ctx, cmd, req.ItemId)
	if err != nil {
		return nil, err
	}
	switch status {
	case 404:
		return api.ApproveInboxItem404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refusal)}, nil
	case 409:
		return api.ApproveInboxItem409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refusal)}, nil
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	decided, err := cmd.Q.DecideApproval(ctx, dbq.DecideApprovalParams{
		HouseholdID: a.HouseholdID, ID: d.approval.ID, State: "approved", DecidedBy: &a.AdultID, Now: &now,
	})
	if err != nil {
		return nil, err
	}
	if d.red != nil {
		if err := cmd.Q.SetRedemptionState(ctx, dbq.SetRedemptionStateParams{HouseholdID: a.HouseholdID, ID: d.red.ID, State: "approved"}); err != nil {
			return nil, err
		}
		return api.ApproveInboxItem200JSONResponse{Id: decided.ID, State: api.DecisionStateApproved}, nil
	}
	if _, err := cmd.Q.UpdateOccurrence(ctx, dbq.UpdateOccurrenceParams{
		HouseholdID: a.HouseholdID, ID: d.occ.ID, State: "complete", CheckedSteps: d.occ.CheckedSteps, Points: d.occ.Points,
	}); err != nil {
		return nil, err
	}
	if err := award(ctx, cmd, a.AdultID, *d.occ); err != nil {
		return nil, err
	}
	return api.ApproveInboxItem200JSONResponse{Id: decided.ID, State: api.DecisionStateApproved}, nil
}

// RejectInboxItem says no: a completion goes back on the child's list with the reason; a
// redemption's points come back.
func (s *Server) RejectInboxItem(ctx context.Context, req api.RejectInboxItemRequestObject) (api.RejectInboxItemResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	d, refusal, status, err := lockPending(ctx, cmd, req.ItemId)
	if err != nil {
		return nil, err
	}
	switch status {
	case 404:
		return api.RejectInboxItem404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(*refusal)}, nil
	case 409:
		return api.RejectInboxItem409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(*refusal)}, nil
	}
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	decided, err := cmd.Q.DecideApproval(ctx, dbq.DecideApprovalParams{
		HouseholdID: a.HouseholdID, ID: d.approval.ID, State: "rejected", Reason: req.Body.Reason, DecidedBy: &a.AdultID, Now: &now,
	})
	if err != nil {
		return nil, err
	}
	if d.red != nil {
		if err := cmd.Q.SetRedemptionState(ctx, dbq.SetRedemptionStateParams{HouseholdID: a.HouseholdID, ID: d.red.ID, State: "rejected"}); err != nil {
			return nil, err
		}
		if _, err := ledger.Refund(ctx, cmd, *d.red.LedgerID, ledger.Entry{Points: d.red.Cost, AdultID: &a.AdultID, Note: req.Body.Reason}); err != nil {
			return nil, err
		}
		return api.RejectInboxItem200JSONResponse{Id: decided.ID, State: api.DecisionStateRejected}, nil
	}
	// Open again, every step unticked, with the reason beside it.
	if _, err := cmd.Q.UpdateOccurrence(ctx, dbq.UpdateOccurrenceParams{
		HouseholdID: a.HouseholdID, ID: d.occ.ID, State: "open", CheckedSteps: []api.Id{}, Points: d.occ.Points, RejectedReason: req.Body.Reason,
	}); err != nil {
		return nil, err
	}
	return api.RejectInboxItem200JSONResponse{Id: decided.ID, State: api.DecisionStateRejected}, nil
}
