package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
	"github.com/oldsj/opencozy-next/internal/screentime"
)

var (
	errRewardUnavailable = apiError("reward_unavailable", "That reward isn't available right now.")
	errNotEnoughPoints   = apiError("not_enough_points", "Not enough points for that yet.")
	errMinutes           = apiError("minutes_mismatch", "Give minutes for iPad time, and none for other rewards.")
	errTargetMismatch    = apiError("target_mismatch", "Only a screen-time reward is for the iPad or the TV.")
)

func toReward(r dbq.Reward) api.Reward {
	out := api.Reward{Id: r.ID, Title: r.Title, Cost: int(r.Cost), Kind: api.RewardKind(r.Kind), Available: r.Available, Version: int(r.Version)}
	if r.Minutes.Valid {
		out.Minutes = ptr(int(r.Minutes.Int32))
	}
	if r.ScreenTimeTarget != nil {
		out.Target = ptr(api.Target(*r.ScreenTimeTarget))
	}
	return out
}

// rewardTarget is a reward's screen ("ipad" when screen time names none), or nil for an item.
// ok is false when an item names a screen.
func rewardTarget(screen bool, t *api.Target) (target *string, ok bool) {
	switch {
	case !screen:
		return nil, t == nil
	case t == nil:
		return ptr(string(api.TargetIpad)), true
	}
	return ptr(string(*t)), true
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
	target, ok := rewardTarget(b.Kind == api.NewRewardKindScreenTime, b.Target)
	if !ok {
		return api.CreateReward422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errTargetMismatch)}, nil
	}
	var minutes pgtype.Int4
	if b.Minutes != nil {
		minutes = pgtype.Int4{Int32: int32(*b.Minutes), Valid: true} //nolint:gosec // 1-1440 by the spec
	}
	r, err := mustCmd(ctx).Q.CreateReward(ctx, dbq.CreateRewardParams{
		HouseholdID: mustActor(ctx).HouseholdID, Title: strings.TrimSpace(b.Title), Cost: int32(b.Cost), Kind: string(b.Kind), Minutes: minutes, //nolint:gosec // 1-100000 by the spec
		ScreenTimeTarget: target,
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

// RedeemReward spends a child's points on a reward. A child's request (on a display or their
// device) waits for an adult; an adult's is approved at once. Screen time needs no approval: its
// minutes are banked for the child at once (allowed hours limit spending them, not buying them).
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
	screen := r.Minutes.Valid
	toTV := screen && r.ScreenTimeTarget != nil && *r.ScreenTimeTarget == string(api.TargetTv)
	adult := a.Kind == actor.Adult || a.Kind == actor.Phone
	state := "pending"
	var fulfilled *time.Time
	switch {
	case screen:
		now, err := cmd.Now(ctx)
		if err != nil {
			return nil, err
		}
		state, fulfilled = "fulfilled", &now
	case adult:
		state = "approved"
	}
	red, err := cmd.Q.CreateRedemption(ctx, dbq.CreateRedemptionParams{
		HouseholdID: a.HouseholdID, ChildID: req.ChildId, RewardID: r.ID, Title: r.Title, Cost: r.Cost, Minutes: r.Minutes,
		ScreenTimeTarget: r.ScreenTimeTarget, State: state, FulfilledAt: fulfilled,
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
	switch {
	case toTV:
		if err := s.tv.AddCredit(ctx, cmd.Q, a.HouseholdID, req.ChildId, &red.ID, r.Minutes.Int32*60, *fulfilled); err != nil {
			return nil, err
		}
	case screen:
		if _, err := screentime.AddCredit(ctx, cmd, screentime.Credit{
			Child: req.ChildId, Source: screentime.SourceRedemption, RedemptionID: &red.ID, Minutes: r.Minutes.Int32,
		}); err != nil {
			return nil, err
		}
		if err := screenTimeChanged(ctx, cmd, req.ChildId); err != nil {
			return nil, err
		}
	case !adult:
		if _, err := cmd.Q.CreateRedemptionApproval(ctx, dbq.CreateRedemptionApprovalParams{
			HouseholdID: a.HouseholdID, ChildID: req.ChildId, RedemptionID: &red.ID, Title: r.Title, Points: r.Cost,
		}); err != nil {
			return nil, err
		}
	}
	return api.RedeemReward201JSONResponse{Id: red.ID, Status: api.RedemptionStatus(state)}, nil
}
