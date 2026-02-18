package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	webexhandler "github.com/3rg0n/webex-message-handler/go"
	"github.com/rs/zerolog"
)

// NativeMode implements Mode using direct Webex access via Mercury WebSocket
// for receiving messages and the REST API for sending.
type NativeMode struct {
	botToken  string
	allowlist *Allowlist
	handler   MessageHandler
	logger    zerolog.Logger
	receiver  *webexhandler.WebexMessageHandler
	sender    *WebexSender
	botName   string // resolved from /people/me, used to strip mentions
}

// NewNativeMode creates a new NativeMode instance.
// allowedEmails controls who may interact with the bot (empty = allow all).
func NewNativeMode(botToken string, allowedEmails []string, logger zerolog.Logger) *NativeMode {
	return &NativeMode{
		botToken:  botToken,
		allowlist: NewAllowlist(allowedEmails),
		logger:    logger,
		sender:    NewWebexSender(botToken),
	}
}

// Connect establishes the Mercury WebSocket connection to Webex.
func (nm *NativeMode) Connect(ctx context.Context) error {
	nm.logger.Info().Msg("NativeMode connecting to Webex via Mercury WebSocket")

	// Resolve bot identity for mention stripping
	if name, err := nm.resolveBotName(ctx); err != nil {
		nm.logger.Warn().Err(err).Msg("Could not resolve bot name; mention stripping may not work")
	} else {
		nm.botName = name
		nm.logger.Info().Str("botName", name).Msg("Resolved bot identity")
	}

	// Create the webex-message-handler
	handler, err := webexhandler.New(webexhandler.Config{
		Token: nm.botToken,
	})
	if err != nil {
		return fmt.Errorf("failed to create Webex message handler: %w", err)
	}
	nm.receiver = handler

	// Register message callback
	nm.receiver.OnMessageCreated(func(msg webexhandler.DecryptedMessage) {
		// Check allowlist — in group rooms, require explicit authorization
		if !nm.allowlist.IsAllowedInRoom(msg.PersonEmail, msg.RoomType) {
			nm.logger.Warn().
				Str("email", msg.PersonEmail).
				Str("roomType", msg.RoomType).
				Msg("Message from non-authorized email, ignoring")
			return
		}

		text := msg.Text

		// In group spaces, strip the bot @mention from the beginning of the message.
		// Webex delivers the mention as the bot's display name at the start of the text.
		if msg.RoomType == "group" && nm.botName != "" {
			text = stripBotMention(text, nm.botName)
		}

		incoming := IncomingMessage{
			ID:       msg.ID,
			SpaceID:  msg.RoomID,
			PersonID: msg.PersonID,
			Email:    msg.PersonEmail,
			Text:     text,
			RoomType: msg.RoomType,
		}

		if nm.handler != nil {
			nm.handler(ctx, incoming)
		}
	})

	nm.receiver.OnConnected(func() {
		nm.logger.Info().Msg("Connected to Webex Mercury WebSocket")
	})

	nm.receiver.OnDisconnected(func(reason string) {
		nm.logger.Warn().Str("reason", reason).Msg("Disconnected from Webex Mercury WebSocket")
	})

	nm.receiver.OnReconnecting(func(attempt int) {
		nm.logger.Info().Int("attempt", attempt).Msg("Reconnecting to Webex Mercury WebSocket")
	})

	nm.receiver.OnError(func(err error) {
		nm.logger.Error().Err(err).Msg("Webex message handler error")
	})

	// Connect to Mercury
	if err := nm.receiver.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Webex Mercury: %w", err)
	}

	return nil
}

// OnMessage registers a handler for incoming messages.
func (nm *NativeMode) OnMessage(handler MessageHandler) {
	nm.handler = handler
}

// SendMessage sends a text message to the specified Webex space via the REST API.
func (nm *NativeMode) SendMessage(ctx context.Context, spaceID string, text string) error {
	return nm.sender.SendMessage(ctx, spaceID, text)
}

// Close disconnects from Webex Mercury.
func (nm *NativeMode) Close() error {
	nm.logger.Info().Msg("NativeMode disconnecting from Webex")
	if nm.receiver != nil {
		return nm.receiver.Disconnect(context.Background())
	}
	return nil
}

// resolveBotName calls GET /people/me to discover the bot's display name.
func (nm *NativeMode) resolveBotName(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://webexapis.com/v1/people/me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+nm.botToken)

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL is a constant
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET /people/me returned %d: %s", resp.StatusCode, string(body))
	}

	var person struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&person); err != nil {
		return "", err
	}

	return person.DisplayName, nil
}

// stripBotMention removes the bot's display name from the beginning of a message.
// In group spaces, Webex delivers messages as "BotName command text".
func stripBotMention(text, botName string) string {
	// Try exact prefix match (case-insensitive)
	if len(text) > len(botName) && strings.EqualFold(text[:len(botName)], botName) {
		stripped := text[len(botName):]
		// Remove leading whitespace after the mention
		stripped = strings.TrimLeft(stripped, " ")
		if stripped != "" {
			return stripped
		}
	}
	return text
}
