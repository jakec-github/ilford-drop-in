package formsclient

import (
	"fmt"
)

// HasResponse checks if a form has any responses
func (c *Client) HasResponse(formID string) (bool, error) {
	responses, err := c.service.Forms.Responses.List(formID).Do()
	if err != nil {
		return false, fmt.Errorf("failed to list form responses: %w", err)
	}

	return len(responses.Responses) > 0, nil
}
