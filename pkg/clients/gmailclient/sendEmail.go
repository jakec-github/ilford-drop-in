package gmailclient

import (
	"encoding/base64"
	"fmt"
	"mime"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
)

const EMAIL_INTERVAL = 3 * time.Second

// SendEmail sends an email with the specified subject and body
// Throttles requests to respect Gmail API rate limits
func (c *Client) SendEmail(to, subject, body string) error {
	message, err := composeMessage(to, subject, body)
	if err != nil {
		return err
	}

	c.sendMutex.Lock()
	defer c.sendMutex.Unlock()

	// Check if we need to wait before sending
	if !c.lastSendTime.IsZero() {
		elapsed := time.Since(c.lastSendTime)
		if elapsed < EMAIL_INTERVAL {
			waitTime := EMAIL_INTERVAL - elapsed
			time.Sleep(waitTime)
		}
	}

	// Encode the message in base64
	encodedMessage := base64.URLEncoding.EncodeToString([]byte(message))

	// Create the Gmail message
	gmailMessage := &gmail.Message{
		Raw: encodedMessage,
	}

	// Send the message
	_, err = c.service.Users.Messages.Send("me", gmailMessage).Do()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	// Update last send time
	c.lastSendTime = time.Now()

	return nil
}

// composeMessage builds the raw RFC 5322 message Gmail sends. The recipient and
// subject become headers, so a line break in either would end the header early
// and start one of the caller's choosing — a Bcc, or a body. Callers validate
// what they put there, but this is the last place before the mail goes, so it
// refuses rather than trusts them.
//
// The subject is encoded as RFC 2047 when it is not plain ASCII, which leaves an
// ASCII subject exactly as it was.
func composeMessage(to, subject, body string) (string, error) {
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return "", fmt.Errorf("refusing to send an email with a line break in its recipient or subject")
	}
	return fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s", to, mime.BEncoding.Encode("utf-8", subject), body), nil
}
