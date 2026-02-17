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
	assert.Equal(t, allowedEmails, nm.allowedEmails)
}

// TestNativeModeConnect tests the Connect method
func TestNativeModeConnect(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

	ctx := context.Background()
	err := nm.Connect(ctx)

	assert.NoError(t, err)
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

// TestNativeModeSendMessage tests the SendMessage method
func TestNativeModeSendMessage(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

	ctx := context.Background()
	err := nm.SendMessage(ctx, "space-123", "test message")

	assert.NoError(t, err)
}

// TestNativeModeClose tests the Close method
func TestNativeModeClose(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

	err := nm.Close()

	assert.NoError(t, err)
}

// TestIsAllowedEmptyAllowlist tests isAllowed with empty allowlist
func TestIsAllowedEmptyAllowlist(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

	// Empty allowlist means all are allowed
	assert.True(t, nm.isAllowed("any@example.com"))
	assert.True(t, nm.isAllowed("someone@example.com"))
}

// TestIsAllowedWithAllowlist tests isAllowed with populated allowlist
func TestIsAllowedWithAllowlist(t *testing.T) {
	logger := zerolog.New(nil)
	allowedEmails := []string{"alice@example.com", "bob@example.com"}
	nm := NewNativeMode("test-token", allowedEmails, logger)

	// Allowed emails
	assert.True(t, nm.isAllowed("alice@example.com"))
	assert.True(t, nm.isAllowed("bob@example.com"))

	// Disallowed emails
	assert.False(t, nm.isAllowed("charlie@example.com"))
	assert.False(t, nm.isAllowed(""))
}

// TestIsAllowedCaseSensitive tests that isAllowed is case-sensitive
func TestIsAllowedCaseSensitive(t *testing.T) {
	logger := zerolog.New(nil)
	allowedEmails := []string{"User@example.com"}
	nm := NewNativeMode("test-token", allowedEmails, logger)

	// Exact match should work
	assert.True(t, nm.isAllowed("User@example.com"))

	// Different case should not match
	assert.False(t, nm.isAllowed("user@example.com"))
}

// TestNativeModeImplementsMode tests that NativeMode implements Mode interface
func TestNativeModeImplementsMode(t *testing.T) {
	logger := zerolog.New(nil)
	nm := NewNativeMode("test-token", []string{}, logger)

	// This is a compile-time check, but we verify it at runtime as well
	var _ Mode = nm

	// Verify all interface methods are present
	assert.NotNil(t, nm.Connect)
	assert.NotNil(t, nm.OnMessage)
	assert.NotNil(t, nm.SendMessage)
	assert.NotNil(t, nm.Close)
}
