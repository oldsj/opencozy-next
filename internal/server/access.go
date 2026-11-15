package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/authz"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/httpx"
)

type routeKey struct{}

type routeInfo struct {
	policy authz.Policy
	params map[string]string
}

func withRoute(ctx context.Context, ri routeInfo) context.Context {
	return context.WithValue(ctx, routeKey{}, ri)
}

func routeFrom(ctx context.Context) routeInfo {
	ri, _ := ctx.Value(routeKey{}).(routeInfo)
	return ri
}

// route finds the operation's policy. Deny by default: an unknown route is a 404, and a route
// without a policy is refused (authz.New makes that impossible at startup, but the check stays).
func (s *Server) route(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, params, err := s.table.Find(r)
		switch {
		case errors.Is(err, authz.ErrNoRoute):
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such endpoint.")
			return
		case errors.Is(err, authz.ErrNoMethod):
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		case err != nil:
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(withRoute(r.Context(), routeInfo{policy: p, params: params})))
	})
}

// sameRoute runs after the ServeMux has matched (a per-route middleware of the generated
// handler). The policy and validation came from kin-openapi's router; dispatch comes from the
// mux. A request the two route differently (pattern or path values) is refused, so one router
// can't authorize what the other then serves.
func sameRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ri := routeFrom(r.Context())
		agree := r.Pattern == ri.policy.Pattern
		for name, v := range ri.params {
			agree = agree && r.PathValue(name) == v
		}
		if !agree {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such endpoint.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bodyCap limits the body to the operation's x-body-limit before anything reads it,
// authentication included (signed device requests hash the body to verify).
func bodyCap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := routeFrom(r.Context()).policy.BodyLimit
		if r.ContentLength > n {
			writeTooLarge(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}

// authenticate resolves the actor. Every authentication failure gets the same 401. On a public
// operation a dead credential is ignored (a stale cookie must not stop a sign-in).
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public := routeFrom(r.Context()).policy.Public
		a, ok, err := s.actors.Resolve(r.Context(), r)
		switch {
		case errors.Is(err, actor.ErrUnauthenticated):
			if !public {
				writeUnauthorized(w)
				return
			}
		case err != nil:
			s.log.ErrorContext(r.Context(), "actor lookup", "err", err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "unavailable", "Try again shortly.")
			return
		case !ok && !public:
			writeUnauthorized(w)
			return
		case ok:
			r = r.WithContext(actor.With(r.Context(), a))
		}
		next.ServeHTTP(w, r)
	})
}

// requireKey gives a command without an Idempotency-Key a clear 400 before the validator's
// generic one.
func requireKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if routeFrom(r.Context()).policy.Command && r.Header.Get(command.KeyHeader) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "idempotency_key_required", "Send an Idempotency-Key header with every change.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorize applies the operation's policy to the actor, after the request has been validated.
func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ri := routeFrom(r.Context())
		a, ok := actor.From(r.Context())
		if s.decide(w, ri.policy.Check(a, ok, ri.params, time.Now())) {
			next.ServeHTTP(w, r)
		}
	})
}

// decide writes the refusal for d and reports whether the request may go on.
func (s *Server) decide(w http.ResponseWriter, d authz.Decision) bool {
	switch d {
	case authz.Allow:
		return true
	case authz.Unauthenticated:
		writeUnauthorized(w)
	case authz.Reauthenticate:
		httpx.WriteError(w, http.StatusForbidden, "reauthentication_required", "Enter your password again to continue.")
	case authz.NotFound:
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Not found.")
	default:
		writeForbidden(w)
	}
	return false
}

// commands runs x-command operations through internal/command.
func (s *Server) commands(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := routeFrom(r.Context()).policy
		if !p.Command {
			next.ServeHTTP(w, r)
			return
		}
		ri := routeFrom(r.Context())
		s.runner.Wrap(command.Spec{Op: p.OperationID, RecordResponse: p.RecordResponse, Recheck: func(ctx context.Context, q *dbq.Queries, w http.ResponseWriter, a actor.Actor) (actor.Actor, bool) {
			fresh, err := actor.Resolver{Q: q}.Recheck(ctx, a)
			if errors.Is(err, actor.ErrUnauthenticated) {
				writeUnauthorized(w)
				return actor.Actor{}, false
			}
			if err != nil {
				s.log.ErrorContext(ctx, "actor recheck", "err", err)
				httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
				return actor.Actor{}, false
			}
			// Scopes or children may have changed while the command waited.
			if !s.decide(w, p.Check(fresh, true, ri.params, time.Now())) {
				return actor.Actor{}, false
			}
			return fresh, true
		}}, next, w, r)
	})
}

// mustCmd returns the running command. Handlers of x-command operations always have one.
func mustCmd(ctx context.Context) *command.Cmd {
	c, ok := command.From(ctx)
	if !ok {
		panic("server: command handler reached without a command")
	}
	return c
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="opencozy"`)
	httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
}

func writeForbidden(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusForbidden, "forbidden", "You can't do this.")
}

// mustActor returns the request's actor. Handlers run only after authorize, so a missing actor
// on a non-public operation is a bug.
func mustActor(ctx context.Context) actor.Actor {
	a, ok := actor.From(ctx)
	if !ok {
		panic("server: handler reached without an actor")
	}
	return a
}
