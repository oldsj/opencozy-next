package screentime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/command"
	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// "Ask for more time". A child asks from their own device (the shield, sent by the app, or the
// kid view), at any hour. Parents answer once: 30 minutes, an hour, all day, or no. A yes is a
// parent's grant spent at once: it queues after anything already running, ignores allowed hours,
// and never draws on the child's own bank. "All day" lasts until the end of today's allowed
// hours, or until midnight when the child has no rule or today's hours are over.

const (
	// AskAgainAfter is how long a child waits after a "no" before asking again.
	AskAgainAfter = 5 * time.Minute
	// RemindAfter is how often asking again, while a request waits, reminds the parents.
	RemindAfter = 2 * time.Minute
	// ShownFor is how long a decided request is still shown to the child.
	ShownFor = time.Hour
	// RequestTitle is the inbox item's title.
	RequestTitle = "More iPad time"
)

// Choices.
const (
	Choice30Min  = "30min"
	Choice1Hour  = "1h"
	ChoiceAllDay = "all_day"
	ChoiceNo     = "no"
)

// ErrAskedTooSoon refuses asking again shortly after a "no".
type ErrAskedTooSoon struct{ Until time.Time }

func (e ErrAskedTooSoon) Error() string {
	return fmt.Sprintf("screentime: ask again after %s", e.Until)
}

// ErrAlreadyUntil refuses "all day" when the child already has time queued past its end.
type ErrAlreadyUntil struct{ Until time.Time }

func (e ErrAlreadyUntil) Error() string { return fmt.Sprintf("screentime: already until %s", e.Until) }

// Asked is a request after asking: New when it was made now, Reminded when a waiting one was
// asked again (its parents are reminded).
type Asked struct {
	Request       dbq.TimeRequest
	New, Reminded bool
}

// Ask makes a request for the child, or returns the one already waiting (reminding the parents if
// it was asked at least RemindAfter ago). A new request is also an inbox item.
func Ask(ctx context.Context, c *command.Cmd, child uuid.UUID, device *uuid.UUID) (Asked, error) {
	if !c.Holds(child) {
		return Asked{}, ErrNotLocked
	}
	h := c.HouseholdID()
	now, err := c.Now(ctx)
	if err != nil {
		return Asked{}, err
	}
	latest, err := c.Q.LatestTimeRequest(ctx, dbq.LatestTimeRequestParams{HouseholdID: h, ChildID: child})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Asked{}, err
	case latest.State == "pending":
		if now.Sub(latest.AskedAt) < RemindAfter {
			return Asked{Request: latest}, nil
		}
		r, err := c.Q.RemindTimeRequest(ctx, dbq.RemindTimeRequestParams{HouseholdID: h, ID: latest.ID, Now: now})
		return Asked{Request: r, Reminded: true}, err
	case latest.State == "declined" && latest.DecidedAt != nil && now.Sub(*latest.DecidedAt) < AskAgainAfter:
		return Asked{}, ErrAskedTooSoon{Until: latest.DecidedAt.Add(AskAgainAfter)}
	}
	r, err := c.Q.InsertTimeRequest(ctx, dbq.InsertTimeRequestParams{HouseholdID: h, ChildID: child, DeviceID: device, Now: now})
	if err != nil {
		return Asked{}, err
	}
	if _, err := c.Q.CreateTimeRequestApproval(ctx, dbq.CreateTimeRequestApprovalParams{
		HouseholdID: h, ChildID: child, TimeRequestID: &r.ID, Title: RequestTitle,
	}); err != nil {
		return Asked{}, err
	}
	return Asked{Request: r, New: true}, nil
}

// Answer decides a waiting request (locked by the caller, with its child). A yes banks a parent's
// grant and spends it at once.
func Answer(ctx context.Context, c *command.Cmd, req dbq.TimeRequest, choice string, by uuid.UUID) (dbq.TimeRequest, error) {
	if !c.Holds(req.ChildID) {
		return dbq.TimeRequest{}, ErrNotLocked
	}
	h := c.HouseholdID()
	now, err := c.Now(ctx)
	if err != nil {
		return dbq.TimeRequest{}, err
	}
	p := dbq.DecideTimeRequestParams{HouseholdID: h, ID: req.ID, State: "declined", Choice: &choice, DecidedBy: &by, Now: &now}
	if choice != ChoiceNo {
		ws, err := Windows(ctx, c.Q, h, req.ChildID, now)
		if err != nil {
			return dbq.TimeRequest{}, err
		}
		start := now
		if end := queueEnd(ws); end != nil {
			start = *end
		}
		var seconds int64
		switch choice {
		case Choice30Min:
			seconds = 30 * 60
		case Choice1Hour:
			seconds = 60 * 60
		case ChoiceAllDay:
			rule, err := ChildRule(ctx, c.Q, h, req.ChildID)
			if err != nil {
				return dbq.TimeRequest{}, err
			}
			end := day.AllDayEnd(rule.Hours, now, rule.Zone)
			// Whole seconds, rounded up: it may run past its end by under a second.
			seconds = int64((end.Sub(start) + time.Second - 1) / time.Second)
			if seconds <= 0 {
				return dbq.TimeRequest{}, ErrAlreadyUntil{Until: start}
			}
		default:
			return dbq.TimeRequest{}, fmt.Errorf("screentime: unknown choice %q", choice)
		}
		minutes := min(MaxGrantMinutes, (seconds+59)/60)
		seconds = min(seconds, minutes*60)
		cr, err := AddCredit(ctx, c, Credit{
			Child: req.ChildID, Source: SourceTimeRequest, GrantedBy: &by, Minutes: int32(minutes), Seconds: int32(seconds), //nolint:gosec // at most a day
		})
		if err != nil {
			return dbq.TimeRequest{}, err
		}
		spent, err := Spend(ctx, c, req.ChildID, seconds, &cr.ID, nil)
		if err != nil {
			return dbq.TimeRequest{}, err
		}
		p.State, p.CreditID, p.GrantedUntil = "approved", &cr.ID, &spent.EndsAt
	}
	return c.Q.DecideTimeRequest(ctx, p)
}

// Shown is the child's latest request while it waits, or for ShownFor after its answer; nil
// otherwise.
func Shown(ctx context.Context, q *dbq.Queries, householdID, childID uuid.UUID, now time.Time) (*dbq.TimeRequest, error) {
	r, err := q.LatestTimeRequest(ctx, dbq.LatestTimeRequestParams{HouseholdID: householdID, ChildID: childID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if r.State == "pending" || (r.DecidedAt != nil && now.Sub(*r.DecidedAt) < ShownFor) {
		return &r, nil
	}
	return nil, nil
}
