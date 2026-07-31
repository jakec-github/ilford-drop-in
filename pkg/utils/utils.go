package utils

import (
	"crypto/rand"
	"encoding/base64"
)

// RandomToken returns a URL-safe random token: 32 bytes from crypto/rand,
// base64url-encoded without padding. It backs both the OAuth state parameter and
// the availability links that are a volunteer's only identity, so the entropy is
// sized for a bearer credential rather than for a nonce.
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
