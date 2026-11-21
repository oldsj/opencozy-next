package push

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Notification is what one push says. It is stored in push_outbox and rendered for APNs when it
// is sent, so the badge (the inbox count) is the count at delivery, not at the event.
//
// A notification with a Title is an alert; one without is a silent background update (push type
// background), which the app uses to remove a delivered alert for something already answered
// and to correct its badge. A muted adult (adult.push_muted_until in the future) gets only the
// silent ones.
type Notification struct {
	// Type names the event for the app, such as "approval.pending".
	Type string `json:"type"`
	// Collapse is the APNs collapse ID: a later push with the same one replaces the delivered
	// notification (a reminder replaces the first ask). At most 64 bytes.
	Collapse string `json:"collapse"`
	Title    string `json:"title,omitempty"`
	Body     string `json:"body,omitempty"`
	// Category selects the app's notification actions (APPROVAL, TIME_REQUEST, TV_WATCHING).
	Category string `json:"category,omitempty"`
	// Thread groups notifications in Notification Centre.
	Thread string `json:"thread,omitempty"`
	// Badge sets the app badge to the household's waiting inbox count (adults only).
	Badge bool `json:"badge,omitempty"`
	// Passive delivers an alert without sound or lighting the screen.
	Passive bool `json:"passive,omitempty"`
	// Wake also wakes the app in the background to sync (content-available) with an alert.
	Wake bool `json:"wake,omitempty"`
	// Data goes to the app under "opencozy", beside "type" (and "pendingCount" with Badge).
	Data map[string]string `json:"data,omitempty"`
}

// Alert reports whether n is shown to the person, rather than a silent update.
func (n Notification) Alert() bool { return n.Title != "" }

// APNs builds the APNs payload. pending is the inbox count, used when n.Badge is set.
func (n Notification) APNs(pending int) map[string]any {
	app := map[string]any{"type": n.Type}
	for k, v := range n.Data {
		app[k] = v
	}
	if n.Badge {
		app["pendingCount"] = pending
	}
	if !n.Alert() {
		// APNs requires a background push to carry content-available and no alert, sound or badge.
		return map[string]any{"aps": map[string]any{"content-available": 1}, "opencozy": app}
	}
	aps := map[string]any{"alert": map[string]any{"title": n.Title, "body": n.Body}}
	if n.Passive {
		aps["interruption-level"] = "passive"
	} else {
		aps["sound"] = "default"
	}
	if n.Badge {
		aps["badge"] = pending
	}
	if n.Category != "" {
		aps["category"] = n.Category
	}
	if n.Thread != "" {
		aps["thread-id"] = n.Thread
	}
	if n.Wake {
		aps["content-available"] = 1
	}
	return map[string]any{"aps": aps, "opencozy": app}
}

// Events that push today. Slices 8 (screen time) and 9 (TV) add theirs here: a child's time
// request is an inbox item, so it pushes as an approval; "Who's watching?" prompts and the kid
// device's pushes (time ended, time answered, TV waiting or ending) follow the same pattern with
// ToAdults or ToChild in the event's own command.

// ApprovalPending tells the parents something is waiting in the inbox: a finished chore or
// routine, a reward to approve, or a request for more time. askedAt is when it was asked: a
// child asking again later (old #74) pushes again, replacing the first notification, because the
// collapse ID stays the item's and the dedupe key changes.
func ApprovalPending(a dbq.Approval, childName string, askedAt time.Time) (key string, n Notification) {
	title := fmt.Sprintf("%s finished %s", childName, a.Title)
	body := fmt.Sprintf("%s · waiting for your approval", points(a.Points))
	switch a.Kind {
	case "redemption":
		title = fmt.Sprintf("%s wants %s", childName, a.Title)
	case "time_request":
		title = fmt.Sprintf("%s is asking for more iPad time", childName)
		body = "Give 30 min, 1 hour, the rest of the day, or say no."
	}
	return "approval.pending/" + a.ID.String() + "/" + strconv.FormatInt(askedAt.Unix(), 10), Notification{
		Type: "approval.pending", Collapse: a.ID.String(), Title: title, Body: body,
		Category: "APPROVAL", Thread: "opencozy-approvals", Badge: true,
		Data: map[string]string{"itemId": a.ID.String(), "kind": a.Kind},
	}
}

// ApprovalResolved is the silent update after an inbox item is decided: every parent's phone
// removes its notification and corrects the badge.
func ApprovalResolved(id uuid.UUID, state string) (key string, n Notification) {
	return "approval.resolved/" + id.String(), Notification{
		Type: "approval.resolved", Collapse: id.String(), Badge: true,
		Data: map[string]string{"itemId": id.String(), "state": state},
	}
}

func points(n int32) string {
	if n == 1 {
		return "1 point"
	}
	return fmt.Sprintf("%d points", n)
}

// ToAdults enqueues n for every adult of the household with a phone that can receive it, in the
// event's transaction (q). key identifies the event; each adult's row adds their ID to it.
func (o Outbox) ToAdults(ctx context.Context, q *dbq.Queries, household uuid.UUID, key string, n Notification) error {
	if !o.Enabled {
		return nil
	}
	adults, err := q.PushAdults(ctx, household)
	if err != nil {
		return err
	}
	for _, id := range adults {
		if err := o.Enqueue(ctx, q, Message{HouseholdID: household, AdultID: &id, DedupeKey: key + "/" + id.String(), Notification: n}); err != nil {
			return err
		}
	}
	return nil
}

// ToChild enqueues n for a child's paired devices, in the event's transaction (q).
func (o Outbox) ToChild(ctx context.Context, q *dbq.Queries, household, child uuid.UUID, key string, n Notification) error {
	n.Badge = false // a kid's app has no inbox
	return o.Enqueue(ctx, q, Message{HouseholdID: household, ChildID: &child, DedupeKey: key, Notification: n})
}
