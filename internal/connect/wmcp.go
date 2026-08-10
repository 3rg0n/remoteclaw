package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// WMCPMode implements Mode using a WebSocket connection to a WMCP backend relay server.
//
// Authorization is not performed here: the email allowlist is enforced at the
// single choke point in Agent.messageHandler, which every Mode feeds. WMCPMode's
// obligation is to report provenance honestly — see readLoop, where RoomType is
// reported as "group" because the relay protocol carries no room-type field and
// "group" is the strict interpretation (empty allowlist denies).
type WMCPMode struct {
	endpoint string
	token    string
	handler  MessageHandler
	logger   zerolog.Logger

	// conn is replaced wholesale on reconnect while readLoop/heartbeatLoop are
	// running, so it is published atomically. A mutex is unnecessary here:
	// websocket.Conn's methods are safe for concurrent use (except Read, which
	// only ever runs on the readLoop goroutine), and holding a lock across a
	// blocking Write would serialize heartbeats behind responses.
	conn   atomic.Pointer[websocket.Conn]
	cancel context.CancelFunc
	done   chan struct{}

	// requestIDs tracks the request_id for each space to include in responses
	requestIDs sync.Map // map[spaceID]requestID
}

// currentConn returns the active connection, or nil before Connect succeeds.
func (wm *WMCPMode) currentConn() *websocket.Conn {
	return wm.conn.Load()
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
	wm.conn.Store(conn)

	// Authenticate
	if err := wm.sendEnvelope(ctx, WMCPEnvelope{
		Type:  "auth",
		Token: wm.token,
	}); err != nil {
		_ = conn.Close(websocket.StatusAbnormalClosure, "auth send failed")
		return fmt.Errorf("failed to send auth message: %w", err)
	}

	// Wait for auth response
	authResp, err := wm.readEnvelope(ctx)
	if err != nil {
		_ = conn.Close(websocket.StatusAbnormalClosure, "auth read failed")
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	switch authResp.Type {
	case "auth_ok":
		wm.logger.Info().Msg("WMCP authentication successful")
	case "auth_error":
		_ = conn.Close(websocket.StatusNormalClosure, "auth failed")
		return fmt.Errorf("WMCP authentication failed: %s", authResp.Error)
	default:
		_ = conn.Close(websocket.StatusAbnormalClosure, "unexpected response")
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

	if conn := wm.currentConn(); conn != nil {
		return conn.Close(websocket.StatusNormalClosure, "agent shutting down")
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

			// RoomType is reported as "group" — the strict setting — because the
			// WMCP envelope carries no room-type field. A relay cannot prove a
			// message came from a 1:1 space, so the agent's authz choke point
			// must not grant it the permissive direct-message semantics
			// (empty allowlist = allow anyone). With "group", an unset
			// allowed_emails denies every sender rather than admitting all.
			msg := IncomingMessage{
				ID:       env.RequestID,
				SpaceID:  env.SpaceID,
				PersonID: env.PersonID,
				Email:    env.Email,
				Text:     env.Text,
				RoomType: "group",
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
		wm.conn.Store(conn)

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
	conn := wm.currentConn()
	if conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	return conn.Write(ctx, websocket.MessageText, data)
}

// readEnvelope reads and decodes a JSON envelope from the WebSocket.
// Only ever called from the readLoop goroutine (and from Connect/reconnect
// before that goroutine starts reading), since websocket.Conn.Read is the one
// method that is not safe for concurrent use.
func (wm *WMCPMode) readEnvelope(ctx context.Context) (*WMCPEnvelope, error) {
	conn := wm.currentConn()
	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read from WebSocket: %w", err)
	}

	var env WMCPEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("failed to unmarshal envelope: %w", err)
	}

	return &env, nil
}
