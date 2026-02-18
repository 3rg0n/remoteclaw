package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const webexMessagesURL = "https://webexapis.com/v1/messages"

// WebexSender sends messages via the Webex REST API.
type WebexSender struct {
	token      string
	httpClient *http.Client
}

// NewWebexSender creates a sender that POSTs messages to the Webex API.
func NewWebexSender(token string) *WebexSender {
	return &WebexSender{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type webexMessageBody struct {
	RoomID   string `json:"roomId"`
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
}

// SendMessage sends a text message to the specified Webex space.
// The text is sent as both plain text (fallback) and markdown (rich formatting).
func (ws *WebexSender) SendMessage(ctx context.Context, spaceID, text string) error {
	body := webexMessageBody{
		RoomID:   spaceID,
		Text:     text,
		Markdown: text,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webexMessagesURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+ws.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ws.httpClient.Do(req) //nolint:gosec // URL is a constant, not user-provided
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webex API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
