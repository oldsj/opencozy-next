package server

import "github.com/oldsj/opencozy-next/internal/api"

func apiError(code, message string) api.Error {
	return api.Error{Error: api.ErrorDetail{Code: code, Message: message}}
}

var (
	errNotFound = apiError("not_found", "Not found.")
	errWrong    = apiError("invalid_credentials", "Wrong email or password.")
)
