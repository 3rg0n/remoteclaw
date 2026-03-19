package connect

import "unicode/utf8"

// FormatResponse formats an AI response for Webex
// Truncates text that exceeds the Webex character limit (~7439 characters)
func FormatResponse(text string) string {
	const webexLimit = 7000

	if utf8.RuneCountInString(text) <= webexLimit {
		return text
	}

	// Truncate by rune count to avoid splitting multi-byte characters
	runes := []rune(text)
	return string(runes[:webexLimit]) + "\n[output truncated]"
}
