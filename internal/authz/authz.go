// Package authz holds the authorization table: who may call each operation, read from the
// operation's x- extensions in api/openapi.yaml. The declaration is the check: an operation
// without one is refused, and the server does not start.
//
//	x-actors:         [adult, phone, ...] or [public]
//	x-scopes:         scopes a service or assistant credential needs (required when either is allowed; may be [])
//	x-display-scopes: scopes a display needs (required when display is allowed)
//	x-child-param:    the path parameter naming a child; actors limited to some children get 404 for others
//	x-recent-auth:    true: an adult web session must have signed in within RecentAuth
//	x-body-limit:     the body cap in bytes (default DefaultBodyLimit)
//	x-command:        true: a state change, run by internal/command (transaction, locks, idempotency).
//	                  It must declare the required Idempotency-Key header (common.yaml).
//	x-record-response: false: a command whose response holds a secret; only the outcome is
//	                  recorded, and a replay is told the request already succeeded
package authz

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
)

// DefaultBodyLimit caps request bodies when an operation sets no x-body-limit.
const DefaultBodyLimit = 64 << 10

// MaxBodyLimit is the most any operation may declare.
const MaxBodyLimit = 1 << 20

// RecentAuth is how recent an adult's sign-in must be for x-recent-auth operations.
const RecentAuth = 10 * time.Minute

// Policy is one operation's rule.
type Policy struct {
	OperationID   string
	Public        bool
	Actors        []actor.Kind
	Scopes        []string
	DisplayScopes []string
	ChildParam    string
	RecentAuth    bool
	// RecentAuthWindow overrides RecentAuth's 10 minutes when set (config RECENT_AUTH_WINDOW).
	RecentAuthWindow time.Duration
	BodyLimit        int64
	Command          bool
	// RecordResponse is false when the response must never be stored (it holds a secret).
	RecordResponse bool
	// Pattern is the route that matched, "METHOD /path/{param}", set by Find. The ServeMux that
	// dispatches must have matched the same one (internal/server checks).
	Pattern string
}

// Decision is the outcome of checking an actor against a policy.
type Decision int

// Decisions.
const (
	Allow           Decision = iota
	Unauthenticated          // 401: no live credential
	Forbidden                // 403: this actor may not call this operation
	Reauthenticate           // 403: sign in again first
	NotFound                 // 404: the child is outside the actor's reach
)

// Check decides whether a (present when ok) may call the operation with these path parameters.
func (p Policy) Check(a actor.Actor, ok bool, params map[string]string, now time.Time) Decision {
	if p.Public {
		return Allow
	}
	if !ok {
		return Unauthenticated
	}
	if !slices.Contains(p.Actors, a.Kind) {
		return Forbidden
	}
	switch a.Kind {
	case actor.Service, actor.Assistant:
		if !a.HasScopes(p.Scopes) {
			return Forbidden
		}
	case actor.Display:
		if !a.HasScopes(p.DisplayScopes) {
			return Forbidden
		}
	}
	window := RecentAuth
	if p.RecentAuthWindow > 0 {
		window = p.RecentAuthWindow
	}
	if p.RecentAuth && a.Kind == actor.Adult && now.Sub(a.AuthenticatedAt) > window {
		return Reauthenticate
	}
	if p.ChildParam != "" {
		id, err := uuid.Parse(params[p.ChildParam])
		if err != nil || !a.MayActFor(id) {
			return NotFound
		}
	}
	return Allow
}

// Table maps requests to policies.
type Table struct {
	router routers.Router
	byOp   map[*openapi3.Operation]Policy
	scopes []string
}

// Route errors.
var (
	ErrNoRoute  = errors.New("authz: no such operation")
	ErrNoMethod = errors.New("authz: method not allowed")
	errNoPolicy = errors.New("authz: operation has no policy")
)

// New reads every operation's policy from spec. It fails if any operation's declaration is
// missing or inconsistent.
func New(spec *openapi3.T) (*Table, error) {
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, err
	}
	t := &Table{router: router, byOp: map[*openapi3.Operation]Policy{}}
	known := map[string]bool{}
	var errs []error
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			p, err := parse(op, item)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s %s: %w", method, path, err))
				continue
			}
			t.byOp[op] = p
			for _, s := range p.Scopes {
				known[s] = true
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	for s := range known {
		t.scopes = append(t.scopes, s)
	}
	sort.Strings(t.scopes)
	return t, nil
}

// Scopes lists every credential scope some operation requires, the vocabulary credentials may
// be issued with.
func (t *Table) Scopes() []string { return t.scopes }

// Find returns the request's policy and path parameters. Deny by default: a route without a
// policy is an error, never an open door.
func (t *Table) Find(r *http.Request) (Policy, map[string]string, error) {
	route, params, err := t.router.FindRoute(r)
	switch {
	case errors.Is(err, routers.ErrMethodNotAllowed):
		return Policy{}, nil, ErrNoMethod
	case err != nil:
		return Policy{}, nil, ErrNoRoute
	}
	p, ok := t.byOp[route.Operation]
	if !ok {
		return Policy{}, nil, errNoPolicy
	}
	p.Pattern = route.Method + " " + route.Path
	return p, params, nil
}

// Policies returns every operation's policy keyed by "METHOD path", for tests.
func Policies(spec *openapi3.T) (map[string]Policy, error) {
	out := map[string]Policy{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			p, err := parse(op, item)
			if err != nil {
				return nil, err
			}
			out[method+" "+path] = p
		}
	}
	return out, nil
}

func parse(op *openapi3.Operation, item *openapi3.PathItem) (Policy, error) {
	p := Policy{OperationID: op.OperationID, BodyLimit: DefaultBodyLimit, RecordResponse: true}
	actors, ok := stringList(op.Extensions["x-actors"])
	if !ok || len(actors) == 0 {
		return p, errors.New("x-actors is missing")
	}
	for _, a := range actors {
		switch {
		case a == "public":
			p.Public = true
		case slices.Contains(actor.Kinds, actor.Kind(a)):
			p.Actors = append(p.Actors, actor.Kind(a))
		default:
			return p, fmt.Errorf("x-actors: unknown actor %q", a)
		}
	}
	if p.Public && len(p.Actors) > 0 {
		return p, errors.New("x-actors: public cannot be combined with actors")
	}
	if v, present := op.Extensions["x-scopes"]; present {
		if p.Scopes, ok = stringList(v); !ok {
			return p, errors.New("x-scopes must be a list of strings")
		}
		if p.Scopes == nil {
			p.Scopes = []string{}
		}
	}
	if (slices.Contains(p.Actors, actor.Service) || slices.Contains(p.Actors, actor.Assistant)) && p.Scopes == nil {
		return p, errors.New("x-scopes is required when service or assistant is allowed")
	}
	if v, present := op.Extensions["x-display-scopes"]; present {
		if p.DisplayScopes, ok = stringList(v); !ok || len(p.DisplayScopes) == 0 {
			return p, errors.New("x-display-scopes must be a non-empty list of strings")
		}
	}
	if slices.Contains(p.Actors, actor.Display) && len(p.DisplayScopes) == 0 {
		return p, errors.New("x-display-scopes is required when display is allowed")
	}
	if v, present := op.Extensions["x-child-param"]; present {
		name, ok := v.(string)
		if !ok || !hasPathParam(op, item, name) {
			return p, fmt.Errorf("x-child-param %v is not a path parameter", v)
		}
		p.ChildParam = name
	}
	if v, present := op.Extensions["x-recent-auth"]; present {
		b, ok := v.(bool)
		if !ok {
			return p, errors.New("x-recent-auth must be a boolean")
		}
		p.RecentAuth = b
	}
	// Recent sign-in is defined for the adult web session only; a phone session has no rule yet,
	// so it would pass unchecked.
	if p.RecentAuth && slices.ContainsFunc(p.Actors, func(k actor.Kind) bool { return k != actor.Adult }) {
		return p, errors.New("x-recent-auth is only defined for the adult actor")
	}
	if v, present := op.Extensions["x-command"]; present {
		b, ok := v.(bool)
		if !ok {
			return p, errors.New("x-command must be a boolean")
		}
		p.Command = b
	}
	if p.Public && p.Command {
		return p, errors.New("x-command needs an actor; a public operation cannot be one")
	}
	if p.Command && !hasRequiredHeader(op, item, "Idempotency-Key") {
		return p, errors.New("x-command needs the required Idempotency-Key header parameter")
	}
	if v, present := op.Extensions["x-record-response"]; present {
		b, ok := v.(bool)
		if !ok || !p.Command {
			return p, errors.New("x-record-response must be a boolean, on an x-command operation")
		}
		p.RecordResponse = b
	}
	// A command's answer is stored for idempotent retries, so one that returns a secret must say
	// x-record-response: false.
	if p.Command && p.RecordResponse {
		if name := secretProperty(op); name != "" {
			return p, fmt.Errorf("the response has a %q property: a command returning a secret needs x-record-response: false", name)
		}
	}
	if v, present := op.Extensions["x-body-limit"]; present {
		n, ok := v.(float64)
		if !ok || n < 1 || n > MaxBodyLimit || n != float64(int64(n)) {
			return p, fmt.Errorf("x-body-limit must be an integer from 1 to %d", MaxBodyLimit)
		}
		p.BodyLimit = int64(n)
	}
	return p, nil
}

// secretProperty names the first property of any response schema that looks like a secret or a
// token, at any depth, or returns "".
func secretProperty(op *openapi3.Operation) string {
	if op.Responses == nil {
		return ""
	}
	seen := map[*openapi3.Schema]bool{}
	var walk func(*openapi3.SchemaRef) string
	walk = func(ref *openapi3.SchemaRef) string {
		if ref == nil || ref.Value == nil || seen[ref.Value] {
			return ""
		}
		sc := ref.Value
		seen[sc] = true
		for name, prop := range sc.Properties {
			if secretName(name) {
				return name
			}
			if n := walk(prop); n != "" {
				return n
			}
		}
		for _, refs := range []openapi3.SchemaRefs{sc.AllOf, sc.OneOf, sc.AnyOf} {
			for _, r := range refs {
				if n := walk(r); n != "" {
					return n
				}
			}
		}
		if n := walk(sc.Items); n != "" {
			return n
		}
		if sc.AdditionalProperties.Schema != nil {
			return walk(sc.AdditionalProperties.Schema)
		}
		return ""
	}
	for _, resp := range op.Responses.Map() {
		if resp.Value == nil {
			continue
		}
		for _, mt := range resp.Value.Content {
			if n := walk(mt.Schema); n != "" {
				return n
			}
		}
	}
	return ""
}

// secretName: secret, token, password, or a name ending in one (accessToken, clientSecret).
func secretName(name string) bool {
	n := strings.ToLower(name)
	for _, w := range []string{"secret", "token", "password"} {
		if strings.HasSuffix(n, w) {
			return true
		}
	}
	return false
}

func stringList(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func hasRequiredHeader(op *openapi3.Operation, item *openapi3.PathItem, name string) bool {
	for _, ps := range []openapi3.Parameters{item.Parameters, op.Parameters} {
		for _, ref := range ps {
			if ref.Value != nil && ref.Value.In == "header" && ref.Value.Name == name && ref.Value.Required {
				return true
			}
		}
	}
	return false
}

func hasPathParam(op *openapi3.Operation, item *openapi3.PathItem, name string) bool {
	for _, ps := range []openapi3.Parameters{item.Parameters, op.Parameters} {
		for _, ref := range ps {
			if ref.Value != nil && ref.Value.In == "path" && ref.Value.Name == name {
				return true
			}
		}
	}
	return false
}
