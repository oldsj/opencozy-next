package calendar

import "net/http"

// FetcherFor is a Fetcher with a stand-in client, for tests outside this package. It exists
// only in test builds.
func FetcherFor(c interface {
	Do(*http.Request) (*http.Response, error)
}) Fetcher {
	return Fetcher{client: c}
}
