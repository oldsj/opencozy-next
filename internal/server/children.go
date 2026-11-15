package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// maxChildren is the most active children a household may have.
const maxChildren = 20

func toChild(c dbq.Child) api.Child {
	return api.Child{Id: c.ID, Name: c.Name, Archived: c.ArchivedAt != nil, Version: int(c.Version)}
}

// ListChildren lists the children the caller may act for.
func (s *Server) ListChildren(ctx context.Context, req api.ListChildrenRequestObject) (api.ListChildrenResponseObject, error) {
	a := mustActor(ctx)
	include := req.Params.IncludeArchived != nil && *req.Params.IncludeArchived && a.IsAdult()
	rows, err := s.q.ListChildren(ctx, dbq.ListChildrenParams{HouseholdID: a.HouseholdID, IncludeArchived: include})
	if err != nil {
		return nil, err
	}
	items := make([]api.Child, 0, len(rows))
	for _, c := range rows {
		if a.MayActFor(c.ID) {
			items = append(items, toChild(c))
		}
	}
	return api.ListChildren200JSONResponse{Items: items}, nil
}

// CreateChild adds a child.
func (s *Server) CreateChild(ctx context.Context, req api.CreateChildRequestObject) (api.CreateChildResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	// The household lock makes the count and the insert one step.
	n, err := cmd.Q.CountChildren(ctx, a.HouseholdID)
	if err != nil {
		return nil, err
	}
	if n >= maxChildren {
		return api.CreateChild409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("too_many_children", "A household can have at most 20 children."))}, nil
	}
	c, err := cmd.Q.CreateChild(ctx, dbq.CreateChildParams{HouseholdID: a.HouseholdID, Name: req.Body.Name})
	if err != nil {
		return nil, err
	}
	return api.CreateChild201JSONResponse(toChild(c)), nil
}

// GetChild returns one child of the caller's household.
func (s *Server) GetChild(ctx context.Context, req api.GetChildRequestObject) (api.GetChildResponseObject, error) {
	c, err := s.q.GetChild(ctx, dbq.GetChildParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.ChildId})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetChild404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetChild200JSONResponse(toChild(c)), nil
}

// UpdateChild renames, archives or restores a child, if it is still at the expected version.
func (s *Server) UpdateChild(ctx context.Context, req api.UpdateChildRequestObject) (api.UpdateChildResponseObject, error) {
	a, cmd := mustActor(ctx), mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	c, err := cmd.Q.UpdateChild(ctx, dbq.UpdateChildParams{
		HouseholdID: a.HouseholdID, ID: req.ChildId, Version: int32(req.Body.ExpectedVersion), //nolint:gosec // the spec caps it at int32
		Name: req.Body.Name, Archived: req.Body.Archived, Now: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err := cmd.Q.GetChild(ctx, dbq.GetChildParams{HouseholdID: a.HouseholdID, ID: req.ChildId})
		if errors.Is(err, pgx.ErrNoRows) {
			return api.UpdateChild404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
		}
		if err != nil {
			return nil, err
		}
		return api.UpdateChild409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("version_conflict", "Someone changed this child meanwhile. Reload and try again."))}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.UpdateChild200JSONResponse(toChild(c)), nil
}
