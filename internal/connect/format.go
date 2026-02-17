package connect

// FormatResponse formats an AI response for Webex
// Truncates text that exceeds the Webex character limit (~7439 characters)
func FormatResponse(text string) string {
	const webexLimit = 7000

	if len(text) > webexLimit {
		return text[:webexLimit] + "\n[output truncated]"
	}

	return text
}
