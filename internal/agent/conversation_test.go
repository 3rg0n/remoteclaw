package agent

import (
	"fmt"
	"sync"
	"testing"

	"github.com/3rg0n/remoteclaw/internal/ai"
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

// TestConversationManager_ConcurrentDistinctKeys exercises readers and writers
// spread across distinct keys, so the map itself is contended even though no
// single history is. TestConversationManager_ConcurrentSameKey covers the
// stronger single-key case.
func TestConversationManager_ConcurrentDistinctKeys(t *testing.T) {
	cm := NewConversationManager(100)

	baseMessages := []ai.Message{
		{
			Role: "user",
			Content: []ai.ContentBlock{
				{Type: "text", Text: "Hello"},
			},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("space%d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				cm.UpdateHistory(key, baseMessages)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = cm.GetHistory(key)
			}
		}()
	}
	wg.Wait()

	assert.NotNil(t, cm.histories)
}

// TestConversationManager_ConcurrentSameKey exercises readers and writers
// contending on a *single* key, including reads of the nested Content/Input
// data. TestConversationManager_ConcurrentAccess gives each goroutine its own
// key, so it never exercises this path.
//
// GetHistory's safety depends on UpdateHistory replacing stored slices wholesale
// rather than mutating them in place. This test is the guard on that invariant:
// if a future change ever appends to or edits a stored history, it turns into a
// real data race and this test catches it under -race.
func TestConversationManager_ConcurrentSameKey(t *testing.T) {
	cm := NewConversationManager(20)

	const key = "shared-space"
	const iterations = 200

	msg := func(text string) []ai.Message {
		return []ai.Message{{
			Role: "user",
			Content: []ai.ContentBlock{{
				Type:  "text",
				Text:  text,
				Input: map[string]interface{}{"k": text},
			}},
		}}
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				cm.UpdateHistory(key, msg(fmt.Sprintf("w%d-%d", g, i)))
			}
		}(g)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				for _, m := range cm.GetHistory(key) {
					for _, b := range m.Content {
						_ = b.Text
						for k := range b.Input {
							_ = k
						}
					}
				}
			}
		}()
	}
	wg.Wait()

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
