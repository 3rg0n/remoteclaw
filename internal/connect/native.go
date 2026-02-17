package connect

import (
	"context"

	"github.com/rs/zerolog"
)

// NativeMode implements Mode using direct Webex access
type NativeMode struct {
	botToken      string
	allowedEmails []string
	handler       MessageHandler
	logger        zerolog.Logger
	// The actual Webex client will be added when webex-message-handler is integrated
}

// NewNativeMode creates a new NativeMode instance
func NewNativeMode(botToken string, allowedEmails []string, logger zerolog.Logger) *NativeMode {
	return &NativeMode{
		botToken:      botToken,
		allowedEmails: allowedEmails,
		logger:        logger,
	}
}

// Connect establishes the connection
func (nm *NativeMode) Connect(ctx context.Context) error {
	nm.logger.Info().Msg("NativeMode connecting to Webex")
	nm.logger.Debug().Msg("Webex Mercury WebSocket client will be initialized here")
	// Placeholder: The actual Webex Mercury WebSocket client initialization will be added
	// when webex-message-handler is integrated
	return nil
}

// OnMessage registers a handler for incoming messages
func (nm *NativeMode) OnMessage(handler MessageHandler) {
	nm.logger.Debug().Msg("OnMessage handler registered")
	nm.handler = handler
}

// SendMessage sends a text message to a space
func (nm *NativeMode) SendMessage(ctx context.Context, spaceID string, text string) error {
	nm.logger.Debug().Str("spaceID", spaceID).Int("textLen", len(text)).Msg("SendMessage called")
	nm.logger.Debug().Msg("Message would be sent to Webex API here")
	// Placeholder: The actual message sending will be implemented when webex-message-handler is integrated
	return nil
}

// Close disconnects from Webex
func (nm *NativeMode) Close() error {
	nm.logger.Info().Msg("NativeMode disconnecting from Webex")
	// Placeholder: Cleanup and resource teardown will be implemented
	return nil
}

// isAllowed checks if an email is in the allowed list
// An empty allowlist means all emails are allowed
func (nm *NativeMode) isAllowed(email string) bool {
	if len(nm.allowedEmails) == 0 {
		// No allowlist configured, allow all
		return true
	}

	for _, allowed := range nm.allowedEmails {
		if allowed == email {
			return true
		}
	}

	return false
}
