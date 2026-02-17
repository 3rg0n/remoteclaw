package connect

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFormatResponseNormal tests FormatResponse with normal text
func TestFormatResponseNormal(t *testing.T) {
	text := "Hello, this is a normal message"
	result := FormatResponse(text)
	assert.Equal(t, text, result)
}

// TestFormatResponseLongText tests FormatResponse with long text (truncation)
func TestFormatResponseLongText(t *testing.T) {
	longText := strings.Repeat("x", 7100)

	result := FormatResponse(longText)

	assert.True(t, len(result) <= len(longText))
	assert.Contains(t, result, "[output truncated]")
	assert.True(t, len(result) < 7100)
}

// TestFormatResponseExactLimit tests FormatResponse at exact limit
func TestFormatResponseExactLimit(t *testing.T) {
	text := strings.Repeat("x", 7000)

	result := FormatResponse(text)

	assert.NotContains(t, result, "[output truncated]")
}

// TestFormatResponseEmpty tests FormatResponse with empty text
func TestFormatResponseEmpty(t *testing.T) {
	result := FormatResponse("")
	assert.Equal(t, "", result)
}

// TestFormatResponseJustOverLimit tests FormatResponse just over limit
func TestFormatResponseJustOverLimit(t *testing.T) {
	text := strings.Repeat("x", 7001)

	result := FormatResponse(text)

	assert.Contains(t, result, "[output truncated]")
	expectedLen := 7000 + 1 + len("[output truncated]")
	assert.Equal(t, expectedLen, len(result))
}
