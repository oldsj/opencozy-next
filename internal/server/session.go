package server

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// startSession stores a new adult web session and returns its Set-Cookie value.
func (s *Server) startSession(ctx context.Context, adultID uuid.UUID) (string, error) {
	token, hash, err := auth.NewSessionToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(s.cfg.SessionTTL)
	if err := s.q.CreateAdultSession(ctx, dbq.CreateAdultSessionParams{TokenHash: hash, AdultID: adultID, ExpiresAt: expires}); err != nil {
		return "", err
	}
	return sessionCookie(token, int(s.cfg.SessionTTL.Seconds())), nil
}

func sessionCookie(value string, maxAge int) string {
	c := &http.Cookie{
		Name:     actor.AdultCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true, // browsers allow Secure cookies on http://localhost, so development works
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}

// SweepSessions deletes expired adult sessions every hour until ctx ends.
func (s *Server) SweepSessions(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		n, err := s.q.DeleteExpiredAdultSessions(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.WarnContext(ctx, "session sweep", "err", err)
		} else if n > 0 {
			s.log.InfoContext(ctx, "expired sessions deleted", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
