package agent

import (
	"sync"

	"github.com/ecopelan/remoteclaw/internal/ai"
)

// ConversationManager manages per-user/space conversation history
type ConversationManager struct {
	histories map[string][]ai.Message
	maxLen    int
	mu        sync.RWMutex
}

// NewConversationManager creates a new conversation manager with a maximum history length.
// key is typically the spaceID or userEmail.
func NewConversationManager(maxHistory int) *ConversationManager {
	return &ConversationManager{
		histories: make(map[string][]ai.Message),
		maxLen:    maxHistory,
	}
}

// GetHistory returns a copy of the conversation history for the given key.
// Returns an empty slice if the key is not found.
func (cm *ConversationManager) GetHistory(key string) []ai.Message {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	history, ok := cm.histories[key]
	if !ok {
		return []ai.Message{}
	}

	// Return a deep copy to prevent external modifications
	historyCopy := make([]ai.Message, len(history))
	for i, msg := range history {
		historyCopy[i] = ai.Message{
			Role: msg.Role,
			Content: deepCopyContentBlocks(msg.Content),
		}
	}
	return historyCopy
}

// UpdateHistory stores the history for the given key.
// Trims to maxLen if exceeded, keeping the most recent messages.
func (cm *ConversationManager) UpdateHistory(key string, history []ai.Message) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Create a deep copy to prevent external modifications
	historyCopy := make([]ai.Message, len(history))
	for i, msg := range history {
		historyCopy[i] = ai.Message{
			Role: msg.Role,
			Content: deepCopyContentBlocks(msg.Content),
		}
	}

	// Trim to maxLen if necessary, keeping the most recent messages
	if cm.maxLen > 0 && len(historyCopy) > cm.maxLen {
		historyCopy = historyCopy[len(historyCopy)-cm.maxLen:]
	}

	cm.histories[key] = historyCopy
}

// Clear removes the conversation history for a specific key.
func (cm *ConversationManager) Clear(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.histories, key)
}

// ClearAll removes all conversation histories.
func (cm *ConversationManager) ClearAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.histories = make(map[string][]ai.Message)
}

// deepCopyContentBlocks creates a deep copy of a slice of ContentBlock structs
func deepCopyContentBlocks(blocks []ai.ContentBlock) []ai.ContentBlock {
	if len(blocks) == 0 {
		return []ai.ContentBlock{}
	}

	result := make([]ai.ContentBlock, len(blocks))
	for i, block := range blocks {
		result[i] = ai.ContentBlock{
			Type:      block.Type,
			Text:      block.Text,
			ToolUseID: block.ToolUseID,
			ToolName:  block.ToolName,
			Content:   block.Content,
			IsError:   block.IsError,
		}

		// Deep copy the Input map if present
		if block.Input != nil {
			result[i].Input = make(map[string]interface{})
			for k, v := range block.Input {
				result[i].Input[k] = v
			}
		}
	}

	return result
}
