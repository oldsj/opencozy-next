package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/oauth2"

	"github.com/oldsj/opencozy-next/internal/day"
	"github.com/oldsj/opencozy-next/internal/egress"
)

// CodeTokenExpired means Google no longer accepts the account's refresh token: an adult has
// to connect it again.
const CodeTokenExpired = "token_expired"

// GoogleScope is the one Google permission asked for besides the account's identity.
const GoogleScope = "https://www.googleapis.com/auth/calendar.readonly"

// googleEndpoints are Google's. Only tests use others (export_test.go), a stand-in server.
var googleEndpoints = endpoints{ //nolint:gosec // G101: URLs, not credentials
	Auth:     "https://accounts.google.com/o/oauth2/v2/auth",
	Token:    "https://oauth2.googleapis.com/token",
	Revoke:   "https://oauth2.googleapis.com/revoke",
	UserInfo: "https://openidconnect.googleapis.com/v1/userinfo",
	API:      "https://www.googleapis.com/calendar/v3",
}

// googleHosts are the only hosts the "google" egress client may reach. The consent page
// (accounts.google.com) is opened by the adult's browser, never fetched by the server.
var googleHosts = []string{"oauth2.googleapis.com", "openidconnect.googleapis.com", "www.googleapis.com"}

type endpoints struct{ Auth, Token, Revoke, UserInfo, API string }

const (
	googleTimeout  = 30 * time.Second
	googleMaxBytes = 16 << 20
	googlePageSize = 2500
	maxGooglePages = MaxEvents/googlePageSize + 1
	maxListed      = 500 // calendars listed for one account
)

var (
	// ErrReconnect means Google refused the refresh token (revoked or expired).
	ErrReconnect = errors.New("calendar: Google needs the account connected again")
	// ErrScopeMissing means the adult unticked the calendar permission on Google's page.
	ErrScopeMissing = errors.New("calendar: Google did not grant calendar access")
	// errSyncTokenGone is Google's 410: the sync token expired, so do a full fetch.
	errSyncTokenGone = errors.New("calendar: Google sync token expired")
)

// Google talks to Google for one OAuth client: consent, tokens, calendars and events.
type Google struct {
	oauth  oauth2.Config
	client doer
	// with puts the client in a context for x/oauth2 (egress.Context).
	with func(context.Context) context.Context
	ep   endpoints
}

// NewGoogle builds the "google" egress client (Google's token and API hosts, port 443, no
// redirects) for the installation's OAuth client.
func NewGoogle(clientID, clientSecret, redirectURL string) (*Google, error) {
	c, err := egress.New(egress.Options{
		Name: egress.GoogleClient, Hosts: googleHosts, Timeout: googleTimeout, MaxResponseBytes: googleMaxBytes,
	})
	if err != nil {
		return nil, err
	}
	with := func(ctx context.Context) context.Context { return egress.Context(ctx, c) }
	return newGoogle(clientID, clientSecret, redirectURL, c, with, googleEndpoints), nil
}

func newGoogle(clientID, clientSecret, redirectURL string, c doer, with func(context.Context) context.Context, ep endpoints) *Google {
	return &Google{client: c, with: with, ep: ep, oauth: oauth2.Config{
		ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
		Endpoint: oauth2.Endpoint{AuthURL: ep.Auth, TokenURL: ep.Token, AuthStyle: oauth2.AuthStyleInParams},
		Scopes:   []string{GoogleScope, "openid", "email"},
	}}
}

// AuthURL is Google's consent page for one connect: offline access (a refresh token), always
// asking again, PKCE S256 with verifier.
func (g *Google) AuthURL(state, verifier string) string {
	return g.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce,
		oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("include_granted_scopes", "false"))
}

func (g *Google) ctx(ctx context.Context) context.Context { return g.with(ctx) }

// GoogleAccount is a connected account: who it is and its refresh token.
type GoogleAccount struct {
	Subject, Email, Scope string
	RefreshToken          string
}

// Exchange trades the code Google returned for tokens and reads the account's identity.
func (g *Google) Exchange(ctx context.Context, code, verifier string) (GoogleAccount, error) {
	tok, err := g.oauth.Exchange(g.ctx(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return GoogleAccount{}, fmt.Errorf("calendar: Google code exchange: %w", oauthError(err))
	}
	scope, _ := tok.Extra("scope").(string)
	if !contains(strings.Fields(scope), GoogleScope) {
		return GoogleAccount{}, ErrScopeMissing
	}
	if tok.RefreshToken == "" {
		return GoogleAccount{}, errors.New("calendar: Google returned no refresh token")
	}
	var who struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := g.get(ctx, tok.AccessToken, g.ep.UserInfo, &who); err != nil {
		return GoogleAccount{}, err
	}
	if who.Sub == "" || len(who.Sub) > 255 || who.Email == "" || len(who.Email) > 320 {
		return GoogleAccount{}, errors.New("calendar: Google returned no usable identity")
	}
	return GoogleAccount{Subject: who.Sub, Email: who.Email, Scope: clip(scope, 2000), RefreshToken: tok.RefreshToken}, nil
}

// Access is an access token for one run, and the refresh token when Google rotated it.
type Access struct {
	Token   string
	Rotated string
}

// Access refreshes an access token. ErrReconnect means Google refused the refresh token.
func (g *Google) Access(ctx context.Context, refreshToken string) (Access, error) {
	tok, err := g.oauth.TokenSource(g.ctx(ctx), &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return Access{}, oauthError(err)
	}
	a := Access{Token: tok.AccessToken}
	if tok.RefreshToken != "" && tok.RefreshToken != refreshToken {
		a.Rotated = tok.RefreshToken
	}
	return a, nil
}

// oauthError keeps Google's error code and drops the rest (bodies can echo request values).
func oauthError(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		// invalid_grant: revoked or expired. unauthorized_client: issued to another OAuth client
		// (an import whose installation uses a new client); only connecting again helps.
		if re.ErrorCode == "invalid_grant" || re.ErrorCode == "unauthorized_client" {
			return ErrReconnect
		}
		code := 0
		if re.Response != nil {
			code = re.Response.StatusCode
		}
		return fmt.Errorf("google token endpoint: status %d %s", code, re.ErrorCode)
	}
	return transportError(err)
}

// Revoke asks Google to revoke a token. Disconnecting does not depend on it.
func (g *Google) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.ep.Revoke, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return transportError(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest { // 400: already revoked
		return fmt.Errorf("google revoke: status %d", resp.StatusCode)
	}
	return nil
}

// GoogleCalendar is one calendar an account can read.
type GoogleCalendar struct {
	ID, Name string
	Primary  bool
}

// Calendars lists the account's calendars, primary first, then by name.
func (g *Google) Calendars(ctx context.Context, accessToken string) ([]GoogleCalendar, error) {
	var out []GoogleCalendar
	page := ""
	for range 4 {
		q := url.Values{"minAccessRole": {"reader"}, "maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var res struct {
			Items []struct {
				ID              string `json:"id"`
				Summary         string `json:"summary"`
				SummaryOverride string `json:"summaryOverride"`
				Primary         bool   `json:"primary"`
				Deleted         bool   `json:"deleted"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := g.get(ctx, accessToken, g.ep.API+"/users/me/calendarList?"+q.Encode(), &res); err != nil {
			return nil, err
		}
		for _, c := range res.Items {
			name := c.SummaryOverride
			if name == "" {
				name = c.Summary
			}
			if c.Deleted || c.ID == "" || len(c.ID) > 1024 || len(out) >= maxListed {
				continue
			}
			out = append(out, GoogleCalendar{ID: c.ID, Name: clip(name, 200), Primary: c.Primary})
		}
		if page = res.NextPageToken; page == "" {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// gEvent is the part of a Google event that is stored.
type gEvent struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	Summary           string `json:"summary"`
	Location          string `json:"location"`
	Description       string `json:"description"`
	RecurringEventID  string `json:"recurringEventId"`
	Start             gTime  `json:"start"`
	End               gTime  `json:"end"`
	OriginalStartTime gTime  `json:"originalStartTime"`
}

type gTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
}

type eventsPage struct {
	Items         []gEvent `json:"items"`
	NextPageToken string   `json:"nextPageToken"`
	NextSyncToken string   `json:"nextSyncToken"`
}

func (g *Google) eventsURL(calendarID string, q url.Values) string {
	q.Set("singleEvents", "true")
	q.Set("showDeleted", "true")
	q.Set("maxResults", fmt.Sprint(googlePageSize))
	return g.ep.API + "/calendars/" + url.PathEscape(calendarID) + "/events?" + q.Encode()
}

// Changed asks Google whether a calendar changed since syncToken. Any change at all means a
// full fetch, so the stored events are always one consistent window. It returns the next sync
// token when nothing changed.
func (g *Google) Changed(ctx context.Context, accessToken, calendarID, syncToken string) (changed bool, next string, err error) {
	var p eventsPage
	err = g.get(ctx, accessToken, g.eventsURL(calendarID, url.Values{"syncToken": {syncToken}}), &p)
	if err != nil {
		return false, "", err
	}
	if len(p.Items) > 0 || p.NextPageToken != "" || p.NextSyncToken == "" {
		return true, "", nil
	}
	return false, p.NextSyncToken, nil
}

// Events fetches a calendar's events in w (in zone) as a Feed, with the sync token to ask about
// changes next time ("" when the calendar had more events than are stored).
func (g *Google) Events(ctx context.Context, accessToken, calendarID string, zone *time.Location, w Window) (Feed, string, error) {
	var items []gEvent
	q := url.Values{"timeMin": {w.From.Start(zone).Format(time.RFC3339)}, "timeMax": {w.To.Start(zone).Format(time.RFC3339)}}
	for range maxGooglePages {
		var p eventsPage
		if err := g.get(ctx, accessToken, g.eventsURL(calendarID, q), &p); err != nil {
			return Feed{}, "", err
		}
		items = append(items, p.Items...)
		if p.NextPageToken == "" {
			return googleFeed(items, zone), p.NextSyncToken, nil
		}
		q.Set("pageToken", p.NextPageToken)
	}
	feed := googleFeed(items, zone)
	feed.TooMany = true
	return feed, "", nil
}

// googleFeed turns expanded Google events into a Feed keyed like an ICS one: the series is
// the recurring event (or the event itself), the key the instance's original start.
func googleFeed(items []gEvent, zone *time.Location) Feed {
	var feed Feed
	index := map[string]int{}
	seen := map[string]bool{}
	count := 0
	for _, it := range items {
		if it.Status == "cancelled" {
			continue
		}
		uid, key := it.ID, "single"
		if it.RecurringEventID != "" {
			uid = it.RecurringEventID
			k, ok := googleKey(it.OriginalStartTime, zone)
			if !ok {
				feed.Skipped++
				continue
			}
			key = k
		}
		if uid == "" {
			feed.Skipped++
			continue
		}
		uid = storedUID(uid)
		e, ok := googleEvent(it, key, zone)
		if !ok {
			feed.Skipped++
			continue
		}
		if seen[uid+"\x00"+key] {
			continue
		}
		if count >= MaxEvents {
			feed.TooMany = true
			continue
		}
		seen[uid+"\x00"+key] = true
		count++
		i, ok := index[uid]
		if !ok {
			i = len(feed.Series)
			index[uid] = i
			feed.Series = append(feed.Series, Series{UID: uid, Recurring: it.RecurringEventID != ""})
		}
		feed.Series[i].Events = append(feed.Series[i].Events, e)
	}
	return feed
}

// googleKey is an instance's recurrence key from its original start: YYYYMMDD or UTC
// YYYYMMDDTHHMMSSZ, as for ICS.
func googleKey(t gTime, zone *time.Location) (string, bool) {
	if t.Date != "" {
		d, err := day.ParseDate(t.Date)
		if err != nil {
			return "", false
		}
		return keyOf(d.Start(zone), true), true
	}
	at, err := time.Parse(time.RFC3339, t.DateTime)
	if err != nil {
		return "", false
	}
	return keyOf(at, false), true
}

func googleEvent(it gEvent, key string, zone *time.Location) (Event, bool) {
	e := Event{Key: key, Title: clip(it.Summary, maxTitle), Location: clip(it.Location, maxLocation),
		Description: clip(it.Description, maxDesc), Tentative: it.Status == "tentative"}
	if it.Start.Date != "" {
		sd, err := day.ParseDate(it.Start.Date)
		if err != nil {
			return Event{}, false
		}
		ed, err := day.ParseDate(it.End.Date)
		if err != nil || ed.Compare(sd) <= 0 {
			ed = sd.AddDays(1)
		}
		e.AllDay, e.StartDate, e.EndDate = true, sd, ed
		return e, true
	}
	start, err := time.Parse(time.RFC3339, it.Start.DateTime)
	if err != nil {
		return Event{}, false
	}
	end, err := time.Parse(time.RFC3339, it.End.DateTime)
	if err != nil || end.Before(start) {
		end = start
	}
	e.Start, e.End = start.In(zone), end.In(zone)
	return e, true
}

// clip trims s, drops control characters but newlines, and cuts it to max characters.
func clip(s string, maxChars int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r >= 0x20 && r != 0x7f {
			return r
		}
		return -1
	}, s))
	if utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:maxChars]))
}

// get GETs a Google API URL with the access token and decodes the JSON. Errors are *SyncError
// (or errSyncTokenGone) and never contain the URL, which can carry a calendar id or token.
func (g *Google) get(ctx context.Context, accessToken, rawURL string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fail(CodeInvalidCalendar, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return transportError(err)
	}
	switch status := resp.StatusCode; {
	case status == http.StatusOK:
	case status == http.StatusGone:
		return errSyncTokenGone
	case status == http.StatusUnauthorized:
		return fail(CodeTokenExpired, fmt.Errorf("status %d", status))
	case status == http.StatusTooManyRequests, status == http.StatusForbidden && rateLimited(body):
		return fail(CodeRateLimited, fmt.Errorf("status %d", status))
	case status == http.StatusForbidden:
		return fail(CodeAccessDenied, fmt.Errorf("status %d", status))
	case status == http.StatusNotFound:
		return fail(CodeNotFound, fmt.Errorf("status %d", status))
	case status >= 500:
		return fail(CodeTimeout, fmt.Errorf("status %d", status))
	default:
		return fail(CodeInvalidCalendar, fmt.Errorf("status %d", status))
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fail(CodeInvalidCalendar, errors.New("google: unreadable response"))
	}
	return nil
}

// rateLimited reports Google's 403 rate-limit reasons.
func rateLimited(body []byte) bool {
	var e struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return false
	}
	for _, r := range e.Error.Errors {
		if r.Reason == "rateLimitExceeded" || r.Reason == "userRateLimitExceeded" {
			return true
		}
	}
	return false
}
