package agent

import (
	"sync"
	"time"

	"github.com/3rg0n/remoteclaw/internal/ai"
)

const maxHistoryBytes = 128 * 1024 // 128KB total content size cap per conversation

// conversationTTL is how long an idle conversation is kept before cleanup.
const conversationTTL = 24 * time.Hour

// ConversationManager manages per-user/space conversation history
type ConversationManager struct {
	histories  map[string][]ai.Message
	lastAccess map[string]time.Time // tracks last access for TTL cleanup
	maxLen     int
	mu         sync.RWMutex
}

// NewConversationManager creates a new conversation manager with a maximum history length.
// key is typically the spaceID or userEmail.
func NewConversationManager(maxHistory int) *ConversationManager {
	return &ConversationManager{
		histories:  make(map[string][]ai.Message),
		lastAccess: make(map[string]time.Time),
		maxLen:     maxHistory,
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
	cm.mu.RUnlock()
	cm.mu.Lock()
	cm.lastAccess[key] = time.Now()
	cm.mu.Unlock()
	cm.mu.RLock()

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

	// Trim by total content size to prevent unbounded memory from large tool results
	historyCopy = trimHistoryBySize(historyCopy, maxHistoryBytes)

	cm.histories[key] = historyCopy
	cm.lastAccess[key] = time.Now()

	// Clean up stale conversations while we hold the lock
	cm.cleanupStaleLocked()
}

// Clear removes the conversation history for a specific key.
func (cm *ConversationManager) Clear(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.histories, key)
	delete(cm.lastAccess, key)
}

// ClearAll removes all conversation histories.
func (cm *ConversationManager) ClearAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.histories = make(map[string][]ai.Message)
	cm.lastAccess = make(map[string]time.Time)
}

// cleanupStaleLocked removes conversations that haven't been accessed within conversationTTL.
// Must be called with cm.mu held for writing.
func (cm *ConversationManager) cleanupStaleLocked() {
	cutoff := time.Now().Add(-conversationTTL)
	for key, last := range cm.lastAccess {
		if last.Before(cutoff) {
			delete(cm.histories, key)
			delete(cm.lastAccess, key)
		}
	}
}

// trimHistoryBySize trims the oldest messages until total content size is within limit.
func trimHistoryBySize(history []ai.Message, maxBytes int) []ai.Message {
	for len(history) > 1 {
		total := 0
		for _, msg := range history {
			for _, b := range msg.Content {
				total += len(b.Text) + len(b.Content)
			}
		}
		if total <= maxBytes {
			break
		}
		// Drop the oldest message
		history = history[1:]
	}
	return history
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
