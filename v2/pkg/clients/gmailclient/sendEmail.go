package gmailclient

import (
	"encoding/base64"
	"fmt"
	"time"

	"google.golang.org/api/gmail/v1"
)

const EMAIL_INTERVAL = 3 * time.Second

// SendEmail sends an email with the specified subject and body
// Throttles requests to respect Gmail API rate limits
func (c *Client) SendEmail(to, subject, body string) error {
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

	// Create the email message
	message := fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s", to, subject, body)

	// Encode the message in base64
	encodedMessage := base64.URLEncoding.EncodeToString([]byte(message))

	// Create the Gmail message
	gmailMessage := &gmail.Message{
		Raw: encodedMessage,
	}

	// Send the message
	_, err := c.service.Users.Messages.Send("me", gmailMessage).Do()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	// Update last send time
	c.lastSendTime = time.Now()

	return nil
}
