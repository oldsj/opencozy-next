package server

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

func toCredential(c dbq.Credential, children, rules []uuid.UUID) api.Credential {
	if children == nil {
		children = []uuid.UUID{}
	}
	if rules == nil {
		rules = []uuid.UUID{}
	}
	return api.Credential{
		Id: c.ID, Kind: api.CredentialKind(c.Kind), Name: c.Name, Scopes: c.Scopes,
		AllChildren: c.AllChildren, ChildIds: children, AwardRuleIds: rules, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt,
	}
}

// credentialRules maps the household's credentials to the award rules each may use.
func credentialRules(ctx context.Context, q *dbq.Queries, h uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	rows, err := q.ListHouseholdCredentialRules(ctx, h)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]uuid.UUID{}
	for _, r := range rows {
		out[r.CredentialID] = append(out[r.CredentialID], r.RuleID)
	}
	return out, nil
}

// ListCredentials lists the household's live credentials.
func (s *Server) ListCredentials(ctx context.Context, _ api.ListCredentialsRequestObject) (api.ListCredentialsResponseObject, error) {
	h := mustActor(ctx).HouseholdID
	rows, err := s.q.ListCredentials(ctx, h)
	if err != nil {
		return nil, err
	}
	links, err := s.q.ListHouseholdCredentialChildren(ctx, h)
	if err != nil {
		return nil, err
	}
	children := map[uuid.UUID][]uuid.UUID{}
	for _, l := range links {
		children[l.CredentialID] = append(children[l.CredentialID], l.ChildID)
	}
	rules, err := credentialRules(ctx, s.q, h)
	if err != nil {
		return nil, err
	}
	items := make([]api.Credential, 0, len(rows))
	for _, c := range rows {
		items = append(items, toCredential(c, children[c.ID], rules[c.ID]))
	}
	return api.ListCredentials200JSONResponse{Items: items}, nil
}

// CreateCredential issues a service or assistant credential. Scopes must be ones some operation
// requires; children must be live children of this household.
func (s *Server) CreateCredential(ctx context.Context, req api.CreateCredentialRequestObject) (api.CreateCredentialResponseObject, error) {
	a := mustActor(ctx)
	bad := func(code, msg string) (api.CreateCredentialResponseObject, error) {
		return api.CreateCredential400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError(code, msg))}, nil
	}
	for _, sc := range req.Body.Scopes {
		if !slices.Contains(s.table.Scopes(), sc) {
			return bad("unknown_scope", fmt.Sprintf("Unknown scope %q.", sc))
		}
	}
	all := req.Body.AllChildren != nil && *req.Body.AllChildren
	var childIDs []uuid.UUID
	if req.Body.ChildIds != nil {
		childIDs = *req.Body.ChildIds
	}
	if all && len(childIDs) > 0 {
		return bad("invalid_children", "Give either allChildren or childIds, not both.")
	}
	var ruleIDs []uuid.UUID
	if req.Body.AwardRuleIds != nil {
		ruleIDs = *req.Body.AwardRuleIds
	}
	if slices.Contains(req.Body.Scopes, ScopeLearningAward) {
		if req.Body.Kind != api.CreateCredentialJSONBodyKindService {
			return bad("unknown_scope", "Only a service credential can award learning points.")
		}
		if len(ruleIDs) == 0 || (!all && len(childIDs) == 0) {
			return bad("invalid_rules", "A learning credential needs at least one child and one award rule.")
		}
	} else if len(ruleIDs) > 0 {
		return bad("invalid_rules", "Award rules apply only to learning:award.")
	}
	prefix := auth.PrefixService
	if req.Body.Kind == api.CreateCredentialJSONBodyKindAssistant {
		prefix = auth.PrefixAssistant
	}
	secret, hash, err := auth.NewToken(prefix)
	if err != nil {
		return nil, err
	}

	q := mustCmd(ctx).Q
	// Lock the children so none is archived while it is linked.
	if err := mustCmd(ctx).LockChildren(ctx, childIDs...); errors.Is(err, command.ErrNoChild) {
		return bad("invalid_children", "One of the children is not in this household.")
	} else if err != nil {
		return nil, err
	}
	// Lock the rules so none is archived while it is given.
	if len(ruleIDs) > 0 {
		live, err := q.LiveAwardRuleIDs(ctx, dbq.LiveAwardRuleIDsParams{HouseholdID: a.HouseholdID, Ids: ruleIDs})
		if err != nil {
			return nil, err
		}
		if len(live) != len(ruleIDs) {
			return bad("invalid_rules", "One of the award rules is archived or not in this household.")
		}
	}
	adultID := a.AdultID
	c, err := q.CreateCredential(ctx, dbq.CreateCredentialParams{
		HouseholdID: a.HouseholdID, Kind: string(req.Body.Kind), Name: req.Body.Name, TokenHash: hash,
		Scopes: req.Body.Scopes, AllChildren: all, AdultID: &adultID,
	})
	if err != nil {
		return nil, err
	}
	for _, id := range childIDs {
		if err := q.AddCredentialChild(ctx, dbq.AddCredentialChildParams{HouseholdID: a.HouseholdID, CredentialID: c.ID, ChildID: id}); err != nil {
			return nil, err
		}
	}
	if len(ruleIDs) > 0 {
		if err := q.AddCredentialRules(ctx, dbq.AddCredentialRulesParams{HouseholdID: a.HouseholdID, CredentialID: c.ID, RuleIds: ruleIDs}); err != nil {
			return nil, err
		}
	}
	return api.CreateCredential201JSONResponse{Credential: toCredential(c, childIDs, ruleIDs), Secret: secret}, nil
}

// RevokeCredential revokes one of the household's credentials.
func (s *Server) RevokeCredential(ctx context.Context, req api.RevokeCredentialRequestObject) (api.RevokeCredentialResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.RevokeCredential(ctx, dbq.RevokeCredentialParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.CredentialId, Now: now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.RevokeCredential404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.RevokeCredential204Response{}, nil
}

// GetOwnCredential describes the calling credential.
func (s *Server) GetOwnCredential(ctx context.Context, _ api.GetOwnCredentialRequestObject) (api.GetOwnCredentialResponseObject, error) {
	a := mustActor(ctx)
	c, err := s.q.GetCredential(ctx, dbq.GetCredentialParams{HouseholdID: a.HouseholdID, ID: a.CredentialID})
	if err != nil {
		return nil, err
	}
	var children []uuid.UUID
	if a.Kind == actor.Service || a.Kind == actor.Assistant {
		children = a.Children
	}
	rules, err := credentialRules(ctx, s.q, a.HouseholdID)
	if err != nil {
		return nil, err
	}
	return api.GetOwnCredential200JSONResponse(toCredential(c, children, rules[c.ID])), nil
}
