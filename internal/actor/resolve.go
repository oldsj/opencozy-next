package actor

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Cookie names. The __Host- prefix makes the browser accept them only with Secure, Path=/ and no
// Domain, so a subdomain can't set or overwrite them.
const (
	AdultCookie     = "__Host-session"
	DisplayCookie   = "__Host-display"
	DeviceWebCookie = "__Host-device"
)

// ErrUnauthenticated means the request presented a credential that is malformed, unknown,
// expired or revoked. Every such case gets the same 401.
var ErrUnauthenticated = errors.New("actor: unauthenticated")

// Resolver looks credentials up. Every request is resolved against the database, so a
// revocation applies to the next request.
type Resolver struct {
	Q *dbq.Queries
}

// Resolve returns the request's actor. ok is false when the request carries no credential at
// all. A credential that is present but not live gives ErrUnauthenticated; other errors are
// database failures.
//
// A signed device request (X-OpenCozy-Device) is resolved from its signature alone, and a
// request with an Authorization header from it alone. A device web session cookie, when present,
// decides the request on its own: live is the device's child, anything else is
// ErrUnauthenticated. It never falls through to an adult cookie in the same jar (the child's web
// view must not turn into an adult's when its session ends). Otherwise the adult, then the
// display cookie is tried; the first live one wins. A present but dead one of those is skipped,
// not an error, because browsers keep stale cookies; only when no cookie is live and at least
// one was present is the result ErrUnauthenticated.
func (rv Resolver) Resolve(ctx context.Context, r *http.Request) (a Actor, ok bool, err error) {
	if r.Header.Get(DeviceHeader) != "" {
		a, err := rv.signed(ctx, r)
		return a, err == nil, err
	}
	if h := r.Header.Get("Authorization"); h != "" {
		a, err := rv.bearer(ctx, h)
		return a, err == nil, err
	}
	if c, err := r.Cookie(DeviceWebCookie); err == nil && c.Value != "" {
		if !auth.WellFormedCookie(c.Value) {
			return Actor{}, false, ErrUnauthenticated
		}
		a, err := rv.deviceWeb(ctx, c.Value)
		return a, err == nil, err
	}
	present := false
	for _, try := range []struct {
		name string
		fn   func(context.Context, string) (Actor, error)
	}{
		{AdultCookie, rv.adult},
		{DisplayCookie, rv.display},
	} {
		c, err := r.Cookie(try.name)
		if err != nil || c.Value == "" {
			continue
		}
		present = true
		if !auth.WellFormedCookie(c.Value) {
			continue
		}
		a, err := try.fn(ctx, c.Value)
		switch {
		case err == nil:
			return a, true, nil
		case errors.Is(err, ErrUnauthenticated):
			continue
		default:
			return Actor{}, false, err
		}
	}
	if present {
		return Actor{}, false, ErrUnauthenticated
	}
	return Actor{}, false, nil
}

func (rv Resolver) bearer(ctx context.Context, header string) (Actor, error) {
	scheme, secret, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return Actor{}, ErrUnauthenticated
	}
	prefix, ok := auth.ParseBearer(secret)
	if !ok {
		return Actor{}, ErrUnauthenticated
	}
	kind := map[string]Kind{auth.PrefixPhone: Phone, auth.PrefixService: Service, auth.PrefixAssistant: Assistant}[prefix]
	return rv.byHash(ctx, kind, auth.HashToken(secret))
}

func (rv Resolver) adult(ctx context.Context, token string) (Actor, error) {
	return rv.byHash(ctx, Adult, auth.HashToken(token))
}

func (rv Resolver) display(ctx context.Context, token string) (Actor, error) {
	return rv.byHash(ctx, Display, auth.HashToken(token))
}

// byHash looks up a live credential of the given kind by its token hash.
func (rv Resolver) byHash(ctx context.Context, kind Kind, hash []byte) (Actor, error) {
	switch kind {
	case Adult:
		row, err := rv.Q.GetAdultSession(ctx, hash)
		if err != nil {
			return Actor{}, notFound(err)
		}
		return Actor{Kind: Adult, HouseholdID: row.HouseholdID, AdultID: row.AdultID, AllChildren: true,
			AuthenticatedAt: row.AuthenticatedAt, TokenHash: hash}, nil
	case Display:
		row, err := rv.Q.GetDisplayByHash(ctx, hash)
		if err != nil {
			return Actor{}, notFound(err)
		}
		return Actor{Kind: Display, HouseholdID: row.HouseholdID, CredentialID: row.ID, Scopes: row.Scopes, AllChildren: true, TokenHash: hash}, nil
	case Phone:
		row, err := rv.Q.GetPhoneSessionByHash(ctx, hash)
		if err != nil {
			return Actor{}, notFound(err)
		}
		return Actor{Kind: Phone, HouseholdID: row.HouseholdID, AdultID: row.AdultID, CredentialID: row.ID, AllChildren: true, TokenHash: hash}, nil
	case DeviceWeb:
		row, err := rv.Q.GetDeviceWebSession(ctx, hash)
		if err != nil {
			return Actor{}, notFound(err)
		}
		a := deviceActor(row.HouseholdID, row.DeviceID, row.ChildID)
		a.Kind, a.TokenHash = DeviceWeb, hash
		return a, nil
	case Service, Assistant:
		row, err := rv.Q.GetCredentialByHash(ctx, hash)
		if err != nil {
			return Actor{}, notFound(err)
		}
		// The prefix must match the stored kind, so a secret can't change what it is.
		if Kind(row.Kind) != kind {
			return Actor{}, ErrUnauthenticated
		}
		a := Actor{Kind: kind, HouseholdID: row.HouseholdID, CredentialID: row.ID, Scopes: row.Scopes, AllChildren: row.AllChildren, TokenHash: hash}
		if row.AdultID != nil {
			a.AdultID = *row.AdultID
		}
		if !row.AllChildren {
			ids, err := rv.Q.ListCredentialChildIDs(ctx, dbq.ListCredentialChildIDsParams{HouseholdID: row.HouseholdID, CredentialID: row.ID})
			if err != nil {
				return Actor{}, err
			}
			a.Children = ids
		}
		return a, nil
	}
	return Actor{}, ErrUnauthenticated
}

// Recheck looks the actor's own credential up again and returns its current state: its scopes
// and children may have changed. A command calls it inside its transaction, after the household
// lock, so a revocation that committed while the command waited is seen (design rule 4).
// ErrUnauthenticated if the credential is gone or no longer the same one.
func (rv Resolver) Recheck(ctx context.Context, a Actor) (Actor, error) {
	var fresh Actor
	var err error
	if a.Kind == Device {
		fresh, err = rv.device(ctx, a.CredentialID)
	} else {
		fresh, err = rv.byHash(ctx, a.Kind, a.TokenHash)
	}
	if err != nil {
		return Actor{}, err
	}
	if fresh.HouseholdID != a.HouseholdID || fresh.AdultID != a.AdultID || fresh.CredentialID != a.CredentialID {
		return Actor{}, ErrUnauthenticated
	}
	return fresh, nil
}

// deviceWeb resolves a device web session (the kid view in the app's web view).
func (rv Resolver) deviceWeb(ctx context.Context, token string) (Actor, error) {
	return rv.byHash(ctx, DeviceWeb, auth.HashToken(token))
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	return err
}
