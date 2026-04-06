package ai

import (
	"context"
	"fmt"

	"github.com/3rg0n/remoteclaw/internal/logging"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockClient wraps the AWS Bedrock runtime client
type BedrockClient struct {
	client      *bedrockruntime.Client
	model       string
	temperature float64
}

// NewBedrockClient creates a new Bedrock client
func NewBedrockClient(ctx context.Context, region, model string, temperature float64) (*BedrockClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(cfg)
	return &BedrockClient{
		client:      client,
		model:       model,
		temperature: temperature,
	}, nil
}

// Converse calls the Bedrock Converse API
func (bc *BedrockClient) Converse(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
	maxTokens int,
) (*Message, error) {
	// Convert messages to Bedrock format
	bedrockMessages := make([]types.Message, len(messages))
	for i, msg := range messages {
		bedrockMessages[i] = messageToBedrock(msg)
	}

	// Build inference config
	// MaxTokens should be a positive int in the normal range
	if maxTokens <= 0 || maxTokens > 100_000 {
		maxTokens = 4096
	}
	temp := float32(bc.temperature)
	inferenceConfig := &types.InferenceConfiguration{
		MaxTokens:   int32Ptr(int32(maxTokens)), //nolint:gosec // G115: maxTokens is range-checked above (0 < n <= 100000)
		Temperature: &temp,
	}

	// Build tool configuration if tools are provided
	var toolConfig *types.ToolConfiguration
	if len(tools) > 0 {
		toolConfig = &types.ToolConfiguration{
			Tools: toolsToBedrock(tools),
		}
	}

	// Call Converse API
	input := &bedrockruntime.ConverseInput{
		ModelId:           &bc.model,
		System:            []types.SystemContentBlock{&types.SystemContentBlockMemberText{Value: system}},
		Messages:          bedrockMessages,
		InferenceConfig:   inferenceConfig,
		ToolConfig:        toolConfig,
	}

	output, err := bc.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock converse failed: %w", err)
	}

	// Parse response
	if output.Output == nil {
		return nil, fmt.Errorf("empty response from bedrock")
	}

	messageOutput, ok := output.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return nil, fmt.Errorf("unexpected response type from bedrock")
	}

	result := &Message{
		Role:    string(messageOutput.Value.Role),
		Content: bedrockContentToContent(messageOutput.Value.Content),
	}

	return result, nil
}

// messageToBedrock converts an internal Message to Bedrock Message format
func messageToBedrock(msg Message) types.Message {
	contentBlocks := make([]types.ContentBlock, 0, len(msg.Content))

	for _, cb := range msg.Content {
		switch cb.Type {
		case "text":
			contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{
				Value: cb.Text,
			})
		case "tool_use":
			inputDoc := document.NewLazyDocument(cb.Input)
			contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolUse{
				Value: types.ToolUseBlock{
					ToolUseId: &cb.ToolUseID,
					Name:      &cb.ToolName,
					Input:     inputDoc,
				},
			})
		case "tool_result":
			status := types.ToolResultStatusSuccess
			if cb.IsError {
				status = types.ToolResultStatusError
			}
			contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: &cb.ToolUseID,
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{
							Value: cb.Content,
						},
					},
					Status: status,
				},
			})
		}
	}

	role := types.ConversationRoleUser
	if msg.Role == "assistant" {
		role = types.ConversationRoleAssistant
	}

	return types.Message{
		Role:    role,
		Content: contentBlocks,
	}
}

// bedrockContentToContent converts Bedrock content blocks to internal format
func bedrockContentToContent(blocks []types.ContentBlock) []ContentBlock {
	result := make([]ContentBlock, 0, len(blocks))

	for _, block := range blocks {
		switch v := block.(type) {
		case *types.ContentBlockMemberText:
			result = append(result, ContentBlock{
				Type: "text",
				Text: v.Value,
			})
		case *types.ContentBlockMemberToolUse:
			toolUseBlock := v.Value
			// Convert document.Interface to map[string]any
			var inputMap map[string]any
			if toolUseBlock.Input != nil {
				// Try to deserialize the document
				err := toolUseBlock.Input.UnmarshalSmithyDocument(&inputMap)
				if err != nil {
					// Log deserialization failure for debugging — silent failures mask bugs
					logger := logging.Get()
					logger.Warn().Err(err).Str("tool", derefString(toolUseBlock.Name)).
						Msg("Failed to unmarshal Bedrock tool input, using empty map")
					inputMap = make(map[string]any)
				}
			}

			result = append(result, ContentBlock{
				Type:      "tool_use",
				ToolUseID: derefString(toolUseBlock.ToolUseId),
				ToolName:  derefString(toolUseBlock.Name),
				Input:     inputMap,
			})
		case *types.ContentBlockMemberToolResult:
			toolResultBlock := v.Value
			content := ""
			if len(toolResultBlock.Content) > 0 {
				if textBlock, ok := toolResultBlock.Content[0].(*types.ToolResultContentBlockMemberText); ok {
					content = textBlock.Value
				}
			}

			isError := toolResultBlock.Status == types.ToolResultStatusError
			result = append(result, ContentBlock{
				Type:      "tool_result",
				ToolUseID: derefString(toolResultBlock.ToolUseId),
				Content:   content,
				IsError:   isError,
			})
		}
	}

	return result
}

// toolsToBedrock converts tool definitions to Bedrock format
func toolsToBedrock(toolDefs []ToolDef) []types.Tool {
	tools := make([]types.Tool, len(toolDefs))

	for i, toolDef := range toolDefs {
		// Convert the JSON schema to Bedrock format
		schemaDoc := document.NewLazyDocument(toolDef.InputSchema)

		toolSpec := types.ToolSpecification{
			Name:        &toolDef.Name,
			Description: &toolDef.Description,
			InputSchema: &types.ToolInputSchemaMemberJson{
				Value: schemaDoc,
			},
		}

		tools[i] = &types.ToolMemberToolSpec{
			Value: toolSpec,
		}
	}

	return tools
}

// Helper functions

func int32Ptr(v int32) *int32 {
	return &v
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
