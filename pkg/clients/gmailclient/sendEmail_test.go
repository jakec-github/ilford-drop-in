package gmailclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComposeMessageRefusesALineBreakInAHeader: the recipient and subject are
// headers, so a line break in either would let the text after it become a
// header of its own.
func TestComposeMessageRefusesALineBreakInAHeader(t *testing.T) {
	_, err := composeMessage("a@example.com", "Friday\r\nBcc: b@example.com", "body")
	assert.Error(t, err)

	_, err = composeMessage("a@example.com\nBcc: b@example.com", "Friday", "body")
	assert.Error(t, err)
}

// TestComposeMessageEncodesOnlyANonASCIISubject: an ASCII subject is left
// alone, and any other is encoded, so an Organisation's name with an accent in it
// reaches the inbox intact.
func TestComposeMessageEncodesOnlyANonASCIISubject(t *testing.T) {
	message, err := composeMessage("a@example.com", "Availability by Friday", "Hello\n")
	require.NoError(t, err)
	assert.Equal(t, "To: a@example.com\r\nSubject: Availability by Friday\r\n\r\nHello\n", message)

	message, err = composeMessage("a@example.com", "Café availability", "Hello\n")
	require.NoError(t, err)
	assert.Contains(t, message, "Subject: =?utf-8?b?")
}
