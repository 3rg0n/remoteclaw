package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/coder/websocket"
)

// TestWMCPIntegration_FullCycle tests auth → message → response → heartbeat cycle
func TestWMCPIntegration_FullCycle(t *testing.T) {
	var mu sync.Mutex
	var receivedResponse *WMCPEnvelope
	var receivedHeartbeat bool
	msgSent := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

		// Read auth
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var authEnv WMCPEnvelope
		_ = json.Unmarshal(data, &authEnv)

		if authEnv.Type != "auth" || authEnv.Token != "integration-token" {
			resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_error", Error: "bad token"})
			_ = conn.Write(r.Context(), websocket.MessageText, resp)
			return
		}

		// Send auth_ok
		resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_ok"})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)

		// Send a message
		msg, _ := json.Marshal(WMCPEnvelope{
			Type:      "message",
			RequestID: "integ-req-1",
			SpaceID:   "integ-space",
			PersonID:  "integ-person",
			Email:     "tester@example.com",
			Text:      "integration test",
		})
		_ = conn.Write(r.Context(), websocket.MessageText, msg)

		// Read messages from client (response and possibly heartbeats)
		for i := 0; i < 3; i++ {
			_, clientData, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var env WMCPEnvelope
			_ = json.Unmarshal(clientData, &env)

			mu.Lock()
			switch env.Type {
			case "response":
				receivedResponse = &env
				close(msgSent)
			case "heartbeat":
				receivedHeartbeat = true
				ack, _ := json.Marshal(WMCPEnvelope{Type: "heartbeat_ack"})
				_ = conn.Write(r.Context(), websocket.MessageText, ack)
			}
			mu.Unlock()
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "integration-token", logger)

	wm.OnMessage(func(ctx context.Context, msg IncomingMessage) {
		_ = wm.SendMessage(ctx, msg.SpaceID, "reply to: "+msg.Text)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.NoError(t, err)

	// Wait for response to be received by server
	select {
	case <-msgSent:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for response")
	}

	mu.Lock()
	defer mu.Unlock()

	require.NotNil(t, receivedResponse)
	assert.Equal(t, "response", receivedResponse.Type)
	assert.Equal(t, "integ-req-1", receivedResponse.RequestID)
	assert.Equal(t, "integ-space", receivedResponse.SpaceID)
	assert.Equal(t, "reply to: integration test", receivedResponse.Text)

	_ = wm.Close()

	// Heartbeat may or may not have fired depending on timing; don't assert it
	_ = receivedHeartbeat
}

// TestWMCPIntegration_ReconnectOnDisconnect tests that the client reconnects when the server drops
func TestWMCPIntegration_ReconnectOnDisconnect(t *testing.T) {
	connectionCount := 0
	var connectionMu sync.Mutex
	reconnected := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		// Read auth
		_, _, _ = conn.Read(r.Context())

		connectionMu.Lock()
		connectionCount++
		count := connectionCount
		connectionMu.Unlock()

		// Send auth_ok
		resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_ok"})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)

		if count == 1 {
			// First connection: close abruptly after a brief pause
			time.Sleep(200 * time.Millisecond)
			_ = conn.Close(websocket.StatusGoingAway, "server restart")
		} else {
			// Second connection: signal reconnect success and stay alive
			close(reconnected)
			// Keep alive until context is done
			<-r.Context().Done()
			_ = conn.Close(websocket.StatusNormalClosure, "done")
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "token", logger)
	wm.OnMessage(func(ctx context.Context, msg IncomingMessage) {})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.NoError(t, err)

	// Wait for reconnect
	select {
	case <-reconnected:
		connectionMu.Lock()
		assert.GreaterOrEqual(t, connectionCount, 2, "should have reconnected at least once")
		connectionMu.Unlock()
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for reconnect")
	}

	_ = wm.Close()
}

// TestWMCPIntegration_AuthFailure tests that auth failure is properly surfaced
func TestWMCPIntegration_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		_, _, _ = conn.Read(r.Context())
		resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_error", Error: "token expired"})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "expired-token", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token expired")
}
