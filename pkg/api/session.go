package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// sessionDuration is how long an admin session cookie stays valid.
const sessionDuration = 60 * 24 * time.Hour

// errInvalidSession covers every reason a session token fails verification
// (malformed, tampered, or expired). Callers only need to know it is not valid.
var errInvalidSession = errors.New("invalid session")

// signSession produces a session token proving the holder authenticated as email.
// The token is "<payload>.<sig>" where payload is base64url("<email>|<expiryUnix>")
// and sig is an HMAC-SHA256 of the payload under secret. It proves identity only;
// authority (admin allowlist membership) is re-checked separately on every request.
func signSession(secret []byte, email string, expiry time.Time) string {
	payload := email + "|" + strconv.FormatInt(expiry.Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(hmacSum(secret, encoded))
}

// verifySession returns the email carried by a valid, unexpired token, or
// errInvalidSession. now is passed in for testability.
func verifySession(secret []byte, token string, now time.Time) (string, error) {
	encoded, sig, found := strings.Cut(token, ".")
	if !found {
		return "", errInvalidSession
	}

	gotSig, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return "", errInvalidSession
	}
	if !hmac.Equal(gotSig, hmacSum(secret, encoded)) {
		return "", errInvalidSession
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", errInvalidSession
	}

	email, expiryStr, found := strings.Cut(string(payload), "|")
	if !found {
		return "", errInvalidSession
	}
	expiryUnix, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return "", errInvalidSession
	}
	if now.After(time.Unix(expiryUnix, 0)) {
		return "", errInvalidSession
	}

	return email, nil
}

// hmacSum returns the HMAC-SHA256 of msg under secret.
func hmacSum(secret []byte, msg string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}
