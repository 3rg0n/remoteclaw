package connect

import (
	"context"

	"github.com/rs/zerolog"
)

// WMCPMode implements Mode using WMCP backend
type WMCPMode struct {
	endpoint string
	token    string
	handler  MessageHandler
	logger   zerolog.Logger
}

// NewWMCPMode creates a new WMCPMode instance
func NewWMCPMode(endpoint, token string, logger zerolog.Logger) *WMCPMode {
	return &WMCPMode{
		endpoint: endpoint,
		token:    token,
		logger:   logger,
	}
}

// Connect establishes the connection
func (wm *WMCPMode) Connect(ctx context.Context) error {
	wm.logger.Info().Str("endpoint", wm.endpoint).Msg("WMCPMode connecting to WMCP backend")
	wm.logger.Debug().Msg("WMCP WebSocket connection will be established here")
	// Placeholder: WMCP integration will come later
	return nil
}

// OnMessage registers a handler for incoming messages
func (wm *WMCPMode) OnMessage(handler MessageHandler) {
	wm.logger.Debug().Msg("OnMessage handler registered")
	wm.handler = handler
}

// SendMessage sends a text message to a space
func (wm *WMCPMode) SendMessage(ctx context.Context, spaceID string, text string) error {
	wm.logger.Debug().Str("spaceID", spaceID).Int("textLen", len(text)).Msg("SendMessage called")
	wm.logger.Debug().Str("endpoint", wm.endpoint).Msg("Message would be sent to WMCP backend here")
	// Placeholder: WMCP integration will come later
	return nil
}

// Close disconnects from WMCP backend
func (wm *WMCPMode) Close() error {
	wm.logger.Info().Str("endpoint", wm.endpoint).Msg("WMCPMode disconnecting from WMCP backend")
	// Placeholder: Cleanup and resource teardown will be implemented
	return nil
}
