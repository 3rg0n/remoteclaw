package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/3rg0n/remoteclaw/internal/logging"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// OpenAICompatClient wraps an OpenAI-compatible client that implements Converser.
// It works with any endpoint that follows the OpenAI Chat Completions API spec,
// such as Ollama's /v1, LocalAI, mantle, or the real OpenAI API.
type OpenAICompatClient struct {
	client      openai.Client
	model       string
	temperature float64
}

// NewOpenAICompatClient creates a new OpenAI-compatible client.
// baseURL is the base URL of the OpenAI-compatible endpoint (e.g., "http://localhost:8000/v1" for Ollama).
// apiKey is the API key for authentication; can be empty for local endpoints that don't require it.
// model is the model name to use (required, e.g., "llama2", "gpt-4").
// temperature is the sampling temperature (0.0 to 2.0, typically 0.0-1.0).
func NewOpenAICompatClient(baseURL, apiKey, model string, temperature float64) (*OpenAICompatClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("openai-compat base URL is required")
	}
	if model == "" {
		return nil, fmt.Errorf("openai-compat model is required")
	}

	// Build client with base URL and API key
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)

	return &OpenAICompatClient{
		client:      client,
		model:       model,
		temperature: temperature,
	}, nil
}

// Converse calls the OpenAI-compatible chat endpoint and returns a single response.
// Implements the Converser interface.
func (oc *OpenAICompatClient) Converse(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
	maxTokens int,
) (*Message, error) {
	// Build OpenAI messages: start with system prompt if provided
	oaiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if system != "" {
		oaiMessages = append(oaiMessages, openai.SystemMessage(system))
	}

	// Convert internal messages to OpenAI format
	for _, msg := range messages {
		oaiMessages = append(oaiMessages, oc.messagesToOpenAI(msg)...)
	}

	// Build tools slice from tool definitions
	oaiTools := oc.toolsToOpenAI(tools)

	// Build chat request parameters
	params := openai.ChatCompletionNewParams{
		Messages:    oaiMessages,
		Model:       shared.ChatModel(oc.model),
		Temperature: openai.Float(oc.temperature),
	}

	// Set max tokens if provided
	if maxTokens > 0 {
		params.MaxTokens = openai.Int(int64(maxTokens))
	}

	// Set tools if provided
	if len(oaiTools) > 0 {
		params.Tools = oaiTools
	}

	// Call the OpenAI-compatible endpoint
	resp, err := oc.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai-compat chat failed: %w", err)
	}

	// Verify we got choices in the response
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai-compat returned no choices")
	}

	// Convert response to internal Message format
	return oc.responseToMessage(resp.Choices[0].Message), nil
}

// messagesToOpenAI converts an internal Message to one or more OpenAI message unions.
// Tool results are emitted as separate ToolMessage calls.
func (oc *OpenAICompatClient) messagesToOpenAI(msg Message) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion

	// Collect text content and tool use blocks
	var textContent string
	var toolCalls []openai.ChatCompletionMessageToolCallParam

	for _, cb := range msg.Content {
		switch cb.Type {
		case "text":
			textContent += cb.Text
		case "tool_use":
			// Marshal the Input map to JSON string
			args, err := json.Marshal(cb.Input)
			if err != nil {
				// Log but continue with empty arguments
				logger := logging.Get()
				logger.Warn().Err(err).Str("tool", cb.ToolName).Msg("Failed to marshal tool input")
				args = []byte("{}")
			}

			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
				ID: cb.ToolUseID,
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      cb.ToolName,
					Arguments: string(args),
				},
			})
		case "tool_result":
			// Tool results become separate ToolMessage calls
			content := cb.Content
			if cb.IsError {
				content = "Error: " + content
			}
			result = append(result, openai.ToolMessage(content, cb.ToolUseID))
		}
	}

	// Build assistant or user message depending on role
	if msg.Role == "assistant" {
		// Assistant message with optional text and tool calls
		if textContent != "" || len(toolCalls) > 0 {
			asst := openai.ChatCompletionAssistantMessageParam{}
			if textContent != "" {
				asst.Content.OfString = openai.String(textContent)
			}
			if len(toolCalls) > 0 {
				asst.ToolCalls = toolCalls
			}
			msgUnion := openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
			// Prepend the assistant message before any tool results
			result = append([]openai.ChatCompletionMessageParamUnion{msgUnion}, result...)
		}
	} else {
		// User or other role: emit as user message if there's text
		if textContent != "" {
			result = append([]openai.ChatCompletionMessageParamUnion{openai.UserMessage(textContent)}, result...)
		}
	}

	return result
}

// toolsToOpenAI converts internal tool definitions to OpenAI tool parameters
func (oc *OpenAICompatClient) toolsToOpenAI(toolDefs []ToolDef) []openai.ChatCompletionToolParam {
	tools := make([]openai.ChatCompletionToolParam, len(toolDefs))

	for i, td := range toolDefs {
		tools[i] = openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        td.Name,
				Description: openai.String(td.Description),
				Parameters:  shared.FunctionParameters(td.InputSchema),
			},
		}
	}

	return tools
}

// responseToMessage converts an OpenAI ChatCompletionMessage to an internal Message
func (oc *OpenAICompatClient) responseToMessage(msg openai.ChatCompletionMessage) *Message {
	var content []ContentBlock

	// Add text content if present
	if msg.Content != "" {
		content = append(content, ContentBlock{
			Type: "text",
			Text: msg.Content,
		})
	}

	// Add tool calls if present
	for _, tc := range msg.ToolCalls {
		// Parse the Arguments JSON string into a map
		var input map[string]any
		err := json.Unmarshal([]byte(tc.Function.Arguments), &input)
		if err != nil {
			// Log but continue with empty map
			logger := logging.Get()
			logger.Warn().Err(err).Str("tool", tc.Function.Name).Msg("Failed to unmarshal tool arguments")
			input = make(map[string]any)
		}

		content = append(content, ContentBlock{
			Type:      "tool_use",
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			Input:     input,
		})
	}

	return &Message{
		Role:    "assistant",
		Content: content,
	}
}
