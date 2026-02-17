package ai

// Message represents a conversation message
type Message struct {
	Role    string         // "user" or "assistant"
	Content []ContentBlock // Message content blocks
}

// ContentBlock represents a piece of content in a message
type ContentBlock struct {
	Type      string         // "text", "tool_use", "tool_result"
	Text      string         // For text blocks
	ToolUseID string         // For tool_use and tool_result blocks
	ToolName  string         // For tool_use blocks
	Input     map[string]any // For tool_use blocks
	Content   string         // For tool_result blocks (result content)
	IsError   bool           // For tool_result blocks
}
