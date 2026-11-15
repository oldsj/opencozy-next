package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"rsc.io/qr"

	"github.com/oldsj/opencozy-next/internal/actor"
	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/auth"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// DefaultLockMessage is what a locked device shows when the household set none (the iOS app's
// own words, so the shield and Manage → Devices agree).
const DefaultLockMessage = "Earn time on the chore board, then spend your points in OpenCozy."

// devicePairRatePerMinute is per client address, for redeeming offers (the old app's 20).
const devicePairRatePerMinute = 20

func newDeviceCode() (string, error) {
	out := make([]byte, 8)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = codeAlphabet[n.Int64()]
	}
	return string(out[:4]) + "-" + string(out[4:]), nil
}

func deviceCodeHash(code string) []byte {
	return auth.HashToken("device-pairing:" + normalizeCode(code))
}

// qrModules encodes text as a QR code, row by row ("1" dark), for the design system's QrCode.
func qrModules(text string) ([]string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	rows := make([]string, c.Size)
	var b strings.Builder
	for y := range c.Size {
		b.Reset()
		for x := range c.Size {
			if c.Black(x, y) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows[y] = b.String()
	}
	return rows, nil
}

type originKey struct{}

// requestOrigin is the origin the client reached the server at, for the pairing QR code: https,
// except plain http to a loopback address (development and the e2e tests).
func requestOrigin(r *http.Request) string {
	host := r.Host
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	scheme := "https"
	if r.TLS == nil && (h == "localhost" || net.ParseIP(h).IsLoopback()) {
		scheme = "http"
	}
	return scheme + "://" + host
}

func originFrom(ctx context.Context) string {
	o, _ := ctx.Value(originKey{}).(string)
	return o
}

func lockMessageOf(m *string) string {
	if m == nil {
		return DefaultLockMessage
	}
	return *m
}

func toDevice(id uuid.UUID, name, platform string, paired time.Time, seen *time.Time, childID *uuid.UUID, childName, childColor *string) api.Device {
	d := api.Device{Id: id, Name: name, Platform: api.Platform(platform), PairedAt: paired, LastSeenAt: seen}
	if childID != nil && childName != nil && childColor != nil {
		d.Child = &api.DeviceChild{Id: *childID, Name: *childName, Color: api.ProfileColor(*childColor)}
	}
	return d
}

func (s *Server) deviceSelf(ctx context.Context, q *dbq.Queries, household, id uuid.UUID) (api.DeviceSelf, error) {
	v, err := q.GetDeviceView(ctx, dbq.GetDeviceViewParams{HouseholdID: household, ID: id})
	if err != nil {
		return api.DeviceSelf{}, err
	}
	return api.DeviceSelf{
		Device:     toDevice(v.ID, v.Name, v.Platform, v.PairedAt, v.LastSeenAt, v.ChildID, v.ChildName, v.ChildColor),
		Household:  api.DeviceHousehold{Id: household, Name: v.HouseholdName, TimeZone: v.TimeZone, LockMessage: lockMessageOf(v.LockMessage)},
		ServerTime: time.Now().UTC(),
	}, nil
}

// liveChild reports whether the child is in the household and not archived.
func liveChild(ctx context.Context, q *dbq.Queries, household, child uuid.UUID) (bool, error) {
	c, err := q.GetChild(ctx, dbq.GetChildParams{HouseholdID: household, ID: child})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return c.ArchivedAt == nil, nil
}

var errNoSuchChild = apiError("no_such_child", "Choose a child of this household.")

// CreateDevicePairing offers a pairing for one child's device.
func (s *Server) CreateDevicePairing(ctx context.Context, req api.CreateDevicePairingRequestObject) (api.CreateDevicePairingResponseObject, error) {
	cmd := mustCmd(ctx)
	h := mustActor(ctx).HouseholdID
	ok, err := liveChild(ctx, cmd.Q, h, req.Body.ChildId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return api.CreateDevicePairing404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNoSuchChild)}, nil
	}
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	code, err := newDeviceCode()
	if err != nil {
		return nil, err
	}
	row, err := cmd.Q.CreateDevicePairing(ctx, dbq.CreateDevicePairingParams{HouseholdID: h, ChildID: req.Body.ChildId, TokenHash: tokenHash, CodeHash: deviceCodeHash(code)})
	if err != nil {
		return nil, err
	}
	payload := "opencozy-pair:v1?origin=" + url.QueryEscape(originFrom(ctx)) + "&token=" + token
	modules, err := qrModules(payload)
	if err != nil {
		return nil, err
	}
	return api.CreateDevicePairing201JSONResponse{Id: row.ID, Code: code, Token: token, QrPayload: payload, QrModules: modules, ExpiresAt: row.ExpiresAt}, nil
}

var errPairingUnavailable = apiError("pairing_unavailable", "This pairing isn't available. Ask an adult for a new code.")

// PairDevice redeems an offer with the device's new key.
func (s *Server) PairDevice(ctx context.Context, req api.PairDeviceRequestObject) (api.PairDeviceResponseObject, error) {
	b := req.Body
	unavailable := api.PairDevice409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(errPairingUnavailable)}
	if (b.Token == nil) == (b.Code == nil) {
		return api.PairDevice400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError("bad_request", "Send a pairing token or a code."))}, nil
	}
	raw, err := base64.StdEncoding.DecodeString(b.PublicKey)
	if err != nil || len(raw) != 65 {
		return api.PairDevice400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError("bad_request", "The public key must be an uncompressed P-256 point."))}, nil
	}
	pub, err := actor.ParsePublicKey(raw)
	if err != nil {
		return api.PairDevice400JSONResponse{BadRequestJSONResponse: api.BadRequestJSONResponse(apiError("bad_request", "The public key must be an uncompressed P-256 point."))}, nil
	}
	params := dbq.LockDevicePairingParams{}
	sent := ""
	if b.Token != nil {
		sent, params.TokenHash = *b.Token, auth.HashToken(*b.Token)
	} else {
		sent, params.CodeHash = *b.Code, deviceCodeHash(*b.Code)
	}
	if !actor.VerifyP1363(pub, actor.PairProof(sent, b.PublicKey), b.Proof) {
		return unavailable, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)
	p, err := q.LockDevicePairing(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return unavailable, nil
	} else if err != nil {
		return nil, err
	}
	id, err := q.CreateDevice(ctx, dbq.CreateDeviceParams{
		HouseholdID: p.HouseholdID, ChildID: &p.ChildID, Name: strings.TrimSpace(b.Name), Platform: string(b.Platform), PublicKey: raw,
	})
	if errors.Is(err, pgx.ErrNoRows) { // this key is already paired
		return unavailable, nil
	} else if err != nil {
		return nil, err
	}
	if err := q.DeleteDevicePairing(ctx, p.ID); err != nil {
		return nil, err
	}
	self, err := s.deviceSelf(ctx, q, p.HouseholdID, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return api.PairDevice201JSONResponse(self), nil
}

// GetDevice is the device's own record; every sync starts here.
func (s *Server) GetDevice(ctx context.Context, _ api.GetDeviceRequestObject) (api.GetDeviceResponseObject, error) {
	a := mustActor(ctx)
	self, err := s.deviceSelf(ctx, s.q, a.HouseholdID, a.CredentialID)
	if err != nil {
		return nil, err
	}
	return api.GetDevice200JSONResponse(self), nil
}

// CreateDeviceWebSession makes the one-time link the app opens in its web view.
func (s *Server) CreateDeviceWebSession(ctx context.Context, _ api.CreateDeviceWebSessionRequestObject) (api.CreateDeviceWebSessionResponseObject, error) {
	token, hash, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	expires, err := s.q.CreateDeviceWebLink(ctx, dbq.CreateDeviceWebLinkParams{TokenHash: hash, DeviceID: mustActor(ctx).CredentialID})
	if err != nil {
		return nil, err
	}
	return api.CreateDeviceWebSession201JSONResponse{Url: "/device/session?token=" + token, ExpiresAt: expires}, nil
}

// GetDeviceWebSession is the kid view's own session.
func (s *Server) GetDeviceWebSession(ctx context.Context, _ api.GetDeviceWebSessionRequestObject) (api.GetDeviceWebSessionResponseObject, error) {
	a := mustActor(ctx)
	sess, err := s.q.GetDeviceWebSession(ctx, a.TokenHash)
	if err != nil {
		return nil, err
	}
	c, err := s.q.GetChild(ctx, dbq.GetChildParams{HouseholdID: a.HouseholdID, ID: sess.ChildID})
	if err != nil {
		return nil, err
	}
	self, err := s.deviceSelf(ctx, s.q, a.HouseholdID, sess.DeviceID)
	if err != nil {
		return nil, err
	}
	return api.GetDeviceWebSession200JSONResponse{Child: toChild(c), Household: self.Household, ExpiresAt: sess.ExpiresAt}, nil
}

// deviceSessionPage exchanges a one-time link for the device web session (GET /device/session,
// a page route outside /api): it sets __Host-device, signs out any adult or display session in
// this cookie jar, and goes to /kid. A used or expired link goes to /device/expired.
func (s *Server) deviceSessionPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	token := r.URL.Query().Get("token")
	expired := func() { http.Redirect(w, r, "/device/expired", http.StatusSeeOther) }
	if !auth.WellFormedCookie(token) {
		expired()
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "device session", "err", err)
		http.Error(w, "Try again shortly.", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)
	deviceID, err := q.UseDeviceWebLink(ctx, auth.HashToken(token))
	if err == nil {
		_, err = q.GetSigningDevice(ctx, deviceID) // revoked, or its child archived, since the link
	}
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Commit(ctx)
		expired()
		return
	}
	sessToken, sessHash, err := auth.NewSessionToken()
	var expires time.Time
	if err == nil {
		expires, err = q.CreateDeviceWebSession(ctx, dbq.CreateDeviceWebSessionParams{TokenHash: sessHash, DeviceID: deviceID})
	}
	if err == nil {
		if c, cerr := r.Cookie(actor.AdultCookie); cerr == nil && auth.WellFormedCookie(c.Value) {
			err = q.DeleteAdultSession(ctx, auth.HashToken(c.Value))
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		s.log.ErrorContext(ctx, "device session", "err", err)
		http.Error(w, "Try again shortly.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Add("Set-Cookie", displayCookie(actor.DeviceWebCookie, sessToken, int(time.Until(expires)/time.Second)))
	w.Header().Add("Set-Cookie", displayCookie(actor.AdultCookie, "", -1))
	w.Header().Add("Set-Cookie", displayCookie(actor.DisplayCookie, "", -1))
	http.Redirect(w, r, "/kid", http.StatusSeeOther)
}

// ListDevices lists the household's devices and its lock message.
func (s *Server) ListDevices(ctx context.Context, _ api.ListDevicesRequestObject) (api.ListDevicesResponseObject, error) {
	h := mustActor(ctx).HouseholdID
	rows, err := s.q.ListDevices(ctx, h)
	if err != nil {
		return nil, err
	}
	msg, err := s.q.GetLockMessage(ctx, h)
	if err != nil {
		return nil, err
	}
	out := api.ListDevices200JSONResponse{Devices: make([]api.Device, len(rows)), LockMessage: msg, DefaultLockMessage: DefaultLockMessage}
	for i, d := range rows {
		out.Devices[i] = toDevice(d.ID, d.Name, d.Platform, d.PairedAt, d.LastSeenAt, d.ChildID, d.ChildName, d.ChildColor)
	}
	return out, nil
}

// UpdateDevice renames a device or gives it to another child. A new child ends the device's
// web sessions, so the kid view can't keep showing the old child.
func (s *Server) UpdateDevice(ctx context.Context, req api.UpdateDeviceRequestObject) (api.UpdateDeviceResponseObject, error) {
	cmd := mustCmd(ctx)
	h := mustActor(ctx).HouseholdID
	p := dbq.UpdateDeviceParams{HouseholdID: h, ID: req.DeviceId, ChildID: req.Body.ChildId}
	if req.Body.Name != nil {
		name := strings.TrimSpace(*req.Body.Name)
		if name == "" {
			return api.UpdateDevice422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("name_required", "Give the device a name."))}, nil
		}
		p.Name = &name
	}
	if p.ChildID != nil {
		ok, err := liveChild(ctx, cmd.Q, h, *p.ChildID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return api.UpdateDevice422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(errNoSuchChild)}, nil
		}
	}
	n, err := cmd.Q.UpdateDevice(ctx, p)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.UpdateDevice404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	if p.ChildID != nil {
		if err := cmd.Q.DeleteDeviceWebSessions(ctx, req.DeviceId); err != nil {
			return nil, err
		}
	}
	self, err := s.deviceSelf(ctx, cmd.Q, h, req.DeviceId)
	if err != nil {
		return nil, err
	}
	return api.UpdateDevice200JSONResponse(self.Device), nil
}

// RevokeDevice unpairs a device; its next request gets 401.
func (s *Server) RevokeDevice(ctx context.Context, req api.RevokeDeviceRequestObject) (api.RevokeDeviceResponseObject, error) {
	cmd := mustCmd(ctx)
	now, err := cmd.Now(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cmd.Q.RevokeDevice(ctx, dbq.RevokeDeviceParams{HouseholdID: mustActor(ctx).HouseholdID, ID: req.DeviceId, Now: &now})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.RevokeDevice404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	if err := cmd.Q.DeleteDeviceWebSessions(ctx, req.DeviceId); err != nil {
		return nil, err
	}
	return api.RevokeDevice204Response{}, nil
}

// SetLockMessage sets (or, with null, clears) the household's lock message.
func (s *Server) SetLockMessage(ctx context.Context, req api.SetLockMessageRequestObject) (api.SetLockMessageResponseObject, error) {
	cmd := mustCmd(ctx)
	var msg *string
	if req.Body.LockMessage != nil {
		m := strings.TrimSpace(*req.Body.LockMessage)
		if m == "" {
			return api.SetLockMessage422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(apiError("message_required", "Write a message, or use the default."))}, nil
		}
		msg = &m
	}
	if err := cmd.Q.SetLockMessage(ctx, dbq.SetLockMessageParams{ID: mustActor(ctx).HouseholdID, LockMessage: msg}); err != nil {
		return nil, err
	}
	return api.SetLockMessage204Response{}, nil
}
