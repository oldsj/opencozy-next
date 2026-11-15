package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/ledger"
	"github.com/oldsj/opencozy-next/internal/live"
)

const (
	// SourceLearning is the ledger source kind of a learning award (source_id: learning_award.id).
	SourceLearning = "learning"
	// ScopeLearningAward lets a service credential award points for learning activities.
	ScopeLearningAward = "learning:award"
	// defaultProvider is the learning app an award names when it names none (Block Buddy, in
	// Minecraft, as in the old app).
	defaultProvider = "minecraft"
)

func toAwardRule(r dbq.AwardRule) api.AwardRule {
	return api.AwardRule{Id: r.ID, ActivityKey: r.ActivityKey, Points: int(r.Points), DailyLimit: int(r.DailyLimit),
		Version: int(r.Version), Archived: r.ArchivedAt != nil}
}

func toLearningLink(l dbq.LearningLink) api.LearningLink {
	return api.LearningLink{Id: l.ID, ChildId: l.ChildID, Provider: l.Provider, ExternalId: l.ExternalID, LinkedAt: l.LinkedAt}
}

func ruleExists() api.Error {
	return apiError("rule_exists", "Another rule already gives points for this activity.")
}

// ListAwardRules lists the household's award rules, live ones unless archived ones are asked for.
func (s *Server) ListAwardRules(ctx context.Context, req api.ListAwardRulesRequestObject) (api.ListAwardRulesResponseObject, error) {
	rows, err := s.q.ListAwardRules(ctx, dbq.ListAwardRulesParams{
		HouseholdID: mustActor(ctx).HouseholdID, IncludeArchived: req.Params.Archived != nil && *req.Params.Archived,
	})
	if err != nil {
		return nil, err
	}
	out := make([]api.AwardRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAwardRule(r))
	}
	return api.ListAwardRules200JSONResponse{Rules: out}, nil
}

// CreateAwardRule adds a rule; an activity has at most one live rule.
func (s *Server) CreateAwardRule(ctx context.Context, req api.CreateAwardRuleRequestObject) (api.CreateAwardRuleResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	b := req.Body
	// The command holds the household lock, so two creates for one activity can't both pass.
	if taken, err := cmd.Q.ActivityTaken(ctx, dbq.ActivityTakenParams{HouseholdID: h, ActivityKey: b.ActivityKey, ExceptID: uuid.Nil}); err != nil {
		return nil, err
	} else if taken {
		return api.CreateAwardRule409JSONResponse(ruleExists()), nil
	}
	r, err := cmd.Q.CreateAwardRule(ctx, dbq.CreateAwardRuleParams{
		HouseholdID: h, ActivityKey: b.ActivityKey, Points: int32(b.Points), DailyLimit: int32(b.DailyLimit), //nolint:gosec // the spec caps both at 1,000,000
	})
	if err != nil {
		return nil, err
	}
	return api.CreateAwardRule201JSONResponse(toAwardRule(r)), nil
}

// UpdateAwardRule changes a live rule, or archives it for good.
func (s *Server) UpdateAwardRule(ctx context.Context, req api.UpdateAwardRuleRequestObject) (api.UpdateAwardRuleResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	b := req.Body
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	if !b.Archived {
		if taken, err := cmd.Q.ActivityTaken(ctx, dbq.ActivityTakenParams{HouseholdID: h, ActivityKey: b.ActivityKey, ExceptID: req.RuleId}); err != nil {
			return nil, err
		} else if taken {
			return api.UpdateAwardRule409JSONResponse(ruleExists()), nil
		}
	}
	r, err := cmd.Q.UpdateAwardRule(ctx, dbq.UpdateAwardRuleParams{
		HouseholdID: h, ID: req.RuleId, ActivityKey: b.ActivityKey, Points: int32(b.Points), DailyLimit: int32(b.DailyLimit), //nolint:gosec // capped by the spec
		Archive: b.Archived, Now: now, ExpectedVersion: int32(b.ExpectedVersion), //nolint:gosec // capped by the spec
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := cmd.Q.GetAwardRule(ctx, dbq.GetAwardRuleParams{HouseholdID: h, ID: req.RuleId}); errors.Is(err, pgx.ErrNoRows) {
			return api.UpdateAwardRule404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		} else if err != nil {
			return nil, err
		}
		return api.UpdateAwardRule409JSONResponse(conflict("rule")), nil // stale, or archived
	} else if err != nil {
		return nil, err
	}
	return api.UpdateAwardRule200JSONResponse(toAwardRule(r)), nil
}

// ListLearningLinks lists the live links of live children.
func (s *Server) ListLearningLinks(ctx context.Context, _ api.ListLearningLinksRequestObject) (api.ListLearningLinksResponseObject, error) {
	rows, err := s.q.ListLearningLinks(ctx, mustActor(ctx).HouseholdID)
	if err != nil {
		return nil, err
	}
	out := make([]api.LearningLink, 0, len(rows))
	for _, l := range rows {
		out = append(out, toLearningLink(l))
	}
	return api.ListLearningLinks200JSONResponse{Links: out}, nil
}

// LinkLearner links a live child to an account no child has.
func (s *Server) LinkLearner(ctx context.Context, req api.LinkLearnerRequestObject) (api.LinkLearnerResponseObject, error) {
	h, cmd := mustActor(ctx).HouseholdID, mustCmd(ctx)
	b := req.Body
	if err := cmd.LockChildren(ctx, b.ChildId); errors.Is(err, command.ErrNoChild) {
		return api.LinkLearner404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	} else if err != nil {
		return nil, err
	}
	if taken, err := cmd.Q.LinkTaken(ctx, dbq.LinkTakenParams{HouseholdID: h, Provider: b.Provider, ExternalID: b.ExternalId}); err != nil {
		return nil, err
	} else if taken {
		return api.LinkLearner409JSONResponse(apiError("link_exists", "That account is already linked to a child. Unlink it first.")), nil
	}
	l, err := cmd.Q.CreateLearningLink(ctx, dbq.CreateLearningLinkParams{HouseholdID: h, ChildID: b.ChildId, Provider: b.Provider, ExternalID: b.ExternalId})
	if err != nil {
		return nil, err
	}
	return api.LinkLearner201JSONResponse(toLearningLink(l)), nil
}

// UnlinkLearner ends a link. The row stays, as do the awards it led to.
func (s *Server) UnlinkLearner(ctx context.Context, req api.UnlinkLearnerRequestObject) (api.UnlinkLearnerResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.UnlinkLearner(ctx, dbq.UnlinkLearnerParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.LinkId, Now: now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.UnlinkLearner404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.UnlinkLearner204Response{}, nil
}

type ruleSnapshot struct {
	ID          uuid.UUID `json:"id"`
	ActivityKey string    `json:"activityKey"`
	Points      int32     `json:"points"`
	DailyLimit  int32     `json:"dailyLimit"`
	Version     int32     `json:"version"`
}

func toLearningAward(a dbq.LearningAward, ruleVersion int32, balance int64) api.LearningAward {
	return api.LearningAward{Id: a.ID, SourceEventId: a.SourceEventID, ChildId: a.ChildID, ActivityKey: a.ActivityKey,
		Points: int(a.Points), RuleVersion: int(ruleVersion), Balance: balance}
}

// AwardLearning pays a learning activity's points: the linked account picks the child, the live
// rule for the activity the points, up to the rule's daily limit. An event pays once, ever.
func (s *Server) AwardLearning(ctx context.Context, req api.AwardLearningRequestObject) (api.AwardLearningResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	h, b := a.HouseholdID, req.Body
	provider := defaultProvider
	if b.Provider != nil {
		provider = *b.Provider
	}
	forbidden := func(code, msg string) (api.AwardLearningResponseObject, error) {
		return api.AwardLearning403JSONResponse(apiError(code, msg)), nil
	}
	childNotPermitted := func() (api.AwardLearningResponseObject, error) {
		return forbidden("child_not_permitted", "This credential may not award points to that child.")
	}
	ruleNotPermitted := func() (api.AwardLearningResponseObject, error) {
		return forbidden("rule_not_permitted", "This credential may not award points for that activity.")
	}
	mayUse := func(rule uuid.UUID) (bool, error) {
		return cmd.Q.CredentialMayUseRule(ctx, dbq.CredentialMayUseRuleParams{CredentialID: a.CredentialID, RuleID: rule})
	}

	// An event already awarded: authorize against what it paid (links and rules may have changed
	// since), then return it, or refuse an event ID reused for something else.
	prior, err := cmd.Q.GetLearningAwardByEvent(ctx, dbq.GetLearningAwardByEventParams{HouseholdID: h, SourceEventID: b.SourceEventId})
	switch {
	case err == nil:
		if !a.MayActFor(prior.ChildID) {
			return childNotPermitted()
		}
		if ok, err := mayUse(prior.RuleID); err != nil {
			return nil, err
		} else if !ok {
			return ruleNotPermitted()
		}
		if prior.Provider != provider || prior.ExternalID != b.ExternalId || prior.ActivityKey != b.ActivityKey {
			return api.AwardLearning409JSONResponse(apiError("event_reused", "This event was already awarded for another account or activity.")), nil
		}
		var snap ruleSnapshot
		if err := json.Unmarshal(prior.Rule, &snap); err != nil {
			return nil, err
		}
		bal, err := ledger.Balance(ctx, cmd.Q, h, prior.ChildID)
		if err != nil {
			return nil, err
		}
		return api.AwardLearning200JSONResponse(toLearningAward(prior, snap.Version, bal)), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}

	link, err := cmd.Q.LinkedChild(ctx, dbq.LinkedChildParams{HouseholdID: h, Provider: provider, ExternalID: b.ExternalId})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AwardLearning404JSONResponse(apiError("unknown_learner", "No child is linked to that account.")), nil
	} else if err != nil {
		return nil, err
	}
	if !a.MayActFor(link.ChildID) {
		return childNotPermitted()
	}
	childArchived := api.AwardLearning409JSONResponse(apiError("child_archived", "That child is archived."))
	if link.Archived {
		return childArchived, nil
	}
	if err := cmd.LockChildren(ctx, link.ChildID); errors.Is(err, command.ErrNoChild) {
		return childArchived, nil // archived meanwhile
	} else if err != nil {
		return nil, err
	}
	rule, err := cmd.Q.LiveRuleForActivity(ctx, dbq.LiveRuleForActivityParams{HouseholdID: h, ActivityKey: b.ActivityKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AwardLearning404JSONResponse(apiError("unknown_activity", "No rule gives points for that activity.")), nil
	} else if err != nil {
		return nil, err
	}
	if ok, err := mayUse(rule.ID); err != nil {
		return nil, err
	} else if !ok {
		return ruleNotPermitted()
	}

	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	hh, err := cmd.Q.GetHousehold(ctx, h)
	if err != nil {
		return nil, err
	}
	zone, err := day.Zone(hh.TimeZone)
	if err != nil {
		return nil, err
	}
	today := day.Of(now, zone)
	awarded, err := cmd.Q.PointsAwardedOnDay(ctx, dbq.PointsAwardedOnDayParams{HouseholdID: h, ChildID: link.ChildID, RuleID: rule.ID, Day: pgDate(today)})
	if err != nil {
		return nil, err
	}
	if awarded+int64(rule.Points) > int64(rule.DailyLimit) {
		return api.AwardLearning409JSONResponse(apiError("daily_limit_reached", "That activity has given all its points for today.")), nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	credential := a.CredentialID
	entry, err := ledger.Award(ctx, cmd, ledger.Entry{Child: link.ChildID, Points: rule.Points, SourceKind: SourceLearning, SourceID: id, CredentialID: &credential})
	if err != nil {
		return nil, err
	}
	snap, err := json.Marshal(ruleSnapshot{ID: rule.ID, ActivityKey: rule.ActivityKey, Points: rule.Points, DailyLimit: rule.DailyLimit, Version: rule.Version})
	if err != nil {
		return nil, err
	}
	award, err := cmd.Q.InsertLearningAward(ctx, dbq.InsertLearningAwardParams{
		ID: id, HouseholdID: h, SourceEventID: b.SourceEventId, ChildID: link.ChildID, RuleID: rule.ID, Rule: snap,
		Points: rule.Points, Day: pgDate(today), Provider: provider, ExternalID: b.ExternalId, ActivityKey: b.ActivityKey,
		CredentialID: &credential, LedgerID: entry.ID,
	})
	if err != nil {
		return nil, err
	}
	bal, err := ledger.Balance(ctx, cmd.Q, h, link.ChildID)
	if err != nil {
		return nil, err
	}
	if err := live.Notify(ctx, cmd.Q, h, live.Event{Topic: live.Today}); err != nil {
		return nil, err
	}
	return api.AwardLearning201JSONResponse(toLearningAward(award, rule.Version, bal)), nil
}
