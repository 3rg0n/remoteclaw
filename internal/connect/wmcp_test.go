package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/coder/websocket"
)

// TestNewWMCPMode tests the NewWMCPMode constructor
func TestNewWMCPMode(t *testing.T) {
	logger := zerolog.New(nil)
	endpoint := "ws://localhost:8080"
	token := "wmcp-token"

	wm := NewWMCPMode(endpoint, token, logger)

	require.NotNil(t, wm)
	assert.Equal(t, endpoint, wm.endpoint)
	assert.Equal(t, token, wm.token)
}

// TestWMCPModeImplementsMode tests that WMCPMode implements Mode interface
func TestWMCPModeImplementsMode(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("ws://localhost", "token", logger)
	var _ Mode = wm
}

// TestWMCPModeOnMessage tests the OnMessage method
func TestWMCPModeOnMessage(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("ws://localhost", "token", logger)

	handlerCalled := false
	wm.OnMessage(func(ctx context.Context, msg IncomingMessage) {
		handlerCalled = true
	})

	assert.NotNil(t, wm.handler)
	wm.handler(context.Background(), IncomingMessage{})
	assert.True(t, handlerCalled)
}

// TestWMCPModeConnectAndAuth tests the full auth flow with a mock server
func TestWMCPModeConnectAndAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		// Read auth message
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var env WMCPEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			return
		}

		if env.Type == "auth" && env.Token == "valid-token" {
			resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_ok"})
			_ = conn.Write(r.Context(), websocket.MessageText, resp)
		} else {
			resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_error", Error: "invalid token"})
			_ = conn.Write(r.Context(), websocket.MessageText, resp)
		}

		// Keep connection alive briefly for cleanup
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "valid-token", logger)
	wm.OnMessage(func(ctx context.Context, msg IncomingMessage) {})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.NoError(t, err)

	// Cleanup
	_ = wm.Close()
}

// TestWMCPModeAuthFailure tests that auth failure returns an error
func TestWMCPModeAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		// Read auth message
		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		// Always reject
		resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_error", Error: "bad token"})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "bad-token", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

// TestWMCPModeMessageDelivery tests that messages from the server are delivered to the handler
func TestWMCPModeMessageDelivery(t *testing.T) {
	msgReceived := make(chan IncomingMessage, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		// Handle auth
		_, _, _ = conn.Read(r.Context())
		resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_ok"})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)

		// Send a message to the client
		msg, _ := json.Marshal(WMCPEnvelope{
			Type:      "message",
			RequestID: "req-1",
			SpaceID:   "space-1",
			PersonID:  "person-1",
			Email:     "user@example.com",
			Text:      "hello agent",
		})
		_ = conn.Write(r.Context(), websocket.MessageText, msg)

		// Wait for response
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "token", logger)

	wm.OnMessage(func(ctx context.Context, msg IncomingMessage) {
		msgReceived <- msg
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.NoError(t, err)

	// Wait for the message
	select {
	case msg := <-msgReceived:
		assert.Equal(t, "req-1", msg.ID)
		assert.Equal(t, "space-1", msg.SpaceID)
		assert.Equal(t, "person-1", msg.PersonID)
		assert.Equal(t, "user@example.com", msg.Email)
		assert.Equal(t, "hello agent", msg.Text)
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for message")
	}

	_ = wm.Close()
}

// TestWMCPModeSendResponse tests sending a response back to the server
func TestWMCPModeSendResponse(t *testing.T) {
	responseReceived := make(chan WMCPEnvelope, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

		// Handle auth
		_, _, _ = conn.Read(r.Context())
		resp, _ := json.Marshal(WMCPEnvelope{Type: "auth_ok"})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)

		// Send a message to trigger a response
		msg, _ := json.Marshal(WMCPEnvelope{
			Type:      "message",
			RequestID: "req-42",
			SpaceID:   "space-42",
			Email:     "user@example.com",
			Text:      "hello",
		})
		_ = conn.Write(r.Context(), websocket.MessageText, msg)

		// Read the response from the client
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var env WMCPEnvelope
		_ = json.Unmarshal(data, &env)
		responseReceived <- env
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	logger := zerolog.New(nil)
	wm := NewWMCPMode(wsURL, "token", logger)

	handlerDone := make(chan struct{})
	wm.OnMessage(func(ctx context.Context, msg IncomingMessage) {
		// Send response
		_ = wm.SendMessage(ctx, msg.SpaceID, "processed: "+msg.Text)
		close(handlerDone)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wm.Connect(ctx)
	require.NoError(t, err)

	// Wait for handler to complete
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for handler")
	}

	// Check the response
	select {
	case resp := <-responseReceived:
		assert.Equal(t, "response", resp.Type)
		assert.Equal(t, "req-42", resp.RequestID)
		assert.Equal(t, "space-42", resp.SpaceID)
		assert.Equal(t, "processed: hello", resp.Text)
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for response")
	}

	_ = wm.Close()
}

// TestWMCPModeCloseWithoutConnect tests Close without prior Connect
func TestWMCPModeCloseWithoutConnect(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("ws://localhost", "token", logger)

	err := wm.Close()
	assert.NoError(t, err)
}
