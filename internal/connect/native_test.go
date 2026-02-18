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
	allowedEmails := []string{"user@example.com"}

	nm := NewNativeMode(botToken, allowedEmails, logger)

	require.NotNil(t, nm)
	assert.Equal(t, botToken, nm.botToken)
	assert.NotNil(t, nm.allowlist)
	assert.NotNil(t, nm.sender)
}

// TestNativeModeOnMessage tests the OnMessage method
func TestNativeModeOnMessage(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

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
	nm := NewNativeMode("test-token", []string{}, logger)

	err := nm.Close()
	assert.NoError(t, err)
}

// TestNativeModeAllowlistIntegration tests that allowlist is properly wired
func TestNativeModeAllowlistIntegration(t *testing.T) {
	logger := zerolog.New(nil)
	allowedEmails := []string{"alice@example.com", "bob@example.com"}
	nm := NewNativeMode("test-token", allowedEmails, logger)

	// Allowed emails (case-insensitive via Allowlist)
	assert.True(t, nm.allowlist.IsAllowed("alice@example.com"))
	assert.True(t, nm.allowlist.IsAllowed("bob@example.com"))
	assert.True(t, nm.allowlist.IsAllowed("Alice@Example.COM"))

	// Disallowed email
	assert.False(t, nm.allowlist.IsAllowed("charlie@example.com"))
}

// TestNativeModeEmptyAllowlist tests that empty allowlist allows all
func TestNativeModeEmptyAllowlist(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

	assert.True(t, nm.allowlist.IsAllowed("anyone@example.com"))
	assert.True(t, nm.allowlist.IsAllowed("someone@example.com"))
}

// TestNativeModeImplementsMode tests that NativeMode implements Mode interface
func TestNativeModeImplementsMode(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

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
