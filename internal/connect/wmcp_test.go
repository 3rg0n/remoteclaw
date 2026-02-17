package connect

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWMCPMode tests the NewWMCPMode constructor
func TestNewWMCPMode(t *testing.T) {
	logger := zerolog.New(nil)
	endpoint := "https://wmcp.example.com"
	token := "wmcp-token"

	wm := NewWMCPMode(endpoint, token, logger)

	require.NotNil(t, wm)
	assert.Equal(t, endpoint, wm.endpoint)
	assert.Equal(t, token, wm.token)
}

// TestWMCPModeConnect tests the Connect method
func TestWMCPModeConnect(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("https://wmcp.example.com", "token", logger)

	ctx := context.Background()
	err := wm.Connect(ctx)

	assert.NoError(t, err)
}

// TestWMCPModeOnMessage tests the OnMessage method
func TestWMCPModeOnMessage(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("https://wmcp.example.com", "token", logger)

	handlerCalled := false
	handler := func(ctx context.Context, msg IncomingMessage) {
		handlerCalled = true
	}

	wm.OnMessage(handler)
	assert.NotNil(t, wm.handler)

	// Verify handler is stored
	wm.handler(context.Background(), IncomingMessage{})
	assert.True(t, handlerCalled)
}

// TestWMCPModeSendMessage tests the SendMessage method
func TestWMCPModeSendMessage(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("https://wmcp.example.com", "token", logger)

	ctx := context.Background()
	err := wm.SendMessage(ctx, "space-123", "test message")

	assert.NoError(t, err)
}

// TestWMCPModeClose tests the Close method
func TestWMCPModeClose(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("https://wmcp.example.com", "token", logger)

	err := wm.Close()

	assert.NoError(t, err)
}

// TestWMCPModeImplementsMode tests that WMCPMode implements Mode interface
func TestWMCPModeImplementsMode(t *testing.T) {
	logger := zerolog.New(nil)
	wm := NewWMCPMode("https://wmcp.example.com", "token", logger)

	// This is a compile-time check, but we verify it at runtime as well
	var _ Mode = wm

	// Verify all interface methods are present
	assert.NotNil(t, wm.Connect)
	assert.NotNil(t, wm.OnMessage)
	assert.NotNil(t, wm.SendMessage)
	assert.NotNil(t, wm.Close)
}

// TestWMCPModeWithDifferentEndpoints tests multiple WMCP instances
func TestWMCPModeWithDifferentEndpoints(t *testing.T) {
	logger := zerolog.New(nil)

	wm1 := NewWMCPMode("https://wmcp1.example.com", "token1", logger)
	wm2 := NewWMCPMode("https://wmcp2.example.com", "token2", logger)

	assert.NotEqual(t, wm1.endpoint, wm2.endpoint)
	assert.NotEqual(t, wm1.token, wm2.token)

	// Both should implement Mode
	var _ Mode = wm1
	var _ Mode = wm2
}
