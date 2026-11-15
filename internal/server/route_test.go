package server

import (
	"net/http"
	"strings"
	"testing"
)

// The policy (kin-openapi's router) and dispatch (http.ServeMux) must agree on every request,
// or it is refused. Awkward spellings of a child's path either reach the same child with the
// same policy, or are turned away; none reaches a child the caller may not see.
func TestAwkwardPathsAgreeOrAreRefused(t *testing.T) {
	e := newEnv(t, nil)
	h := e.house("a", e.srv.table.Scopes())
	adult, limited := h.actors["adult"], h.actors["service-child1"]
	c1, c2 := h.child1.String(), h.child2.String()
	canonical := e.send(adult, "GET", "/api/children/"+c1, nil)
	if canonical.status != 200 {
		t.Fatalf("canonical: %d %s", canonical.status, canonical.raw)
	}
	variants := func(id string) []string {
		return []string{
			"/api/children/" + id + "/",
			"/api//children/" + id,
			"/api/children//" + id,
			"/api/children/./" + id,
			"/api/children/x/../" + id,
			"/api/children/%2e%2e/children/" + id,
			"/api/children/" + id + "%2F",
			"/api/children%2F" + id,
			"/api/Children/" + id,
			"/API/children/" + id,
			"/api/children/" + strings.ToUpper(id),
			"/api/children/" + id + "%00",
			"/api/children/" + id + ";x",
		}
	}
	for _, p := range variants(c1) {
		r := e.send(adult, "GET", p, nil)
		switch {
		case r.status >= 500:
			t.Errorf("%s: %d %s", p, r.status, r.raw)
		case r.status == 200 && string(r.raw) != string(canonical.raw):
			t.Errorf("%s: served something else: %s", p, r.raw)
		}
	}
	// service-child1 may see child1 only: no spelling of child2's path gets through.
	for _, p := range append(variants(c2), "/api/children/"+c1+"/../"+c2, "/api/children/"+c1+"%2F..%2F"+c2) {
		if r := e.send(limited, "GET", p, nil); r.status == 200 || r.status >= 500 {
			t.Errorf("service-child1 %s: %d %s", p, r.status, r.raw)
		}
	}
}

// A request whose pattern the mux matched differently from the policy's is refused.
func TestSameRouteRefusesDisagreement(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	for name, c := range map[string]struct {
		pattern, policy string
		mux, params     map[string]string
		want            int
	}{
		"agree":         {"GET /children/{childId}", "GET /children/{childId}", map[string]string{"childId": "a"}, map[string]string{"childId": "a"}, 204},
		"other pattern": {"GET /children/{childId}", "GET /children/{childId}/chores", map[string]string{"childId": "a"}, map[string]string{"childId": "a"}, 404},
		"other value":   {"GET /children/{childId}", "GET /children/{childId}", map[string]string{"childId": "a/b"}, map[string]string{"childId": "a"}, 404},
	} {
		r, _ := http.NewRequest(http.MethodGet, "/children/a", nil)
		r.Pattern = c.pattern
		for k, v := range c.mux {
			r.SetPathValue(k, v)
		}
		ri := routeInfo{params: c.params}
		ri.policy.Pattern = c.policy
		r = r.WithContext(withRoute(r.Context(), ri))
		w := &recorder{h: http.Header{}}
		sameRoute(ok).ServeHTTP(w, r)
		if w.code != c.want {
			t.Errorf("%s: %d, want %d", name, w.code, c.want)
		}
	}
}

type recorder struct {
	h    http.Header
	code int
}

func (w *recorder) Header() http.Header         { return w.h }
func (w *recorder) Write(b []byte) (int, error) { return len(b), nil }
func (w *recorder) WriteHeader(c int)           { w.code = c }
