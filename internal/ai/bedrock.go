package ai

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockClient wraps the AWS Bedrock runtime client
type BedrockClient struct {
	client *bedrockruntime.Client
	model  string
}

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

// NewBedrockClient creates a new Bedrock client
func NewBedrockClient(ctx context.Context, region, model string) (*BedrockClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(cfg)
	return &BedrockClient{
		client: client,
		model:  model,
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
	if maxTokens < 0 {
		maxTokens = 4096
	}
	// Safe cast since we've validated maxTokens is positive and reasonable
	//nolint:gosec // G115: maxTokens is validated above
	inferenceConfig := &types.InferenceConfiguration{
		MaxTokens: int32Ptr(int32(maxTokens)),
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
					// If unmarshal fails, create empty map
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
