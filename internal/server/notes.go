package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

func toNote(n dbq.Note) api.Note {
	return api.Note{Id: n.ID, Title: n.Title, Body: n.Body, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
}

// ListNotes lists the user's notes.
func (s *Server) ListNotes(ctx context.Context, _ api.ListNotesRequestObject) (api.ListNotesResponseObject, error) {
	u, ok := userFrom(ctx)
	if !ok {
		return api.ListNotes401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	rows, err := s.q.ListNotes(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	items := make([]api.Note, len(rows))
	for i, n := range rows {
		items[i] = toNote(n)
	}
	return api.ListNotes200JSONResponse{Items: items}, nil
}

// CreateNote adds a note.
func (s *Server) CreateNote(ctx context.Context, req api.CreateNoteRequestObject) (api.CreateNoteResponseObject, error) {
	u, ok := userFrom(ctx)
	if !ok {
		return api.CreateNote401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	n, err := s.q.CreateNote(ctx, dbq.CreateNoteParams{UserID: u.ID, Title: req.Body.Title, Body: req.Body.Body})
	if err != nil {
		return nil, err
	}
	return api.CreateNote201JSONResponse(toNote(n)), nil
}

// UpdateNote replaces a note's title and body.
func (s *Server) UpdateNote(ctx context.Context, req api.UpdateNoteRequestObject) (api.UpdateNoteResponseObject, error) {
	u, ok := userFrom(ctx)
	if !ok {
		return api.UpdateNote401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	n, err := s.q.UpdateNote(ctx, dbq.UpdateNoteParams{ID: req.NoteId, UserID: u.ID, Title: req.Body.Title, Body: req.Body.Body})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.UpdateNote404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.UpdateNote200JSONResponse(toNote(n)), nil
}

// DeleteNote deletes a note.
func (s *Server) DeleteNote(ctx context.Context, req api.DeleteNoteRequestObject) (api.DeleteNoteResponseObject, error) {
	u, ok := userFrom(ctx)
	if !ok {
		return api.DeleteNote401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	n, err := s.q.DeleteNote(ctx, dbq.DeleteNoteParams{ID: req.NoteId, UserID: u.ID})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.DeleteNote404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.DeleteNote204Response{}, nil
}
