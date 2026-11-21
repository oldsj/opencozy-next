package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
	"github.com/oldsj/opencozy-next/internal/egress"
)

// APNsClient is the egress client's name.
const APNsClient = "apns"

// Origins are APNs' two environments. An app built for development has a development token,
// which only the sandbox accepts.
var Origins = map[string]string{
	"development": "https://api.sandbox.push.apple.com",
	"production":  "https://api.push.apple.com",
}

// APNsConfig is token-based APNs authentication (a .p8 key).
type APNsConfig struct {
	TeamID string // Apple developer team ID
	Topic  string // the app's bundle ID
	KeyID  string // the .p8 key's ID
	Key    string // the .p8 key, PEM (PKCS #8, P-256)
}

// doer is an HTTP client: in production always the egress one from NewAPNs.
type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// APNs sends rows to Apple. It implements Sender.
type APNs struct {
	cfg     APNsConfig
	key     *ecdsa.PrivateKey
	client  doer
	origins map[string]string
	log     *slog.Logger
	now     func() time.Time

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

var appleID = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// ParseKey reads an APNs .p8 key: a PKCS #8 PEM block holding a P-256 key.
func ParseKey(p string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(p))
	if block == nil {
		return nil, errors.New("not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, errors.New("not a P-256 key")
	}
	return ec, nil
}

// NewAPNs makes the sender. Requests go through internal/egress with HTTP/2, which APNs needs.
func NewAPNs(cfg APNsConfig, log *slog.Logger) (*APNs, error) {
	c, err := egress.New(egress.Options{Name: APNsClient, HTTP2: true, Timeout: 10 * time.Second, MaxResponseBytes: 4096})
	if err != nil {
		return nil, err
	}
	return newAPNs(cfg, c, Origins, log)
}

func newAPNs(cfg APNsConfig, c doer, origins map[string]string, log *slog.Logger) (*APNs, error) {
	if !appleID.MatchString(cfg.TeamID) || !appleID.MatchString(cfg.KeyID) || cfg.Topic == "" {
		return nil, errors.New("apns: team ID, key ID and topic are required")
	}
	key, err := ParseKey(cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("apns: key: %w", err)
	}
	return &APNs{cfg: cfg, key: key, client: c, origins: origins, log: log, now: time.Now}, nil
}

// providerToken is the ES256 JWT APNs authenticates with. Apple accepts one 20 to 60 minutes
// old and refuses refreshing it more often than every 20, so it is reused for 50 minutes.
func (a *APNs) providerToken(refresh bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if !refresh && a.token != "" && now.Sub(a.tokenAt) < 50*time.Minute {
		return a.token, nil
	}
	part := func(v any) string {
		j, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(j)
	}
	input := part(map[string]string{"alg": "ES256", "kid": a.cfg.KeyID}) + "." + part(map[string]any{"iss": a.cfg.TeamID, "iat": now.Unix()})
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	a.token, a.tokenAt = input+"."+base64.RawURLEncoding.EncodeToString(sig), now
	return a.token, nil
}

// target is one phone or device.
type target struct {
	id         uuid.UUID
	token, env string
	clear      func(ctx context.Context) error
	skipAlerts bool
}

// errTransient is a delivery worth retrying: a network failure, 429 or 5xx.
var errTransient = errors.New("apns: try again")

// invalidToken are the reasons that mean the token will never work again.
var invalidToken = []string{"BadDeviceToken", "Unregistered", "DeviceTokenNotForTopic"}

// Send delivers one row to every phone of its adult, or every device of its child. A token
// APNs calls invalid is cleared. If any phone failed in a way worth retrying, the whole row is
// retried; phones that got it may get it again, and the collapse ID replaces the copy.
func (a *APNs) Send(ctx context.Context, q *dbq.Queries, row dbq.PushOutbox) error {
	var n Notification
	if err := json.Unmarshal(row.Payload, &n); err != nil {
		return fmt.Errorf("%w: unreadable row: %w", ErrRefused, err)
	}
	var targets []target
	pending := 0
	switch {
	case row.AdultID != nil:
		rows, err := q.AdultPushTargets(ctx, dbq.AdultPushTargetsParams{HouseholdID: row.HouseholdID, AdultID: *row.AdultID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			targets = append(targets, target{id: r.ID, token: r.PushToken, env: r.PushEnvironment, skipAlerts: r.Muted,
				clear: func(ctx context.Context) error {
					tok := r.PushToken
					return q.ClearPhonePushToken(ctx, dbq.ClearPhonePushTokenParams{HouseholdID: row.HouseholdID, ID: r.ID, Token: &tok})
				}})
		}
		if n.Badge && len(targets) > 0 {
			c, err := q.CountPendingInbox(ctx, row.HouseholdID)
			if err != nil {
				return err
			}
			pending = int(c)
		}
	case row.ChildID != nil:
		rows, err := q.ChildPushTargets(ctx, dbq.ChildPushTargetsParams{HouseholdID: row.HouseholdID, ChildID: row.ChildID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			targets = append(targets, target{id: r.ID, token: r.PushToken, env: r.PushEnvironment,
				clear: func(ctx context.Context) error {
					tok := r.PushToken
					return q.ClearDevicePushToken(ctx, dbq.ClearDevicePushTokenParams{HouseholdID: row.HouseholdID, ID: r.ID, Token: &tok})
				}})
		}
	}
	body, err := json.Marshal(n.APNs(pending))
	if err != nil {
		return err
	}
	var retry error
	for _, t := range targets {
		if t.skipAlerts && n.Alert() {
			continue // muted (old #87): silent updates only
		}
		err := a.post(ctx, t, n, body)
		switch {
		case err == nil:
		case errors.Is(err, errInvalidToken):
			a.log.InfoContext(ctx, "push token no longer valid; cleared", "id", t.id)
			if err := t.clear(ctx); err != nil {
				return err
			}
		case errors.Is(err, errTransient):
			retry = err
		default:
			a.log.WarnContext(ctx, "push refused", "id", t.id, "err", err)
		}
	}
	return retry
}

var errInvalidToken = errors.New("apns: invalid token")

func (a *APNs) post(ctx context.Context, t target, n Notification, body []byte) error {
	origin, ok := a.origins[t.env]
	if !ok {
		return fmt.Errorf("apns: unknown environment %q", t.env)
	}
	for attempt := 0; ; attempt++ {
		tok, err := a.providerToken(attempt > 0)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/3/device/"+t.token, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("authorization", "bearer "+tok)
		req.Header.Set("apns-topic", a.cfg.Topic)
		req.Header.Set("apns-collapse-id", n.Collapse)
		req.Header.Set("content-type", "application/json")
		if n.Alert() {
			req.Header.Set("apns-push-type", "alert")
			req.Header.Set("apns-priority", "10")
		} else {
			req.Header.Set("apns-push-type", "background")
			req.Header.Set("apns-priority", "5")
		}
		resp, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %w", errTransient, err)
		}
		var answer struct {
			Reason string `json:"reason"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		_ = json.Unmarshal(raw, &answer)
		switch {
		case resp.StatusCode == http.StatusOK:
			return nil
		case resp.StatusCode == http.StatusGone,
			resp.StatusCode == http.StatusBadRequest && slices.Contains(invalidToken, answer.Reason):
			return errInvalidToken
		case resp.StatusCode == http.StatusForbidden && answer.Reason == "ExpiredProviderToken" && attempt == 0:
			continue // a fresh provider token, once
		case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
			return fmt.Errorf("%w: %d %s", errTransient, resp.StatusCode, safeReason(answer.Reason))
		default:
			return fmt.Errorf("apns: %d %s", resp.StatusCode, safeReason(answer.Reason))
		}
	}
}

var reasonText = regexp.MustCompile(`^[A-Za-z]{1,64}$`)

// safeReason keeps APNs' reason only when it is a plain word, for the log.
func safeReason(r string) string {
	if reasonText.MatchString(r) {
		return r
	}
	return ""
}
