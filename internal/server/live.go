package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/authz"
	"github.com/oldsj/opencozy-next/internal/listen"
	"github.com/oldsj/opencozy-next/internal/live"
	"github.com/oldsj/opencozy-next/internal/push"
)

// StartLive starts this replica's notification listener and live hub. Call it before serving.
func (s *Server) StartLive(ctx context.Context) {
	l := listen.New(s.pool, s.log, live.Channel, push.Channel)
	hub := live.NewHub(s.log)
	s.live.Store(hub)
	s.pushNotes = l.C(push.Channel)
	go l.Run(ctx)
	go hub.Run(ctx, l.C(live.Channel))
}

// PushNotes wakes the push sender on NOTIFY push_outbox. Nil before StartLive.
func (s *Server) PushNotes() <-chan listen.Note { return s.pushNotes }

// serveLive answers openLive after the whole middleware chain (route policy, authentication,
// validation, authorization) has run; the strict handler cannot hijack the connection.
func (s *Server) serveLive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ri := routeFrom(r.Context())
		if ri.policy.OperationID != "OpenLive" { // authz keeps the generated (Go) name
			next.ServeHTTP(w, r)
			return
		}
		s.live.Load().Serve(w, r, mustActor(r.Context()), func(ctx context.Context, a actor.Actor) (actor.Actor, bool, error) {
			fresh, err := actor.Resolver{Q: s.q}.Recheck(ctx, a)
			if errors.Is(err, actor.ErrUnauthenticated) {
				return actor.Actor{}, false, nil
			}
			if err != nil {
				return actor.Actor{}, false, err
			}
			return fresh, ri.policy.Check(fresh, true, ri.params, time.Now()) == authz.Allow, nil
		})
	})
}

// OpenLive is never reached: serveLive answers the operation first.
func (s *Server) OpenLive(context.Context, api.OpenLiveRequestObject) (api.OpenLiveResponseObject, error) {
	return nil, errors.New("server: openLive reached the strict handler")
}
