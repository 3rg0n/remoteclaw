package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// WMCPMode implements Mode using a WebSocket connection to a WMCP backend relay server.
type WMCPMode struct {
	endpoint string
	token    string
	handler  MessageHandler
	logger   zerolog.Logger

	conn   *websocket.Conn
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex // protects conn writes

	// requestIDs tracks the request_id for each space to include in responses
	requestIDs sync.Map // map[spaceID]requestID
}

// NewWMCPMode creates a new WMCPMode instance.
func NewWMCPMode(endpoint, token string, logger zerolog.Logger) *WMCPMode {
	return &WMCPMode{
		endpoint: endpoint,
		token:    token,
		logger:   logger,
	}
}

// Connect dials the WMCP backend, authenticates, and starts the read/heartbeat loops.
func (wm *WMCPMode) Connect(ctx context.Context) error {
	// Reject insecure WebSocket endpoints
	if !strings.HasPrefix(wm.endpoint, "wss://") {
		wm.logger.Warn().Str("endpoint", wm.endpoint).
			Msg("WMCP endpoint does not use TLS (wss://). Auth tokens will be sent in cleartext.")
	}

	wm.logger.Info().Str("endpoint", wm.endpoint).Msg("WMCPMode connecting to backend")

	conn, resp, err := websocket.Dial(ctx, wm.endpoint, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("failed to dial WMCP endpoint: %w", err)
	}
	wm.conn = conn

	// Authenticate
	if err := wm.sendEnvelope(ctx, WMCPEnvelope{
		Type:  "auth",
		Token: wm.token,
	}); err != nil {
		_ = wm.conn.Close(websocket.StatusAbnormalClosure, "auth send failed")
		return fmt.Errorf("failed to send auth message: %w", err)
	}

	// Wait for auth response
	authResp, err := wm.readEnvelope(ctx)
	if err != nil {
		_ = wm.conn.Close(websocket.StatusAbnormalClosure, "auth read failed")
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	switch authResp.Type {
	case "auth_ok":
		wm.logger.Info().Msg("WMCP authentication successful")
	case "auth_error":
		_ = wm.conn.Close(websocket.StatusNormalClosure, "auth failed")
		return fmt.Errorf("WMCP authentication failed: %s", authResp.Error)
	default:
		_ = wm.conn.Close(websocket.StatusAbnormalClosure, "unexpected response")
		return fmt.Errorf("unexpected auth response type: %s", authResp.Type)
	}

	// Start background loops
	loopCtx, cancel := context.WithCancel(ctx)
	wm.cancel = cancel
	wm.done = make(chan struct{})

	go wm.readLoop(loopCtx)
	go wm.heartbeatLoop(loopCtx)

	return nil
}

// OnMessage registers a handler for incoming messages.
func (wm *WMCPMode) OnMessage(handler MessageHandler) {
	wm.handler = handler
}

// SendMessage sends a response to the WMCP backend for delivery to the specified space.
func (wm *WMCPMode) SendMessage(ctx context.Context, spaceID string, text string) error {
	// Look up the request_id for this space
	var requestID string
	if val, ok := wm.requestIDs.LoadAndDelete(spaceID); ok {
		requestID = val.(string)
	}

	return wm.sendEnvelope(ctx, WMCPEnvelope{
		Type:      "response",
		RequestID: requestID,
		SpaceID:   spaceID,
		Text:      text,
	})
}

// Close disconnects from the WMCP backend.
func (wm *WMCPMode) Close() error {
	wm.logger.Info().Str("endpoint", wm.endpoint).Msg("WMCPMode disconnecting")

	if wm.cancel != nil {
		wm.cancel()
	}

	// Wait for loops to finish
	if wm.done != nil {
		select {
		case <-wm.done:
		case <-time.After(5 * time.Second):
			wm.logger.Warn().Msg("Timed out waiting for WMCP loops to finish")
		}
	}

	if wm.conn != nil {
		return wm.conn.Close(websocket.StatusNormalClosure, "agent shutting down")
	}
	return nil
}

// readLoop reads messages from the WebSocket and dispatches them to the handler.
func (wm *WMCPMode) readLoop(ctx context.Context) {
	defer close(wm.done)

	for {
		env, err := wm.readEnvelope(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled, shutting down
			}
			wm.logger.Error().Err(err).Msg("Error reading from WMCP WebSocket")

			// Attempt reconnect
			if wm.reconnect(ctx) {
				continue
			}
			return
		}

		switch env.Type {
		case "message":
			wm.logger.Debug().
				Str("request_id", env.RequestID).
				Str("email", env.Email).
				Msg("Received message from WMCP")

			// Store request_id for response
			wm.requestIDs.Store(env.SpaceID, env.RequestID)

			msg := IncomingMessage{
				ID:       env.RequestID,
				SpaceID:  env.SpaceID,
				PersonID: env.PersonID,
				Email:    env.Email,
				Text:     env.Text,
			}

			if wm.handler != nil {
				wm.handler(ctx, msg)
			}

		case "heartbeat_ack":
			wm.logger.Debug().Msg("Received heartbeat_ack from WMCP")

		default:
			wm.logger.Warn().Str("type", env.Type).Msg("Unknown WMCP message type")
		}
	}
}

// heartbeatLoop sends periodic heartbeat messages.
func (wm *WMCPMode) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := wm.sendEnvelope(ctx, WMCPEnvelope{Type: "heartbeat"}); err != nil {
				if ctx.Err() == nil {
					wm.logger.Error().Err(err).Msg("Failed to send heartbeat")
				}
			}
		}
	}
}

// reconnect attempts to reconnect with exponential backoff.
func (wm *WMCPMode) reconnect(ctx context.Context) bool {
	backoff := time.Second
	maxBackoff := 60 * time.Second

	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}

		wm.logger.Info().Int("attempt", attempt).Msg("Attempting WMCP reconnect")

		conn, resp, err := websocket.Dial(ctx, wm.endpoint, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			wm.logger.Error().Err(err).Int("attempt", attempt).Msg("WMCP reconnect dial failed")
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Re-authenticate
		wm.mu.Lock()
		wm.conn = conn
		wm.mu.Unlock()

		if err := wm.sendEnvelope(ctx, WMCPEnvelope{
			Type:  "auth",
			Token: wm.token,
		}); err != nil {
			wm.logger.Error().Err(err).Msg("WMCP re-auth send failed")
			_ = conn.Close(websocket.StatusAbnormalClosure, "re-auth failed")
			continue
		}

		authResp, err := wm.readEnvelope(ctx)
		if err != nil || authResp.Type != "auth_ok" {
			wm.logger.Error().Err(err).Msg("WMCP re-auth response failed")
			_ = conn.Close(websocket.StatusAbnormalClosure, "re-auth failed")
			continue
		}

		wm.logger.Info().Int("attempt", attempt).Msg("WMCP reconnected successfully")
		return true
	}
}

// sendEnvelope writes a JSON envelope to the WebSocket.
func (wm *WMCPMode) sendEnvelope(ctx context.Context, env WMCPEnvelope) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	return wm.conn.Write(ctx, websocket.MessageText, data)
}

// readEnvelope reads and decodes a JSON envelope from the WebSocket.
func (wm *WMCPMode) readEnvelope(ctx context.Context) (*WMCPEnvelope, error) {
	_, data, err := wm.conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read from WebSocket: %w", err)
	}

	var env WMCPEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("failed to unmarshal envelope: %w", err)
	}

	return &env, nil
}
