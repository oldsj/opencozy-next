package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/config"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/egress"
)

// Single sign-on with an OpenID Connect provider (design §6, slice 10b): the authorization code
// flow with PKCE (S256), a state bound to the browser by a cookie, and a nonce checked against
// the signed ID token. Discovery, the token exchange and the provider's keys are fetched only
// through internal/egress.
//
// It never creates anyone. A provider account (issuer, subject) signs in the adult it is linked
// to. With no link yet, it links to the adult whose email is the provider's email, and only when
// the provider says that email is verified; an adult already linked to another account at the
// same issuer is refused rather than given a second link.

const (
	// oidcCookie binds a sign-in to the browser that started it. __Secure- rather than __Host-
	// so it can be scoped to the callback's path; it is sent nowhere else.
	oidcCookie       = "__Secure-oidc"
	oidcSignInTTL    = 10 * time.Minute
	oidcFetchTimeout = 15 * time.Second
	// oidcRediscover is how long discovery's answer is kept. A failed refresh keeps the old one.
	oidcRediscover = time.Hour
)

var errOIDCOff = apiError("oidc_off", "Single sign-on isn’t set up here.")

// oidcFlow is the configured provider. Its discovery is fetched on first use, not at startup,
// so a provider that is down doesn't stop OpenCozy from starting.
type oidcFlow struct {
	issuer, clientID, clientSecret, redirectURL, name string
	// with puts the HTTP client every request to the provider goes through into a context:
	// the egress client (egress.Context), except in tests.
	with func(context.Context) context.Context

	mu  sync.Mutex
	cur *oidcProvider
}

type oidcProvider struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	// p answers userinfo, for providers that keep the email out of the ID token.
	p       *oidc.Provider
	fetched time.Time
}

// oidcIdentity is what a verified ID token says about the person.
type oidcIdentity struct {
	subject, email string
	emailVerified  bool
}

// newOIDCFlow builds the flow from the configuration. with is nil except in tests, which pass
// one carrying a client that reaches their fake provider.
func newOIDCFlow(cfg config.Config, with func(context.Context) context.Context) (*oidcFlow, error) {
	if with == nil {
		u, err := url.Parse(cfg.OIDCIssuer)
		if err != nil {
			return nil, err
		}
		ports := []uint16{443}
		if p := u.Port(); p != "" && p != "443" {
			var n uint16
			if _, err := fmt.Sscan(p, &n); err != nil {
				return nil, fmt.Errorf("issuer port %q", p)
			}
			ports = append(ports, n)
		}
		client, err := egress.New(egress.Options{
			Name: egress.OIDCClient, PrivateCIDRs: cfg.OIDCAllowedPrivateCIDRs, Ports: ports,
			Timeout: oidcFetchTimeout, MaxResponseBytes: 1 << 20,
		})
		if err != nil {
			return nil, err
		}
		with = func(ctx context.Context) context.Context { return egress.Context(ctx, client) }
	}
	return &oidcFlow{
		issuer: cfg.OIDCIssuer, clientID: cfg.OIDCClientID, clientSecret: cfg.OIDCClientSecret,
		redirectURL: cfg.PublicOrigin + config.OIDCCallbackPath, name: strings.TrimSpace(cfg.OIDCName), with: with,
	}, nil
}

// provider returns the provider's endpoints and ID-token verifier, from discovery.
func (f *oidcFlow) provider(ctx context.Context) (*oidcProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur != nil && time.Since(f.cur.fetched) < oidcRediscover {
		return f.cur, nil
	}
	p, err := f.discover(ctx)
	if err != nil {
		if f.cur != nil {
			return f.cur, nil // the last good answer beats none
		}
		return nil, err
	}
	f.cur = p
	return p, nil
}

func (f *oidcFlow) discover(ctx context.Context) (*oidcProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcFetchTimeout)
	defer cancel()
	p, err := oidc.NewProvider(f.with(ctx), f.issuer)
	if err != nil {
		return nil, err
	}
	var meta struct {
		JWKS     string `json:"jwks_uri"`
		UserInfo string `json:"userinfo_endpoint"`
	}
	if err := p.Claims(&meta); err != nil {
		return nil, err
	}
	ep := p.Endpoint()
	// The endpoints use the issuer's scheme: https, except an e2e build's loopback provider.
	scheme := "https"
	if u, err := url.Parse(f.issuer); err == nil {
		scheme = u.Scheme
	}
	endpoints := []string{ep.AuthURL, ep.TokenURL, meta.JWKS}
	if meta.UserInfo != "" { // optional: only asked when the ID token has no email
		endpoints = append(endpoints, meta.UserInfo)
	}
	for _, e := range endpoints {
		if u, err := url.Parse(e); err != nil || u.Scheme != scheme || u.Host == "" {
			return nil, fmt.Errorf("oidc discovery: endpoint %q is not %s", e, scheme)
		}
	}
	// The key set fetches later, on its own schedule: give it the egress client too. Its context
	// is only a carrier for the client (go-oidc ignores its cancellation).
	keys := f.with(context.Background())
	return &oidcProvider{
		oauth: oauth2.Config{
			ClientID: f.clientID, ClientSecret: f.clientSecret, Endpoint: ep,
			RedirectURL: f.redirectURL, Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: p.VerifierContext(keys, &oidc.Config{ClientID: f.clientID}),
		p:        p,
		fetched:  time.Now(),
	}, nil
}

// identify exchanges the code (with the PKCE verifier) and verifies the ID token: signature,
// issuer, audience, expiry and nonce.
func (f *oidcFlow) identify(ctx context.Context, p *oidcProvider, code, verifier, nonce string) (oidcIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcFetchTimeout)
	defer cancel()
	tok, err := p.oauth.Exchange(f.with(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return oidcIdentity{}, fmt.Errorf("token exchange: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return oidcIdentity{}, errors.New("no ID token")
	}
	idt, err := p.verifier.Verify(f.with(ctx), raw)
	if err != nil {
		return oidcIdentity{}, fmt.Errorf("ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return oidcIdentity{}, errors.New("ID token: wrong nonce")
	}
	var c struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
		AZP           string `json:"azp"`
	}
	if err := idt.Claims(&c); err != nil {
		return oidcIdentity{}, fmt.Errorf("ID token claims: %w", err)
	}
	if len(idt.Audience) > 1 && c.AZP != f.clientID {
		return oidcIdentity{}, errors.New("ID token: issued to another party")
	}
	if idt.Subject == "" || len(idt.Subject) > 255 {
		return oidcIdentity{}, errors.New("ID token: bad subject")
	}
	email, verified := c.Email, emailVerified(c.EmailVerified)
	if email == "" {
		// OIDC Core §5.4: in the code flow a provider may return the email scope's claims only
		// from userinfo. Asked through egress, and its answer must be about the same subject.
		email, verified, err = f.userInfo(ctx, p, tok, idt.Subject)
		if err != nil {
			return oidcIdentity{}, err
		}
	}
	return oidcIdentity{subject: idt.Subject, email: normalizeEmail(email), emailVerified: verified}, nil
}

// userInfo asks the provider's userinfo endpoint for the email of subject. A provider with no
// userinfo endpoint answers no email (so the sign-in can't link by email).
func (f *oidcFlow) userInfo(ctx context.Context, p *oidcProvider, tok *oauth2.Token, subject string) (string, bool, error) {
	var meta struct {
		UserInfo string `json:"userinfo_endpoint"`
	}
	if err := p.p.Claims(&meta); err != nil || meta.UserInfo == "" {
		return "", false, nil
	}
	info, err := p.p.UserInfo(f.with(ctx), oauth2.StaticTokenSource(tok))
	if err != nil {
		return "", false, fmt.Errorf("userinfo: %w", err)
	}
	if info.Subject != subject { // §5.3.2: otherwise the response must not be used
		return "", false, errors.New("userinfo: another subject")
	}
	var c struct {
		Email         string `json:"email"`
		EmailVerified any    `json:"email_verified"`
	}
	if err := info.Claims(&c); err != nil {
		return "", false, fmt.Errorf("userinfo claims: %w", err)
	}
	return c.Email, emailVerified(c.EmailVerified), nil
}

// emailVerified reads email_verified, which some providers send as a string.
func emailVerified(v any) bool { return v == true || v == "true" }

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func oidcBindingCookie(value string, maxAge int) string {
	c := &http.Cookie{
		Name: oidcCookie, Value: value, Path: config.OIDCCallbackPath, MaxAge: maxAge,
		HttpOnly: true, Secure: true,
		// Lax: the provider's redirect back is a top-level GET navigation from another site.
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}

// GetSignInMethods says which ways of signing in the sign-in page should offer.
func (s *Server) GetSignInMethods(_ context.Context, _ api.GetSignInMethodsRequestObject) (api.GetSignInMethodsResponseObject, error) {
	m := api.SignInMethods{Passkeys: s.wa != nil}
	if s.oidc != nil {
		m.Oidc = &api.OidcProvider{Name: s.oidc.name}
	}
	return api.GetSignInMethods200JSONResponse(m), nil
}

// StartOidcSignIn starts a sign-in: it stores the state, PKCE verifier and nonce, binds them to
// this browser with a cookie, and answers the provider's authorization URL.
func (s *Server) StartOidcSignIn(ctx context.Context, _ api.StartOidcSignInRequestObject) (api.StartOidcSignInResponseObject, error) {
	if s.oidc == nil {
		return api.StartOidcSignIn404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errOIDCOff)}, nil
	}
	if onKidDevice(ctx) {
		return api.StartOidcSignIn409JSONResponse(errKidDevice), nil
	}
	p, err := s.oidc.provider(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "oidc discovery failed", "err", err)
		return api.StartOidcSignIn503JSONResponse(apiError("oidc_unavailable", "Single sign-on can’t be reached right now. Try again, or sign in another way.")), nil
	}
	state, err := randomToken()
	if err != nil {
		return nil, err
	}
	binding, err := randomToken()
	if err != nil {
		return nil, err
	}
	nonce, err := randomToken()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()
	if err := s.q.CreateOIDCSignIn(ctx, dbq.CreateOIDCSignInParams{
		StateHash: auth.HashToken(state), BindingHash: auth.HashToken(binding), CodeVerifier: verifier, Nonce: nonce,
	}); err != nil {
		return nil, err
	}
	cookie := oidcBindingCookie(binding, int(oidcSignInTTL.Seconds()))
	resp := api.StartOidcSignIn200JSONResponse{Headers: api.StartOidcSignIn200ResponseHeaders{SetCookie: &cookie}}
	resp.Body.Url = p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
	return resp, nil
}

// OidcCallback finishes a sign-in. Every outcome is a redirect: home with a new session, or the
// sign-in page with an error code it can put into words.
func (s *Server) OidcCallback(ctx context.Context, req api.OidcCallbackRequestObject) (api.OidcCallbackResponseObject, error) {
	adult, refusal, err := s.oidcCallback(ctx, req.Params)
	if err != nil {
		return nil, err
	}
	if refusal != "" {
		s.log.InfoContext(ctx, "oidc sign-in refused", "reason", refusal)
		to := "/login?error=" + url.QueryEscape(refusal)
		return api.OidcCallback303Response{Headers: api.OidcCallback303ResponseHeaders{Location: &to}}, nil
	}
	cookie, err := s.startSession(ctx, adult.ID)
	if err != nil {
		return nil, err
	}
	home := "/"
	return api.OidcCallback303Response{Headers: api.OidcCallback303ResponseHeaders{Location: &home, SetCookie: &cookie}}, nil
}

// oidcCallback returns the adult to sign in, or the error code to send the browser back with.
func (s *Server) oidcCallback(ctx context.Context, p api.OidcCallbackParams) (dbq.Adult, string, error) {
	const failed = "oidc_failed"
	if s.oidc == nil {
		return dbq.Adult{}, "oidc_off", nil
	}
	if onKidDevice(ctx) {
		return dbq.Adult{}, "kid_device", nil
	}
	if p.State == nil || *p.State == "" {
		return dbq.Adult{}, failed, nil
	}
	// Taken before anything else is checked, so a state is tried at most once.
	row, err := s.q.TakeOIDCSignIn(ctx, auth.HashToken(*p.State))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Adult{}, failed, nil
	}
	if err != nil {
		return dbq.Adult{}, "", err
	}
	// The browser that comes back must be the one that started: its cookie matches the row's.
	binding := p.UnderscoreUnderscoreSecureOidc
	if binding == nil || subtle.ConstantTimeCompare(auth.HashToken(*binding), row.BindingHash) != 1 {
		return dbq.Adult{}, failed, nil
	}
	if p.Error != nil || p.Code == nil || *p.Code == "" {
		return dbq.Adult{}, failed, nil // declined at the provider, or a broken redirect
	}
	if p.Iss != nil && *p.Iss != s.oidc.issuer { // RFC 9207: a response from another issuer
		return dbq.Adult{}, failed, nil
	}
	prov, err := s.oidc.provider(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "oidc discovery failed", "err", err)
		return dbq.Adult{}, failed, nil
	}
	id, err := s.oidc.identify(ctx, prov, *p.Code, row.CodeVerifier, row.Nonce)
	if err != nil {
		s.log.WarnContext(ctx, "oidc sign-in: provider answer refused", "err", err)
		return dbq.Adult{}, failed, nil
	}
	return s.oidcAdult(ctx, id)
}

// oidcAdult finds the adult a verified identity signs in, linking it on first use.
func (s *Server) oidcAdult(ctx context.Context, id oidcIdentity) (dbq.Adult, string, error) {
	issuer := s.oidc.issuer
	a, err := s.q.GetAdultByOIDCSubject(ctx, dbq.GetAdultByOIDCSubjectParams{Issuer: issuer, Subject: id.subject})
	switch {
	case err == nil:
		return a, "", s.q.UseOIDCLink(ctx, dbq.UseOIDCLinkParams{HouseholdID: a.HouseholdID, AdultID: a.ID, Issuer: issuer})
	case !errors.Is(err, pgx.ErrNoRows):
		return dbq.Adult{}, "", err
	}
	// No link yet. Unverified first, so an unverified address can't learn who is an adult here.
	if !id.emailVerified || id.email == "" {
		return dbq.Adult{}, "oidc_unverified", nil
	}
	a, err = s.q.GetAdultByEmail(ctx, id.email)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Adult{}, "oidc_no_adult", nil
	}
	if err != nil {
		return dbq.Adult{}, "", err
	}
	n, err := s.q.CreateOIDCLink(ctx, dbq.CreateOIDCLinkParams{
		HouseholdID: a.HouseholdID, AdultID: a.ID, Issuer: issuer, Subject: id.subject, Email: id.email,
	})
	if err != nil {
		return dbq.Adult{}, "", err
	}
	if n == 0 {
		// The adult is linked to another account here, or this account was linked a moment ago
		// by a concurrent sign-in: only the latter, to this same adult, may go on.
		again, err := s.q.GetAdultByOIDCSubject(ctx, dbq.GetAdultByOIDCSubjectParams{Issuer: issuer, Subject: id.subject})
		if err == nil && again.ID == a.ID {
			return a, "", nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return dbq.Adult{}, "", err
		}
		return dbq.Adult{}, "oidc_linked_other", nil
	}
	return a, "", nil
}

// GetOidcLink returns the signed-in adult's link with the configured provider, if any.
func (s *Server) GetOidcLink(ctx context.Context, _ api.GetOidcLinkRequestObject) (api.GetOidcLinkResponseObject, error) {
	if s.oidc == nil {
		return api.GetOidcLink404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errOIDCOff)}, nil
	}
	a := mustActor(ctx)
	l, err := s.q.GetOIDCLinkForAdult(ctx, dbq.GetOIDCLinkForAdultParams{HouseholdID: a.HouseholdID, AdultID: a.AdultID, Issuer: s.oidc.issuer})
	if errors.Is(err, pgx.ErrNoRows) {
		return api.GetOidcLink200JSONResponse{}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetOidcLink200JSONResponse{Link: &api.OidcLink{Email: l.Email, LinkedAt: l.CreatedAt, LastUsedAt: l.LastUsedAt}}, nil
}

// UnlinkOidc removes the signed-in adult's link. Their next sign-in with the provider links
// again by verified email; password and passkeys are untouched.
func (s *Server) UnlinkOidc(ctx context.Context, _ api.UnlinkOidcRequestObject) (api.UnlinkOidcResponseObject, error) {
	if s.oidc == nil {
		return api.UnlinkOidc404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errOIDCOff)}, nil
	}
	a := mustActor(ctx)
	n, err := s.q.DeleteOIDCLink(ctx, dbq.DeleteOIDCLinkParams{HouseholdID: a.HouseholdID, AdultID: a.AdultID, Issuer: s.oidc.issuer})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.UnlinkOidc404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.UnlinkOidc204Response{}, nil
}
