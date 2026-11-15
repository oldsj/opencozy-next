// Package server wires the HTTP stack together and implements api.StrictServerInterface.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/jackc/pgx/v5/pgxpool"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/authz"
	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/httpx"
	"github.com/oldsj/opencozy-next/internal/live"
	"github.com/oldsj/opencozy-next/web"
)

// Server holds what the handlers need.
type Server struct {
	cfg      config.Config
	log      *slog.Logger
	pool     *pgxpool.Pool
	q        *dbq.Queries
	actors   actor.Resolver
	table    *authz.Table
	runner   command.Runner
	draining atomic.Bool
	live     atomic.Pointer[live.Hub] // slice 0d: StartLive
	// calendarWake starts the calendar refresh early (slice 13a).
	calendarWake chan struct{}
}

var _ api.StrictServerInterface = (*Server)(nil)

// New makes a Server.
func New(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) *Server {
	q := dbq.New(pool)
	return &Server{cfg: cfg, log: log, pool: pool, q: q, actors: actor.Resolver{Q: q}, runner: command.Runner{Pool: pool, Log: log}, calendarWake: make(chan struct{}, 1)}
}

// Drain makes /readyz fail, so the load balancer stops sending traffic before shutdown.
func (s *Server) Drain() { s.draining.Store(true) }

// Handler is the whole application: probes, the API under /api and the SPA.
func (s *Server) Handler() (http.Handler, error) {
	apiHandler, err := s.apiHandler()
	if err != nil {
		return nil, err
	}

	app := http.NewServeMux()
	app.Handle("/api/", http.StripPrefix("/api", apiHandler))
	app.Handle("/", web.Handler(web.Dist()))

	// Go's CSRF defence: refuses cross-origin unsafe requests using Sec-Fetch-Site and Origin.
	// Together with SameSite=Lax cookies this needs no tokens. Bearer requests carry no cookies,
	// and native clients send neither header, so they pass.
	csrf := http.NewCrossOriginProtection()
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusForbidden, "cross_origin", "Cross-origin request refused.")
	}))

	root := http.NewServeMux()
	// Probes skip the access log and middleware; they would only add noise.
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	root.HandleFunc("GET /readyz", s.ready)
	root.Handle("/", httpx.Chain(app,
		httpx.Recover(s.log),
		httpx.ClientIP(s.cfg.TrustedProxies),
		httpx.AccessLog(s.log),
		httpx.SecurityHeaders,
		csrf.Handler,
	))
	return root, nil
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.draining.Load() {
		http.Error(w, "draining", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiHandler runs every API request through, in order: the route's policy (deny by default),
// the body cap, rate limits, authentication, the Idempotency-Key check for commands, the request
// validator, authorization, the command runner, and only then the handler. Authentication comes before anything parses the body.
func (s *Server) apiHandler() (http.Handler, error) {
	spec, err := api.GetSpec()
	if err != nil {
		return nil, err
	}
	spec.Servers = nil // mounted under /api with the prefix stripped
	// kin-openapi checks only the formats it is told about.
	openapi3.DefineStringFormatValidator("email", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForEmail))

	if s.table, err = authz.New(spec); err != nil {
		return nil, err
	}

	validator := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		DoNotValidateServers: true,
		// Who may call an operation is x-actors (internal/authz), not OpenAPI security.
		Options:              openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		ErrorHandlerWithOpts: s.validationError,
	})

	strict := api.NewStrictHandlerWithOptions(s, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			if tooLarge(err) {
				writeTooLarge(w)
				return
			}
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The request body could not be read.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.ErrorContext(r.Context(), "handler failed", "path", httpx.LogSafe(r.URL.Path), "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		},
	})
	handler := api.HandlerWithOptions(strict, api.StdHTTPServerOptions{BaseRouter: http.NewServeMux(), Middlewares: []api.MiddlewareFunc{sameRoute}})

	// Sign-in and pairing have separate buckets: a display polling its pairing every few seconds
	// must not use up the sign-in attempts of an adult at the same address.
	limited := httpx.NewRateLimiter(s.cfg.AuthRatePerMinute).Middleware
	pairing := httpx.NewRateLimiter(pairingRatePerMinute).Middleware
	limitAuth := func(next http.Handler) http.Handler {
		auth, pair := limited(next), pairing(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method != http.MethodPost:
				next.ServeHTTP(w, r)
			case r.URL.Path == "/auth/login" || r.URL.Path == "/auth/confirm":
				auth.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/display/pairings"):
				pair.ServeHTTP(w, r)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}

	return httpx.Chain(handler,
		noStore,
		s.route,
		bodyCap,
		limitAuth,
		s.authenticate,
		requireKey,
		validator,
		s.authorize,
		s.serveLive,
		s.commands,
	), nil
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validationError(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
	switch {
	case tooLarge(err):
		writeTooLarge(w)
	case opts.MatchedRoute == nil && opts.StatusCode == http.StatusNotFound:
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such endpoint.")
	case opts.StatusCode == http.StatusMethodNotAllowed:
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	default:
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", validationMessage(err))
	}
}

func tooLarge(err error) bool {
	var mb *http.MaxBytesError
	return errors.As(err, &mb)
}

func writeTooLarge(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "The request body is too large.")
}

// validationMessage keeps the first line of the validator's message, which names the field and
// the rule, and drops the schema dump that follows.
func validationMessage(err error) string {
	msg, _, _ := strings.Cut(err.Error(), "\n")
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}
