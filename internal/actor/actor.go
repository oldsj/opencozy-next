// Package actor resolves who is calling. It is the only package that reads credentials (cookies
// and bearer secrets); handlers get an Actor from the context and never look at either.
package actor

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Kind is what sort of caller an actor is. The names are the values of x-actors in the spec.
type Kind string

// Actor kinds.
const (
	Adult     Kind = "adult"      // adult web session (cookie)
	Phone     Kind = "phone"      // parent phone session (ocp_ bearer)
	Display   Kind = "display"    // wall display (cookie)
	Device    Kind = "device"     // paired device, signed requests (slice 7)
	DeviceWeb Kind = "device-web" // a device's web session (cookie, slice 7)
	Service   Kind = "service"    // ocz_ bearer
	Assistant Kind = "assistant"  // oca_ bearer
)

// Kinds lists every actor kind, for the spec check and the authorization matrix.
var Kinds = []Kind{Adult, Phone, Display, Device, DeviceWeb, Service, Assistant}

// Actor is a resolved, live caller. Its household comes only from its credential.
type Actor struct {
	Kind        Kind
	HouseholdID uuid.UUID
	// AdultID is set for Adult, Phone and Assistant.
	AdultID uuid.UUID
	// CredentialID is the session, credential or display row that authenticated the request.
	CredentialID uuid.UUID
	// Scopes are the credential's scopes (Service, Assistant) or the display's scopes (Display).
	Scopes []string
	// AllChildren is true when the actor may act for every child of the household; otherwise
	// Children lists the ones it may.
	AllChildren bool
	Children    []uuid.UUID
	// AuthenticatedAt is the adult's last password or passkey check (Adult only).
	AuthenticatedAt time.Time
	// TokenHash is the SHA-256 of the secret that authenticated the request: for sign-out,
	// re-confirmation and the re-check inside a command.
	TokenHash []byte
}

// HasScopes reports whether the actor holds every one of want.
func (a Actor) HasScopes(want []string) bool {
	for _, s := range want {
		if !slices.Contains(a.Scopes, s) {
			return false
		}
	}
	return true
}

// MayActFor reports whether the actor may act for the child. It does not check the child's
// household: queries do that, since they always take the actor's household.
func (a Actor) MayActFor(child uuid.UUID) bool {
	return a.AllChildren || slices.Contains(a.Children, child)
}

// IsAdult reports whether the actor is an adult, on the web or on a phone.
func (a Actor) IsAdult() bool { return a.Kind == Adult || a.Kind == Phone }

// ActsAsAdult reports whether what the actor does counts as an adult's: an adult, or an
// assistant acting for its adult (within its scopes).
func (a Actor) ActsAsAdult() bool { return a.IsAdult() || a.Kind == Assistant }

// Via is the assistant credential an adult's action came through, or nil when the adult acted
// themselves. Records keep it next to the adult ("via assistant").
func (a Actor) Via() *uuid.UUID {
	if a.Kind != Assistant {
		return nil
	}
	id := a.CredentialID
	return &id
}

type key struct{}

// With returns ctx carrying a.
func With(ctx context.Context, a Actor) context.Context { return context.WithValue(ctx, key{}, a) }

// From returns the request's actor, if it has one.
func From(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(key{}).(Actor)
	return a, ok
}
