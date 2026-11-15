package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/oldsj/opencozy-next/internal/api"
	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Passkeys (design §6), with go-webauthn. Every passkey is discoverable and verifies the user,
// since it is the whole sign-in. A ceremony's challenge lives in webauthn_challenge for 5
// minutes and is deleted by the call that finishes it, whatever the outcome.

const maxPasskeys = 20

const (
	purposeRegister = "register"
	purposeSignIn   = "sign_in"
	purposeConfirm  = "confirm"
)

var (
	errPasskeysOff    = apiError("passkeys_off", "Passkeys are not set up on this server.")
	errPasskey        = apiError("passkey_not_accepted", "That passkey was not accepted.")
	errChallenge      = apiError("challenge_expired", "That took too long. Try again.")
	errPasskeyInvalid = apiError("passkey_invalid", "That passkey could not be added. Try again.")
)

func newWebAuthn(rpID string, origins []string) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "OpenCozy",
		RPOrigins:     origins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
	})
}

// passkeyUser is an adult as go-webauthn sees them.
type passkeyUser struct {
	adult dbq.Adult
	creds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.adult.WebauthnUserHandle }
func (u passkeyUser) WebAuthnName() string                       { return u.adult.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.adult.Name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// toWebAuthnCredential converts a stored passkey. asserted is the backup-eligible flag of the
// assertion being checked, adopted when the stored one is unknown (an imported passkey before its
// first sign-in); the signature check then vouches for it, and usePasskey records it.
func toWebAuthnCredential(p dbq.Passkey, asserted *bool) webauthn.Credential {
	eligible := p.BackupEligible.Bool
	if !p.BackupEligible.Valid && asserted != nil {
		eligible = *asserted
	}
	transports := make([]protocol.AuthenticatorTransport, len(p.Transports))
	for i, t := range p.Transports {
		transports[i] = protocol.AuthenticatorTransport(t)
	}
	return webauthn.Credential{
		ID:              p.CredentialID,
		PublicKey:       p.PublicKey,
		AttestationType: p.AttestationType,
		Transport:       transports,
		Flags:           webauthn.CredentialFlags{UserPresent: true, UserVerified: true, BackupEligible: eligible, BackupState: p.BackedUp},
		Authenticator:   webauthn.Authenticator{AAGUID: p.Aaguid, SignCount: uint32(p.SignCount)}, //nolint:gosec // the column is 0..2^32-1
	}
}

func (s *Server) passkeyUser(ctx context.Context, a dbq.Adult, asserted *bool) (passkeyUser, []dbq.Passkey, error) {
	rows, err := s.q.ListPasskeys(ctx, dbq.ListPasskeysParams{HouseholdID: a.HouseholdID, AdultID: a.ID})
	if err != nil {
		return passkeyUser{}, nil, err
	}
	u := passkeyUser{adult: a}
	for _, p := range rows {
		u.creds = append(u.creds, toWebAuthnCredential(p, asserted))
	}
	return u, rows, nil
}

// startCeremony stores the session and returns the options the browser needs.
func (s *Server) startCeremony(ctx context.Context, purpose string, adultID *uuid.UUID, options any, session *webauthn.SessionData) (api.PasskeyOptions, error) {
	raw, err := json.Marshal(session)
	if err != nil {
		return api.PasskeyOptions{}, err
	}
	id, err := s.q.CreateChallenge(ctx, dbq.CreateChallengeParams{Purpose: purpose, AdultID: adultID, Session: raw})
	if err != nil {
		return api.PasskeyOptions{}, err
	}
	j, err := json.Marshal(options)
	if err != nil {
		return api.PasskeyOptions{}, err
	}
	var m map[string]any
	if err := json.Unmarshal(j, &m); err != nil {
		return api.PasskeyOptions{}, err
	}
	return api.PasskeyOptions{ChallengeId: id, Options: m}, nil
}

// takeCeremony deletes the challenge and returns its session, if it is live, for this purpose
// and (for an adult's own ceremonies) this adult.
func (s *Server) takeCeremony(ctx context.Context, id uuid.UUID, purpose string, adultID *uuid.UUID) (webauthn.SessionData, bool, error) {
	row, err := s.q.TakeChallenge(ctx, dbq.TakeChallengeParams{ID: id, Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return webauthn.SessionData{}, false, nil
	}
	if err != nil {
		return webauthn.SessionData{}, false, err
	}
	if (adultID == nil) != (row.AdultID == nil) || (adultID != nil && *adultID != *row.AdultID) {
		return webauthn.SessionData{}, false, nil
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return webauthn.SessionData{}, false, fmt.Errorf("challenge session: %w", err)
	}
	return session, true, nil
}

// eligibleFlag is the assertion's backup-eligible flag.
func eligibleFlag(parsed *protocol.ParsedCredentialAssertionData) *bool {
	be := parsed.Response.AuthenticatorData.Flags.HasBackupEligible()
	return &be
}

func parseAssertion(credential map[string]any) (*protocol.ParsedCredentialAssertionData, error) {
	j, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	return protocol.ParseCredentialRequestResponseBody(bytes.NewReader(j))
}

// usePasskey records a successful assertion. A sign counter that went backwards means the
// credential may have been cloned, so it is refused.
func (s *Server) usePasskey(ctx context.Context, c *webauthn.Credential) (bool, error) {
	if c.Authenticator.CloneWarning {
		s.log.WarnContext(ctx, "passkey sign counter went backwards; refused")
		return false, nil
	}
	return true, s.q.UsePasskey(ctx, dbq.UsePasskeyParams{
		CredentialID: c.ID, SignCount: int64(c.Authenticator.SignCount), BackedUp: c.Flags.BackupState, BackupEligible: c.Flags.BackupEligible,
	})
}

// PasskeySignInOptions starts a sign-in with any of the household's passkeys.
func (s *Server) PasskeySignInOptions(ctx context.Context, _ api.PasskeySignInOptionsRequestObject) (api.PasskeySignInOptionsResponseObject, error) {
	if s.wa == nil {
		return api.PasskeySignInOptions404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errPasskeysOff)}, nil
	}
	assertion, session, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	opts, err := s.startCeremony(ctx, purposeSignIn, nil, assertion.Response, session)
	if err != nil {
		return nil, err
	}
	return api.PasskeySignInOptions200JSONResponse(opts), nil
}

// PasskeySignIn signs in with a passkey: the same session as a password sign-in.
func (s *Server) PasskeySignIn(ctx context.Context, req api.PasskeySignInRequestObject) (api.PasskeySignInResponseObject, error) {
	refused := api.PasskeySignIn401JSONResponse{UnauthorizedJSONResponse: api.UnauthorizedJSONResponse(errPasskey)}
	if s.wa == nil {
		return refused, nil
	}
	session, ok, err := s.takeCeremony(ctx, req.Body.ChallengeId, purposeSignIn, nil)
	if err != nil || !ok {
		return refused, err
	}
	parsed, err := parseAssertion(req.Body.Credential)
	if err != nil {
		s.log.InfoContext(ctx, "passkey sign-in: unreadable response", "err", err)
		return refused, nil
	}
	var adult dbq.Adult
	user, cred, err := s.wa.ValidatePasskeyLogin(func(_, userHandle []byte) (webauthn.User, error) {
		a, err := s.q.GetAdultByUserHandle(ctx, userHandle)
		if err != nil {
			return nil, err
		}
		adult = a
		u, _, err := s.passkeyUser(ctx, a, eligibleFlag(parsed))
		return u, err
	}, session, parsed)
	if err != nil || user == nil {
		s.log.InfoContext(ctx, "passkey sign-in refused", "err", err)
		return refused, nil
	}
	if ok, err := s.usePasskey(ctx, cred); err != nil || !ok {
		return refused, err
	}
	me, err := s.me(ctx, adult)
	if err != nil {
		return nil, err
	}
	cookie, err := s.startSession(ctx, adult.ID)
	if err != nil {
		return nil, err
	}
	return api.PasskeySignIn200JSONResponse{Body: me, Headers: api.PasskeySignIn200ResponseHeaders{SetCookie: &cookie}}, nil
}

// PasskeyConfirmOptions starts confirming the current sign-in with one of the adult's passkeys.
func (s *Server) PasskeyConfirmOptions(ctx context.Context, _ api.PasskeyConfirmOptionsRequestObject) (api.PasskeyConfirmOptionsResponseObject, error) {
	if s.wa == nil {
		return api.PasskeyConfirmOptions404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errPasskeysOff)}, nil
	}
	a := mustActor(ctx)
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	user, _, err := s.passkeyUser(ctx, adult, nil)
	if err != nil {
		return nil, err
	}
	if len(user.creds) == 0 {
		return api.PasskeyConfirmOptions404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(apiError("no_passkeys", "You have no passkeys yet."))}, nil
	}
	assertion, session, err := s.wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, err
	}
	opts, err := s.startCeremony(ctx, purposeConfirm, &adult.ID, assertion.Response, session)
	if err != nil {
		return nil, err
	}
	return api.PasskeyConfirmOptions200JSONResponse(opts), nil
}

// PasskeyConfirm counts as a fresh sign-in for operations that need one, like ConfirmSignIn.
func (s *Server) PasskeyConfirm(ctx context.Context, req api.PasskeyConfirmRequestObject) (api.PasskeyConfirmResponseObject, error) {
	refused := api.PasskeyConfirm403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errPasskey)}
	if s.wa == nil {
		return refused, nil
	}
	a := mustActor(ctx)
	session, ok, err := s.takeCeremony(ctx, req.Body.ChallengeId, purposeConfirm, &a.AdultID)
	if err != nil || !ok {
		return refused, err
	}
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	parsed, err := parseAssertion(req.Body.Credential)
	if err != nil {
		return refused, nil //nolint:nilerr // an unreadable response is a refusal
	}
	user, _, err := s.passkeyUser(ctx, adult, eligibleFlag(parsed))
	if err != nil {
		return nil, err
	}
	cred, err := s.wa.ValidateLogin(user, session, parsed)
	if err != nil {
		s.log.InfoContext(ctx, "passkey confirm refused", "err", err)
		return refused, nil
	}
	if ok, err := s.usePasskey(ctx, cred); err != nil || !ok {
		return refused, err
	}
	if err := s.q.ConfirmAdultSession(ctx, a.TokenHash); err != nil {
		return nil, err
	}
	return api.PasskeyConfirm204Response{}, nil
}

func toPasskey(p dbq.Passkey) api.Passkey {
	out := api.Passkey{Id: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, BackedUp: p.BackedUp}
	if p.LastUsedAt != nil {
		out.LastUsedAt = p.LastUsedAt
	}
	return out
}

// ListPasskeys lists the adult's own passkeys.
func (s *Server) ListPasskeys(ctx context.Context, _ api.ListPasskeysRequestObject) (api.ListPasskeysResponseObject, error) {
	a := mustActor(ctx)
	rows, err := s.q.ListPasskeys(ctx, dbq.ListPasskeysParams{HouseholdID: a.HouseholdID, AdultID: a.AdultID})
	if err != nil {
		return nil, err
	}
	out := api.Passkeys{Passkeys: make([]api.Passkey, 0, len(rows))}
	for _, p := range rows {
		out.Passkeys = append(out.Passkeys, toPasskey(p))
	}
	return api.ListPasskeys200JSONResponse(out), nil
}

// PasskeyRegistrationOptions starts adding a passkey for the signed-in adult.
func (s *Server) PasskeyRegistrationOptions(ctx context.Context, _ api.PasskeyRegistrationOptionsRequestObject) (api.PasskeyRegistrationOptionsResponseObject, error) {
	if s.wa == nil {
		return api.PasskeyRegistrationOptions404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errPasskeysOff)}, nil
	}
	a := mustActor(ctx)
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	user, _, err := s.passkeyUser(ctx, adult, nil)
	if err != nil {
		return nil, err
	}
	creation, session, err := s.wa.BeginRegistration(user, webauthn.WithExclusions(webauthn.Credentials(user.creds).CredentialDescriptors()))
	if err != nil {
		return nil, err
	}
	opts, err := s.startCeremony(ctx, purposeRegister, &adult.ID, creation.Response, session)
	if err != nil {
		return nil, err
	}
	return api.PasskeyRegistrationOptions200JSONResponse(opts), nil
}

// AddPasskey finishes adding a passkey.
func (s *Server) AddPasskey(ctx context.Context, req api.AddPasskeyRequestObject) (api.AddPasskeyResponseObject, error) {
	invalid := func(e api.Error) api.AddPasskeyResponseObject {
		return api.AddPasskey422JSONResponse{UnprocessableEntityJSONResponse: api.UnprocessableEntityJSONResponse(e)}
	}
	if s.wa == nil {
		return invalid(errPasskeysOff), nil
	}
	a := mustActor(ctx)
	session, ok, err := s.takeCeremony(ctx, req.Body.ChallengeId, purposeRegister, &a.AdultID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return invalid(errChallenge), nil
	}
	adult, err := s.q.GetAdult(ctx, dbq.GetAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	user, rows, err := s.passkeyUser(ctx, adult, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) >= maxPasskeys {
		return api.AddPasskey409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("too_many_passkeys", fmt.Sprintf("You can have up to %d passkeys. Remove one first.", maxPasskeys)))}, nil
	}
	j, err := json.Marshal(req.Body.Credential)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(j))
	if err != nil {
		s.log.InfoContext(ctx, "add passkey: unreadable response", "err", err)
		return invalid(errPasskeyInvalid), nil
	}
	cred, err := s.wa.CreateCredential(user, session, parsed)
	if err != nil {
		s.log.InfoContext(ctx, "add passkey refused", "err", err)
		return invalid(errPasskeyInvalid), nil
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		if len(transports) < 8 {
			transports = append(transports, string(t))
		}
	}
	p, err := s.q.CreatePasskey(ctx, dbq.CreatePasskeyParams{
		HouseholdID: adult.HouseholdID, AdultID: adult.ID, CredentialID: cred.ID, PublicKey: cred.PublicKey,
		AttestationType: cred.AttestationType, Aaguid: aaguid(cred.Authenticator.AAGUID), SignCount: int64(cred.Authenticator.SignCount),
		Transports: transports, BackupEligible: pgtype.Bool{Bool: cred.Flags.BackupEligible, Valid: true}, BackedUp: cred.Flags.BackupState, Name: req.Body.Name,
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return api.AddPasskey409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("passkey_exists", "That passkey is already added."))}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.AddPasskey201JSONResponse(toPasskey(p)), nil
}

func aaguid(b []byte) []byte {
	if len(b) != 16 {
		return nil
	}
	return b
}

// RemovePasskey removes one of the adult's passkeys, unless it is their only way to sign in.
// The adult's row is locked first, so two removals at once can't both pass the guard.
func (s *Server) RemovePasskey(ctx context.Context, req api.RemovePasskeyRequestObject) (api.RemovePasskeyResponseObject, error) {
	a := mustActor(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	adult, err := q.LockAdult(ctx, dbq.LockAdultParams{HouseholdID: a.HouseholdID, ID: a.AdultID})
	if err != nil {
		return nil, err
	}
	rows, err := q.ListPasskeys(ctx, dbq.ListPasskeysParams{HouseholdID: a.HouseholdID, AdultID: a.AdultID})
	if err != nil {
		return nil, err
	}
	if adult.PasswordHash == nil && len(rows) == 1 && rows[0].ID == req.PasskeyId {
		return api.RemovePasskey409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse(apiError("last_sign_in", "This is your only way to sign in. Add another passkey or a password first."))}, nil
	}
	n, err := q.DeletePasskey(ctx, dbq.DeletePasskeyParams{ID: req.PasskeyId, HouseholdID: a.HouseholdID, AdultID: a.AdultID})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return api.RemovePasskey404JSONResponse{NotFoundJSONResponse: api.NotFoundJSONResponse(errNotFound)}, nil
	}
	return api.RemovePasskey204Response{}, tx.Commit(ctx)
}
