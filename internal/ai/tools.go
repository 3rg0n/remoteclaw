package ai

// ToolDef defines a tool available to the AI agent
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema
}

// AllTools returns all available tool definitions
func AllTools() []ToolDef {
	return []ToolDef{
		{
			Name:        "execute_command",
			Description: "Execute a shell command on the local system and get the output",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The command to execute",
					},
					"timeout": map[string]any{
						"type":        "string",
						"description": "Execution timeout as a duration string (e.g., '30s', '5m'). If not specified, uses default timeout.",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The file path to read",
					},
					"max_bytes": map[string]any{
						"type":        "integer",
						"description": "Maximum number of bytes to read. If not specified, reads entire file.",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The file path to write to",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "list_dir",
			Description: "List the contents of a directory",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The directory path to list",
					},
					"recursive": map[string]any{
						"type":        "boolean",
						"description": "Whether to recursively list subdirectories",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_processes",
			Description: "List all running processes on the system",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{},
				"required": []string{},
			},
		},
		{
			Name:        "kill_process",
			Description: "Terminate a process by its PID",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pid": map[string]any{
						"type":        "integer",
						"description": "The process ID to kill",
					},
				},
				"required": []string{"pid"},
			},
		},
		{
			Name:        "system_info",
			Description: "Get information about the system",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{},
				"required": []string{},
			},
		},
	}
}
