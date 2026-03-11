package agent

import (
	"sync"
	"testing"

	"github.com/ecopelan/remoteclaw/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConversationManager(t *testing.T) {
	cm := NewConversationManager(10)
	assert.NotNil(t, cm)
	assert.Equal(t, 10, cm.maxLen)
	assert.Equal(t, 0, len(cm.histories))
}

func TestConversationManager_GetHistory_UnknownKey(t *testing.T) {
	cm := NewConversationManager(10)

	history := cm.GetHistory("unknown_key")
	assert.NotNil(t, history)
	assert.Equal(t, 0, len(history))
}

func TestConversationManager_UpdateHistory_StoreAndRetrieve(t *testing.T) {
	cm := NewConversationManager(10)

	key := "space123"
	messages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Hello"},
			},
		},
		{
			Role: "assistant",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Hi there"},
			},
		},
	}

	cm.UpdateHistory(key, messages)
	retrieved := cm.GetHistory(key)

	assert.Equal(t, len(messages), len(retrieved))
	assert.Equal(t, messages[0].Role, retrieved[0].Role)
	assert.Equal(t, messages[1].Role, retrieved[1].Role)
}

func TestConversationManager_GetHistory_ReturnsCopy(t *testing.T) {
	cm := NewConversationManager(10)

	key := "space123"
	messages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Original"},
			},
		},
	}

	cm.UpdateHistory(key, messages)
	retrieved1 := cm.GetHistory(key)

	// Modify retrieved1
	if len(retrieved1) > 0 && len(retrieved1[0].Content) > 0 {
		retrieved1[0].Content[0].Text = "Modified"
	}

	// Get fresh copy should still have original
	retrieved2 := cm.GetHistory(key)
	assert.Equal(t, "Original", retrieved2[0].Content[0].Text)
}

func TestConversationManager_UpdateHistory_Trimming(t *testing.T) {
	cm := NewConversationManager(5)

	key := "space123"
	messages := make([]ai.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = ai.Message{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Message" + string(rune(i+48))},
			},
		}
	}

	cm.UpdateHistory(key, messages)
	retrieved := cm.GetHistory(key)

	// Should be trimmed to maxLen (5)
	assert.Equal(t, 5, len(retrieved))

	// Should keep the most recent messages (indices 5-9)
	assert.Equal(t, "Message5", retrieved[0].Content[0].Text)
	assert.Equal(t, "Message9", retrieved[4].Content[0].Text)
}

func TestConversationManager_UpdateHistory_NoTrimming(t *testing.T) {
	cm := NewConversationManager(0) // 0 means no trimming

	key := "space123"
	messages := make([]ai.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = ai.Message{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Message" + string(rune(i+48))},
			},
		}
	}

	cm.UpdateHistory(key, messages)
	retrieved := cm.GetHistory(key)

	// Should not be trimmed if maxLen is 0
	assert.Equal(t, 10, len(retrieved))
}

func TestConversationManager_Clear(t *testing.T) {
	cm := NewConversationManager(10)

	key := "space123"
	messages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Test"},
			},
		},
	}

	cm.UpdateHistory(key, messages)
	retrieved := cm.GetHistory(key)
	assert.Equal(t, 1, len(retrieved))

	cm.Clear(key)
	retrieved = cm.GetHistory(key)
	assert.Equal(t, 0, len(retrieved))
}

func TestConversationManager_ClearAll(t *testing.T) {
	cm := NewConversationManager(10)

	// Add history for multiple keys
	for i := 0; i < 3; i++ {
		key := "space" + string(rune(i+48))
		messages := []ai.Message{
			{
				Role: "user",
				Content: []ai.ContentBlock{
					{Type: "text", Text: "Test"},
				},
			},
		}
		cm.UpdateHistory(key, messages)
	}

	// Verify all histories exist
	for i := 0; i < 3; i++ {
		key := "space" + string(rune(i+48))
		retrieved := cm.GetHistory(key)
		assert.Equal(t, 1, len(retrieved))
	}

	// Clear all
	cm.ClearAll()

	// Verify all histories are gone
	for i := 0; i < 3; i++ {
		key := "space" + string(rune(i+48))
		retrieved := cm.GetHistory(key)
		assert.Equal(t, 0, len(retrieved))
	}
}

func TestConversationManager_MultipleKeys(t *testing.T) {
	cm := NewConversationManager(10)

	key1 := "space1"
	key2 := "space2"

	messages1 := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Message for space1"},
			},
		},
	}

	messages2 := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Message for space2"},
			},
		},
	}

	cm.UpdateHistory(key1, messages1)
	cm.UpdateHistory(key2, messages2)

	retrieved1 := cm.GetHistory(key1)
	retrieved2 := cm.GetHistory(key2)

	assert.Equal(t, "Message for space1", retrieved1[0].Content[0].Text)
	assert.Equal(t, "Message for space2", retrieved2[0].Content[0].Text)
}

func TestConversationManager_ConcurrentAccess(t *testing.T) {
	cm := NewConversationManager(100)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Create some initial messages
	baseMessages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Hello"},
			},
		},
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		switch i % 3 { //nolint:staticcheck // QF1003 does not apply to switch used for clarity
		case 0:
			// Writer goroutine
			go func(index int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					//nolint:gosec // G115: index is bounded by numGoroutines (10)
					key := "space" + string(rune(index+48))
					messages := baseMessages
					cm.UpdateHistory(key, messages)
				}
			}(i)
		case 1:
			// Reader goroutine
			go func(index int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					//nolint:gosec // G115: index is bounded by numGoroutines (10)
					key := "space" + string(rune(index+48))
					_ = cm.GetHistory(key)
				}
			}(i)
		case 2:
			// Clear goroutine
			go func(index int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					//nolint:gosec // G115: index is bounded by numGoroutines (10)
					key := "space" + string(rune(index+48))
					cm.Clear(key)
				}
			}(i)
		}
	}

	wg.Wait()

	// After concurrent operations, the manager should be in a valid state
	assert.NotNil(t, cm.histories)
}

func TestConversationManager_UpdateHistoryCopy(t *testing.T) {
	cm := NewConversationManager(10)

	key := "space123"
	originalMessages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Original"},
			},
		},
	}

	cm.UpdateHistory(key, originalMessages)

	// Modify the original slice
	if len(originalMessages) > 0 && len(originalMessages[0].Content) > 0 {
		originalMessages[0].Content[0].Text = "Modified externally"
	}

	// Internal history should not be affected
	retrieved := cm.GetHistory(key)
	require.Equal(t, 1, len(retrieved))
	assert.Equal(t, "Original", retrieved[0].Content[0].Text)
}
