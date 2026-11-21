package server

import (
	"bytes"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/authz"
)

// TestAuthzMatrix sends every operation in the spec as every kind of actor and checks the answer
// against the operation's declared policy. Then it checks that household B's actors get 404 for
// household A's ids, and that an actor limited to some children gets 404 for the others, with
// household A's rows unchanged in both cases. A new operation is covered as soon as it is in
// the spec; one without a policy stops the server from starting.
func TestAuthzMatrix(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	policies, err := authz.Policies(spec)
	if err != nil {
		t.Fatal(err)
	}
	examples := map[string][]byte{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.RequestBody == nil {
				continue
			}
			mt := op.RequestBody.Value.Content.Get("application/json")
			if mt == nil || mt.Example == nil {
				t.Fatalf("%s %s: the request body needs an example for this test", method, path)
			}
			examples[method+" "+path], _ = json.Marshal(mt.Example)
		}
	}
	ops := make([]string, 0, len(policies))
	for k := range policies {
		ops = append(ops, k)
	}
	sort.Strings(ops)

	for _, op := range ops {
		p := policies[op]
		method, path, _ := strings.Cut(op, " ")
		t.Run(op, func(t *testing.T) {
			e := newEnv(t, nil)
			scopes := e.srv.table.Scopes()
			a := e.house("a", scopes)
			b := e.house("b", scopes)
			url := func(h house, child uuid.UUID) string {
				u := strings.ReplaceAll(path, "{childId}", child.String())
				u = strings.ReplaceAll(u, "{credentialId}", h.target.String())
				u = strings.ReplaceAll(u, "{displayId}", h.screen.String())
				u = strings.ReplaceAll(u, "{choreId}", h.chore.String())
				u = strings.ReplaceAll(u, "{routineId}", h.routine.String())
				u = strings.ReplaceAll(u, "{stepId}", h.step.String())
				u = strings.ReplaceAll(u, "{itemId}", h.approval.String())
				u = strings.ReplaceAll(u, "{recipeId}", h.recipe.String())
				u = strings.ReplaceAll(u, "{date}", "2026-09-16")
				u = strings.ReplaceAll(u, "{slot}", "dinner")
				if op == "DELETE /meals/{date}/{slot}" {
					u += "?expectedVersion=1" // a required query parameter
				}
				u = strings.ReplaceAll(u, "{entryId}", h.entry.String())
				u = strings.ReplaceAll(u, "{rewardId}", h.reward.String())
				u = strings.ReplaceAll(u, "{redemptionId}", h.redemption.String())
				u = strings.ReplaceAll(u, "{eventId}", h.event.String())
				u = strings.ReplaceAll(u, "{calendarId}", h.calendar.String())
				u = strings.ReplaceAll(u, "{ruleId}", h.rule.String())
				u = strings.ReplaceAll(u, "{linkId}", h.link.String())
				switch op {
				case "GET /calendar":
					u += "?from=2026-09-14&to=2026-09-21"
				case "DELETE /calendars/{calendarId}":
					u += "?expectedVersion=1"
				}
				u = strings.ReplaceAll(u, "{passkeyId}", h.passkey.String())
				u = strings.ReplaceAll(u, "{deviceId}", h.device.String())
				u = strings.ReplaceAll(u, "{phoneSessionId}", h.phone.String())
				u = strings.ReplaceAll(u, "{tvId}", h.tv.String())
				u = strings.ReplaceAll(u, "{promptId}", h.prompt.String())
				u = strings.ReplaceAll(u, "{creditId}", h.credit.String())
				return "/api" + u
			}
			body := func() any {
				if ex, ok := examples[op]; ok {
					return ex
				}
				return nil
			}

			// 1. Every actor of household A against A's own ids.
			var unauthorized []byte
			for _, name := range actorNames {
				r := e.send(a.actors[name], method, url(a, a.child1), body())
				if r.status >= 500 {
					t.Fatalf("%s: %d %s", name, r.status, r.raw)
				}
				kind, live := actorKind(name)
				switch {
				case p.Public:
					if r.status == 403 {
						t.Errorf("%s on a public operation: 403 %s", name, r.raw)
					}
				case !live:
					if r.status != 401 {
						t.Errorf("%s: want 401, got %d %s", name, r.status, r.raw)
					}
					if unauthorized == nil {
						unauthorized = r.raw
					} else if !bytes.Equal(unauthorized, r.raw) {
						t.Errorf("%s: the 401 differs: %s vs %s", name, r.raw, unauthorized)
					}
				case !allowed(p, name, kind):
					if r.status != 403 || r.code() != "forbidden" {
						t.Errorf("%s: want 403, got %d %s", name, r.status, r.raw)
					}
				case p.RecentAuth && name == "adult-stale":
					if r.status != 403 || r.code() != "reauthentication_required" {
						t.Errorf("%s: want 403 reauthentication_required, got %d %s", name, r.status, r.raw)
					}
				default:
					// A handler may answer 403 for its own reasons (a wrong password); only
					// the authorization refusal counts here.
					if r.status == 401 || r.code() == "forbidden" || r.code() == "reauthentication_required" {
						t.Errorf("%s may call this: got %d %s", name, r.status, r.raw)
					}
				}
			}

			// 2. Household B's allowed actors against household A's ids: 404, nothing changed.
			// A path without an id ({date}/{slot}) names B's own row, so only "nothing of A's
			// changed" applies.
			if !p.Public && strings.Contains(path, "{") {
				ids := strings.Contains(path, "Id}")
				before := e.fingerprint(a.id)
				for _, name := range actorNames {
					kind, live := actorKind(name)
					if !live || !allowed(p, name, kind) || (p.RecentAuth && name == "adult-stale") {
						continue
					}
					if r := e.send(b.actors[name], method, url(a, a.child1), body()); ids && r.status != 404 {
						t.Errorf("household B %s on A's ids: want 404, got %d %s", name, r.status, r.raw)
					}
				}
				if e.fingerprint(a.id) != before {
					t.Error("household B's requests changed household A's rows")
				}
			}

			// 3. A child outside the actor's children: 404, nothing changed.
			if p.ChildParam != "" && allowed(p, "service-child1", actor.Service) {
				before := e.fingerprint(a.id)
				if r := e.send(a.actors["service-child1"], method, url(a, a.child2), body()); r.status != 404 {
					t.Errorf("service-child1 on child2: want 404, got %d %s", r.status, r.raw)
				}
				if e.fingerprint(a.id) != before {
					t.Error("a refused child-scope request changed rows")
				}
			}
		})
	}
}

// allowed is the declared rule, restated: the kind is listed, and the credential holds the
// scopes the operation names for its kind.
func allowed(p authz.Policy, name string, kind actor.Kind) bool {
	if !slices.Contains(p.Actors, kind) {
		return false
	}
	switch name {
	case "service-none":
		return len(p.Scopes) == 0
	case "display-none":
		return false // a display with no scopes reaches nothing
	case "display-today":
		return displayHas(p, "today")
	case "display-meals":
		return displayHas(p, "meals")
	case "display-calendar":
		return displayHas(p, "calendar")
	case "display-tv":
		return displayHas(p, "tv")
	}
	return true
}

// displayHas: a display holding only scope reaches the operation.
func displayHas(p authz.Policy, scope string) bool {
	for _, s := range p.DisplayScopes {
		if s != scope {
			return false
		}
	}
	return true
}

func TestRecentAuthWindow(t *testing.T) {
	a := actor.Actor{Kind: actor.Adult, AllChildren: true}
	p := authz.Policy{Actors: []actor.Kind{actor.Adult}, RecentAuth: true}
	now := time.Now()
	a.AuthenticatedAt = now.Add(-authz.RecentAuth + time.Second)
	if d := p.Check(a, true, nil, now); d != authz.Allow {
		t.Errorf("inside the window: %v", d)
	}
	a.AuthenticatedAt = now.Add(-authz.RecentAuth - time.Second)
	if d := p.Check(a, true, nil, now); d != authz.Reauthenticate {
		t.Errorf("outside the window: %v", d)
	}
	// RECENT_AUTH_WINDOW replaces the 10 minutes.
	p.RecentAuthWindow = 30 * time.Second
	a.AuthenticatedAt = now.Add(-29 * time.Second)
	if d := p.Check(a, true, nil, now); d != authz.Allow {
		t.Errorf("inside a configured window: %v", d)
	}
	a.AuthenticatedAt = now.Add(-31 * time.Second)
	if d := p.Check(a, true, nil, now); d != authz.Reauthenticate {
		t.Errorf("outside a configured window: %v", d)
	}
}
