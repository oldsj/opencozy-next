package server

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
)

var (
	errRewardUnavailable = apiError("reward_unavailable", "That reward isn't available right now.")
	errNotEnoughPoints   = apiError("not_enough_points", "Not enough points for that yet.")
	errMinutes           = apiError("minutes_mismatch", "Screen time needs minutes; other rewards take none.")
)

func toReward(r dbq.Reward) api.Reward {
	out := api.Reward{Id: r.ID, Title: r.Title, Cost: int(r.Cost), Kind: api.RewardKind(r.Kind), Available: r.Available, Version: int(r.Version)}
	if r.Minutes.Valid {
		out.Minutes = ptr(int(r.Minutes.Int32))
	}
	return out
}

// ListRewards lists the household's rewards, cheapest first.
func (s *Server) ListRewards(ctx context.Context, _ api.ListRewardsRequestObject) (api.ListRewardsResponseObject, error) {
	rows, err := s.q.ListRewards(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := api.Rewards{Rewards: make([]api.Reward, len(rows))}
	for i, r := range rows {
		out.Rewards[i] = toReward(r)
	}
	return api.ListRewards200JSONResponse(out), nil
}

// CreateReward adds a reward.
func (s *Server) CreateReward(ctx context.Context, req api.CreateRewardRequestObject) (api.CreateRewardResponseObject, error) {
	b := req.Body
	if (b.Kind == api.NewRewardKindScreenTime) != (b.Minutes != nil) {
		return api.CreateReward422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errMinutes)}, nil
	}
	var minutes pgtype.Int4
	if b.Minutes != nil {
		minutes = pgtype.Int4{Int32: int32(*b.Minutes), Valid: true} //nolint:gosec // 1-1440 by the spec
	}
	r, err := mustCmd(ctx).Q.CreateReward(ctx, dbq.CreateRewardParams{
		HouseholdID: mustActor(ctx).HouseholdID, Title: strings.TrimSpace(b.Title), Cost: int32(b.Cost), Kind: string(b.Kind), Minutes: minutes, //nolint:gosec // 1-100000 by the spec
	})
	if err != nil {
		return nil, err
	}
	return api.CreateReward201JSONResponse(toReward(r)), nil
}

// ArchiveReward removes a reward from now on.
func (s *Server) ArchiveReward(ctx context.Context, req api.ArchiveRewardRequestObject) (api.ArchiveRewardResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.ArchiveReward(ctx, dbq.ArchiveRewardParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.RewardId, Now: &now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.ArchiveReward404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.ArchiveReward204Response{}, nil
}

// RedeemReward spends a child's points on a reward. A child's request (on a display) waits for
// an adult; an adult's is approved at once.
func (s *Server) RedeemReward(ctx context.Context, req api.RedeemRewardRequestObject) (api.RedeemRewardResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	notFound := api.RedeemReward404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}
	conflict := func(e api.Error) api.RedeemRewardResponseObject {
		return api.RedeemReward409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(e)}
	}
	if err := cmd.LockChildren(ctx, req.ChildId); errors.Is(err, command.ErrNoChild) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	r, err := cmd.Q.GetLiveReward(ctx, dbq.GetLiveRewardParams{HouseholdID: a.HouseholdID, ID: req.Body.RewardId})
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound, nil
	} else if err != nil {
		return nil, err
	}
	if !r.Available {
		return conflict(errRewardUnavailable), nil
	}
	adult := a.Kind == actor.Adult || a.Kind == actor.Phone
	state := "pending"
	if adult {
		state = "approved"
	}
	red, err := cmd.Q.CreateRedemption(ctx, dbq.CreateRedemptionParams{
		HouseholdID: a.HouseholdID, ChildID: req.ChildId, RewardID: r.ID, Title: r.Title, Cost: r.Cost, Minutes: r.Minutes, State: state,
	})
	if err != nil {
		return nil, err
	}
	entry := ledger.Entry{Child: req.ChildId, Points: r.Cost, SourceKind: SourceRedemption, SourceID: red.ID}
	if adult {
		entry.AdultID = &a.AdultID
	}
	row, err := ledger.Redeem(ctx, cmd, entry)
	if errors.Is(err, ledger.ErrInsufficient) {
		// The command's 409 rolls back to its savepoint: the redemption row goes too.
		return conflict(errNotEnoughPoints), nil
	} else if err != nil {
		return nil, err
	}
	if err := cmd.Q.SetRedemptionLedger(ctx, dbq.SetRedemptionLedgerParams{HouseholdID: a.HouseholdID, ID: red.ID, LedgerID: &row.ID}); err != nil {
		return nil, err
	}
	if !adult {
		if _, err := cmd.Q.CreateRedemptionApproval(ctx, dbq.CreateRedemptionApprovalParams{
			HouseholdID: a.HouseholdID, ChildID: req.ChildId, RedemptionID: &red.ID, Title: r.Title, Points: r.Cost,
		}); err != nil {
			return nil, err
		}
	}
	return api.RedeemReward201JSONResponse{Id: red.ID, Status: api.RedemptionStatus(state)}, nil
}
