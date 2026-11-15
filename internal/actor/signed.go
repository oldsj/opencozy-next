package actor

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/oldsj/opencozy-next/internal/db/dbq"
)

// Signed device requests (slice 7). The kid app holds a P-256 key made at pairing and signs
// every request; the canonical string and headers are the old app's, so imported devices keep
// working.
const (
	DeviceHeader    = "X-OpenCozy-Device"
	TimestampHeader = "X-OpenCozy-Timestamp"
	NonceHeader     = "X-OpenCozy-Nonce"
	SignatureHeader = "X-OpenCozy-Signature"

	SignatureVersion = "OPENCOZY-DEVICE-V1"
	PairProofVersion = "OPENCOZY-PAIR-V1"

	// SignatureWindow is how far a request's timestamp may be from the server's clock, so a
	// request is acceptable for twice that. Nonces are kept for 300 seconds (device_nonce), well
	// past that span even when the app's and the database's clocks differ, so a replay can't
	// outlive its nonce.
	SignatureWindow = 60 * time.Second
)

var (
	timestampRE = regexp.MustCompile(`^[0-9]{1,12}$`)
	nonceRE     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
	signatureRE = regexp.MustCompile(`^[A-Za-z0-9_-]{86}$`)
)

// CanonicalRequest is the string a device signs.
func CanonicalRequest(method, pathAndQuery, timestamp, nonce string, body []byte, deviceID string) string {
	sum := sha256.Sum256(body)
	return strings.Join([]string{SignatureVersion, strings.ToUpper(method), pathAndQuery, timestamp, nonce, hex.EncodeToString(sum[:]), deviceID}, "\n")
}

// PairProof is the string a device signs to prove it holds the key it pairs with.
func PairProof(tokenOrCode, publicKey string) string {
	return strings.Join([]string{PairProofVersion, tokenOrCode, publicKey}, "\n")
}

// ParsePublicKey reads an uncompressed P-256 point (65 bytes, X9.63).
func ParsePublicKey(raw []byte) (*ecdsa.PublicKey, error) {
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
}

// VerifyP1363 checks a raw r||s P-256 signature (64 bytes, base64url) over SHA-256 of msg.
func VerifyP1363(pub *ecdsa.PublicKey, msg, sig string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(raw) != 64 {
		return false
	}
	sum := sha256.Sum256([]byte(msg))
	r, s := new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:])
	return ecdsa.Verify(pub, sum[:], r, s)
}

// signed resolves a signed device request. It reads the body (already capped) and puts it back.
// The nonce is recorded only after the signature verifies, so nobody can burn a device's nonces.
func (rv Resolver) signed(ctx context.Context, r *http.Request) (Actor, error) {
	id := strings.ToLower(r.Header.Get(DeviceHeader))
	ts, nonce, sig := r.Header.Get(TimestampHeader), r.Header.Get(NonceHeader), r.Header.Get(SignatureHeader)
	deviceID, err := uuid.Parse(id)
	if err != nil || deviceID.String() != id || !timestampRE.MatchString(ts) || !nonceRE.MatchString(nonce) || !signatureRE.MatchString(sig) {
		return Actor{}, ErrUnauthenticated
	}
	sec, _ := strconv.ParseInt(ts, 10, 64)
	if d := time.Since(time.Unix(sec, 0)); d > SignatureWindow || d < -SignatureWindow {
		return Actor{}, ErrUnauthenticated
	}
	body := []byte{}
	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return Actor{}, ErrUnauthenticated
			}
			return Actor{}, err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	d, err := rv.Q.GetSigningDevice(ctx, deviceID)
	if err != nil {
		return Actor{}, notFound(err)
	}
	pub, err := ParsePublicKey(d.PublicKey)
	if err != nil || !VerifyP1363(pub, CanonicalRequest(r.Method, r.RequestURI, ts, nonce, body, id), sig) {
		return Actor{}, ErrUnauthenticated
	}
	n, err := rv.Q.RecordDeviceNonce(ctx, dbq.RecordDeviceNonceParams{DeviceID: deviceID, Nonce: nonce})
	if err != nil {
		return Actor{}, err
	}
	if n == 0 {
		return Actor{}, ErrUnauthenticated
	}
	if err := rv.Q.TouchDevice(ctx, deviceID); err != nil {
		return Actor{}, err
	}
	return deviceActor(d.HouseholdID, d.ID, d.ChildID), nil
}

func deviceActor(household, device, child uuid.UUID) Actor {
	return Actor{Kind: Device, HouseholdID: household, CredentialID: device, Children: []uuid.UUID{child}}
}

// device looks a device up again by id, for Recheck.
func (rv Resolver) device(ctx context.Context, id uuid.UUID) (Actor, error) {
	d, err := rv.Q.GetSigningDevice(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrUnauthenticated
	} else if err != nil {
		return Actor{}, err
	}
	return deviceActor(d.HouseholdID, d.ID, d.ChildID), nil
}
