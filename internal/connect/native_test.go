package connect

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewNativeMode tests the NewNativeMode constructor
func TestNewNativeMode(t *testing.T) {
	logger := zerolog.New(nil)
	botToken := "test-token"

	nm := NewNativeMode(botToken, logger)

	require.NotNil(t, nm)
	assert.Equal(t, botToken, nm.botToken)
	assert.NotNil(t, nm.sender)
}

// TestNativeModeOnMessage tests the OnMessage method
func TestNativeModeOnMessage(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", logger)

	handlerCalled := false
	handler := func(ctx context.Context, msg IncomingMessage) {
		handlerCalled = true
	}

	nm.OnMessage(handler)
	assert.NotNil(t, nm.handler)

	// Verify handler is stored
	nm.handler(context.Background(), IncomingMessage{})
	assert.True(t, handlerCalled)
}

// TestNativeModeCloseWithoutConnect tests Close without prior Connect
func TestNativeModeCloseWithoutConnect(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", logger)

	err := nm.Close()
	assert.NoError(t, err)
}

// TestNativeModeImplementsMode tests that NativeMode implements Mode interface
func TestNativeModeImplementsMode(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", logger)

	// Compile-time check
	var _ Mode = nm
}

// TestStripBotMention tests mention stripping for group spaces
func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		botName string
		want    string
	}{
		{"strips exact prefix", "WCC check disk", "WCC", "check disk"},
		{"strips case-insensitive", "wcc check disk", "WCC", "check disk"},
		{"strips with extra spaces", "WCC   check disk", "WCC", "check disk"},
		{"no match returns original", "hello world", "WCC", "hello world"},
		{"empty bot name returns original", "WCC check disk", "", "WCC check disk"},
		{"only bot name returns original", "WCC", "WCC", "WCC"},
		{"bot name with spaces", "My Bot check disk", "My Bot", "check disk"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBotMention(tt.text, tt.botName)
			assert.Equal(t, tt.want, got)
		})
	}
}
