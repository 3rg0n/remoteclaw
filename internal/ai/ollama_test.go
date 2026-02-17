package ai

import (
	"testing"

	"github.com/ollama/ollama/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessagesToOllama_TextMessage(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello, world"},
		},
	}

	result := messagesToOllama(msg)
	require.Len(t, result, 1)
	assert.Equal(t, "user", result[0].Role)
	assert.Equal(t, "Hello, world", result[0].Content)
}

func TestMessagesToOllama_AssistantWithToolCalls(t *testing.T) {
	msg := Message{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "Let me check that."},
			{
				Type:      "tool_use",
				ToolUseID: "tool-1",
				ToolName:  "execute_command",
				Input:     map[string]any{"command": "echo hello"},
			},
		},
	}

	result := messagesToOllama(msg)
	require.Len(t, result, 1)
	assert.Equal(t, "assistant", result[0].Role)
	assert.Equal(t, "Let me check that.", result[0].Content)
	require.Len(t, result[0].ToolCalls, 1)
	assert.Equal(t, "tool-1", result[0].ToolCalls[0].ID)
	assert.Equal(t, "execute_command", result[0].ToolCalls[0].Function.Name)

	cmd, ok := result[0].ToolCalls[0].Function.Arguments.Get("command")
	assert.True(t, ok)
	assert.Equal(t, "echo hello", cmd)
}

func TestMessagesToOllama_ToolResults(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: "tool-1",
				Content:   "hello",
				IsError:   false,
			},
		},
	}

	result := messagesToOllama(msg)
	require.Len(t, result, 1)
	assert.Equal(t, "tool", result[0].Role)
	assert.Equal(t, "hello", result[0].Content)
	assert.Equal(t, "tool-1", result[0].ToolCallID)
}

func TestMessagesToOllama_ToolResultError(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: "tool-2",
				Content:   "command not found",
				IsError:   true,
			},
		},
	}

	result := messagesToOllama(msg)
	require.Len(t, result, 1)
	assert.Equal(t, "tool", result[0].Role)
	assert.Equal(t, "Error: command not found", result[0].Content)
}

func TestToolsToOllama(t *testing.T) {
	tools := []ToolDef{
		{
			Name:        "execute_command",
			Description: "Execute a shell command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The command to execute",
					},
					"timeout": map[string]any{
						"type":        "string",
						"description": "Execution timeout",
					},
				},
				"required": []string{"command"},
			},
		},
	}

	result := toolsToOllama(tools)
	require.Len(t, result, 1)

	assert.Equal(t, "function", result[0].Type)
	assert.Equal(t, "execute_command", result[0].Function.Name)
	assert.Equal(t, "Execute a shell command", result[0].Function.Description)
	assert.Equal(t, "object", result[0].Function.Parameters.Type)
	assert.Equal(t, []string{"command"}, result[0].Function.Parameters.Required)

	props := result[0].Function.Parameters.Properties
	require.NotNil(t, props)
	assert.Equal(t, 2, props.Len())

	cmdProp, ok := props.Get("command")
	assert.True(t, ok)
	assert.Equal(t, api.PropertyType{"string"}, cmdProp.Type)
	assert.Equal(t, "The command to execute", cmdProp.Description)
}

func TestToolsToOllama_RequiredAsAnySlice(t *testing.T) {
	// Test when required is []any (as it might come from JSON unmarshaling)
	tools := []ToolDef{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []any{"field1", "field2"},
			},
		},
	}

	result := toolsToOllama(tools)
	require.Len(t, result, 1)
	assert.Equal(t, []string{"field1", "field2"}, result[0].Function.Parameters.Required)
}

func TestOllamaResponseToMessage_TextOnly(t *testing.T) {
	oc := &OllamaClient{}
	resp := &api.ChatResponse{
		Message: api.Message{
			Role:    "assistant",
			Content: "Here is the output.",
		},
	}

	msg := oc.ollamaResponseToMessage(resp)
	assert.Equal(t, "assistant", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "text", msg.Content[0].Type)
	assert.Equal(t, "Here is the output.", msg.Content[0].Text)
}

func TestOllamaResponseToMessage_WithToolCalls(t *testing.T) {
	oc := &OllamaClient{}
	args := api.NewToolCallFunctionArguments()
	args.Set("command", "ls -la")

	resp := &api.ChatResponse{
		Message: api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{
					Function: api.ToolCallFunction{
						Name:      "execute_command",
						Arguments: args,
					},
				},
			},
		},
	}

	msg := oc.ollamaResponseToMessage(resp)
	assert.Equal(t, "assistant", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "tool_use", msg.Content[0].Type)
	assert.Equal(t, "execute_command", msg.Content[0].ToolName)
	assert.Equal(t, "ls -la", msg.Content[0].Input["command"])
	// Should generate an ID since Ollama didn't provide one
	assert.Contains(t, msg.Content[0].ToolUseID, "ollama-")
}

func TestOllamaResponseToMessage_WithExistingToolID(t *testing.T) {
	oc := &OllamaClient{}
	args := api.NewToolCallFunctionArguments()

	resp := &api.ChatResponse{
		Message: api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{
					ID: "existing-id",
					Function: api.ToolCallFunction{
						Name:      "test_tool",
						Arguments: args,
					},
				},
			},
		},
	}

	msg := oc.ollamaResponseToMessage(resp)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "existing-id", msg.Content[0].ToolUseID)
}

func TestOllamaResponseToMessage_TextAndToolCalls(t *testing.T) {
	oc := &OllamaClient{}
	args := api.NewToolCallFunctionArguments()
	args.Set("path", "/tmp/test.txt")

	resp := &api.ChatResponse{
		Message: api.Message{
			Role:    "assistant",
			Content: "I'll read that file for you.",
			ToolCalls: []api.ToolCall{
				{
					Function: api.ToolCallFunction{
						Name:      "read_file",
						Arguments: args,
					},
				},
			},
		},
	}

	msg := oc.ollamaResponseToMessage(resp)
	assert.Equal(t, "assistant", msg.Role)
	require.Len(t, msg.Content, 2)
	assert.Equal(t, "text", msg.Content[0].Type)
	assert.Equal(t, "I'll read that file for you.", msg.Content[0].Text)
	assert.Equal(t, "tool_use", msg.Content[1].Type)
	assert.Equal(t, "read_file", msg.Content[1].ToolName)
}

func TestToolIDSequenceIncrement(t *testing.T) {
	oc := &OllamaClient{}
	args := api.NewToolCallFunctionArguments()

	resp1 := &api.ChatResponse{
		Message: api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{Function: api.ToolCallFunction{Name: "t1", Arguments: args}},
			},
		},
	}
	resp2 := &api.ChatResponse{
		Message: api.Message{
			Role: "assistant",
			ToolCalls: []api.ToolCall{
				{Function: api.ToolCallFunction{Name: "t2", Arguments: args}},
			},
		},
	}

	msg1 := oc.ollamaResponseToMessage(resp1)
	msg2 := oc.ollamaResponseToMessage(resp2)

	// IDs should be unique and incrementing
	assert.Equal(t, "ollama-1", msg1.Content[0].ToolUseID)
	assert.Equal(t, "ollama-2", msg2.Content[0].ToolUseID)
}
