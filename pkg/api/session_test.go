package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-session-secret-0123456789ab")

func TestSignVerifySession_RoundTrip(t *testing.T) {
	now := time.Now()
	token := signSession(testSecret, "admin@example.com", now.Add(time.Hour))

	email, err := verifySession(testSecret, token, now)
	require.NoError(t, err)
	assert.Equal(t, "admin@example.com", email)
}

func TestVerifySession_Expired(t *testing.T) {
	now := time.Now()
	token := signSession(testSecret, "admin@example.com", now.Add(-time.Second))

	_, err := verifySession(testSecret, token, now)
	assert.ErrorIs(t, err, errInvalidSession)
}

func TestVerifySession_TamperedPayload(t *testing.T) {
	now := time.Now()
	token := signSession(testSecret, "admin@example.com", now.Add(time.Hour))

	// Re-sign a different email under the same secret would be legitimate, so
	// instead flip a byte in the encoded payload: the signature no longer matches.
	tampered := "Q" + token[1:]

	_, err := verifySession(testSecret, tampered, now)
	assert.ErrorIs(t, err, errInvalidSession)
}

func TestVerifySession_WrongSecret(t *testing.T) {
	now := time.Now()
	token := signSession(testSecret, "admin@example.com", now.Add(time.Hour))

	_, err := verifySession([]byte("a-completely-different-secret-!!"), token, now)
	assert.ErrorIs(t, err, errInvalidSession)
}

func TestVerifySession_Malformed(t *testing.T) {
	now := time.Now()
	for _, token := range []string{"", "no-dot", "not-base64.also-not-base64", "."} {
		_, err := verifySession(testSecret, token, now)
		assert.ErrorIs(t, err, errInvalidSession, "token %q should be invalid", token)
	}
}
