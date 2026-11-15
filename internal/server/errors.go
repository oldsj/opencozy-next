package server

import "github.com/oldsj/opencozy-next/internal/api"

func apiError(code, message string) api.Error {
	return api.Error{Error: api.ErrorDetail{Code: code, Message: message}}
}

var (
	errUnauthorized = apiError("unauthorized", "Sign in to continue.")
	errNotFound     = apiError("not_found", "Not found.")
)
