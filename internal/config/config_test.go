package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-bot-token"
  allowed_emails:
    - "admin@example.com"
    - "user@example.com"
aws:
  region: "us-east-1"
ai:
  model: "custom-model"
  max_tokens: 8192
  max_iterations: 20
execution:
  default_timeout: "45s"
  max_timeout: "10m"
  shell: "/bin/bash"
logging:
  level: "debug"
  format: "text"
  file: "/var/log/remoteclaw.log"
health:
  enabled: false
  addr: "0.0.0.0:8080"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "native", cfg.Mode)
	assert.Equal(t, "test-bot-token", cfg.Webex.BotToken)
	assert.Len(t, cfg.Webex.AllowedEmails, 2)
	assert.Equal(t, "admin@example.com", cfg.Webex.AllowedEmails[0])
	assert.Equal(t, "user@example.com", cfg.Webex.AllowedEmails[1])

	assert.Equal(t, "us-east-1", cfg.AWS.Region)

	assert.Equal(t, "custom-model", cfg.AI.Model)
	assert.Equal(t, 8192, cfg.AI.MaxTokens)
	assert.Equal(t, 20, cfg.AI.MaxIterations)

	assert.Equal(t, 45*time.Second, cfg.Execution.DefaultTimeout)
	assert.Equal(t, 10*time.Minute, cfg.Execution.MaxTimeout)
	assert.Equal(t, "/bin/bash", cfg.Execution.Shell)

	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "text", cfg.Logging.Format)
	assert.Equal(t, "/var/log/remoteclaw.log", cfg.Logging.File)

	assert.False(t, cfg.Health.Enabled)
	assert.Equal(t, "0.0.0.0:8080", cfg.Health.Addr)
}

func TestLoadConfigWithEnvVarExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Set environment variables for testing
	t.Setenv("WEBEX_BOT_TOKEN", "env-bot-token-value")
	t.Setenv("WMCP_TOKEN", "env-wmcp-token-value")
	t.Setenv("WMCP_ENDPOINT", "https://env.wmcp.example.com")

	configContent := `
mode: wmcp
webex:
  bot_token: "${WEBEX_BOT_TOKEN}"
wmcp:
  endpoint: "${WMCP_ENDPOINT}"
  token: "${WMCP_TOKEN}"
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "env-bot-token-value", cfg.Webex.BotToken)
	assert.Equal(t, "https://env.wmcp.example.com", cfg.WMCP.Endpoint)
	assert.Equal(t, "env-wmcp-token-value", cfg.WMCP.Token)
}

func TestLoadConfigWithDefaults(t *testing.T) {
	// Clear AWS creds so auto-detection resolves to "local"
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Minimal config with required fields
	configContent := `
mode: native
webex:
  bot_token: "test-token"
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Check defaults are applied
	assert.Equal(t, "auto", cfg.AI.Provider)
	assert.Equal(t, "phi4-mini", cfg.AI.Model)
	assert.Equal(t, 4096, cfg.AI.MaxTokens)
	assert.Equal(t, 10, cfg.AI.MaxIterations)
	assert.InDelta(t, 0.2, cfg.AI.Temperature, 0.001)

	assert.Equal(t, 30*time.Second, cfg.Execution.DefaultTimeout)
	assert.Equal(t, 5*time.Minute, cfg.Execution.MaxTimeout)

	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)

	assert.True(t, cfg.Health.Enabled)
	assert.Equal(t, "127.0.0.1:9090", cfg.Health.Addr)
}

func TestValidateNativeModeRequiresWebexToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Missing bot_token
	configContent := `
mode: native
webex:
  bot_token: ""
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "webex.bot_token is required in native mode")
}

func TestValidateWMCPModeRequiresEndpointAndToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Missing endpoint
	configContent := `
mode: wmcp
wmcp:
  endpoint: ""
  token: "test-token"
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "wmcp.endpoint is required in wmcp mode")
}

func TestValidateWMCPModeRequiresToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Missing token
	configContent := `
mode: wmcp
wmcp:
  endpoint: "https://wmcp.example.com"
  token: ""
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "wmcp.token is required in wmcp mode")
}

func TestValidateInvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: invalid
webex:
  bot_token: "test-token"
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid mode")
}

func TestValidateRequiresAWSRegionForBedrock(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  provider: "bedrock"
aws:
  region: ""
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "aws.region is required when using bedrock provider")
}

func TestValidateAWSRegionNotRequiredForLocal(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  provider: "local"
aws:
  region: ""
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "local", cfg.ResolveAIProvider())
}

func TestLoadNonExistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadWithPartialEnvVarExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	t.Setenv("BOT_TOKEN", "expanded-token")
	// Note: UNDEFINED_VAR is not set

	configContent := `
mode: native
webex:
  bot_token: "prefix-${BOT_TOKEN}-suffix"
aws:
  region: "us-west-2"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "prefix-expanded-token-suffix", cfg.Webex.BotToken)
}

func TestLoadConfigWMCPMode(t *testing.T) {
	// Clear AWS creds so auto-detection resolves to "local"
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: wmcp
wmcp:
  endpoint: "https://wmcp.example.com"
  token: "wmcp-secret-token"
aws:
  region: "eu-west-1"
logging:
  level: "warn"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "wmcp", cfg.Mode)
	assert.Equal(t, "https://wmcp.example.com", cfg.WMCP.Endpoint)
	assert.Equal(t, "wmcp-secret-token", cfg.WMCP.Token)
	assert.Equal(t, "eu-west-1", cfg.AWS.Region)
	assert.Equal(t, "warn", cfg.Logging.Level)

	// Verify other defaults still apply
	assert.Equal(t, "phi4-mini", cfg.AI.Model)
	assert.True(t, cfg.Health.Enabled)
}

func TestTimeoutParsing(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
aws:
  region: "us-west-2"
execution:
  default_timeout: "2m30s"
  max_timeout: "1h"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, 2*time.Minute+30*time.Second, cfg.Execution.DefaultTimeout)
	assert.Equal(t, time.Hour, cfg.Execution.MaxTimeout)
}

func TestConfigBuildInfo(t *testing.T) {
	// Just verify build info variables exist and are accessible
	assert.NotEmpty(t, Version)
	assert.NotEmpty(t, Commit)
	assert.NotEmpty(t, Date)
}

func TestExpandEnvVarsMethod(t *testing.T) {
	cfg := &Config{}
	cfg.Webex.BotToken = "${TEST_VAR}"
	cfg.WMCP.Endpoint = "https://${HOST}/api"
	cfg.WMCP.Token = "${SECRET}"
	cfg.Execution.Shell = "${SHELL_PATH}"

	t.Setenv("TEST_VAR", "expanded-value")
	t.Setenv("HOST", "example.com")
	t.Setenv("SECRET", "secret-value")
	t.Setenv("SHELL_PATH", "/bin/bash")

	cfg.expandEnvVars()

	assert.Equal(t, "expanded-value", cfg.Webex.BotToken)
	assert.Equal(t, "https://example.com/api", cfg.WMCP.Endpoint)
	assert.Equal(t, "secret-value", cfg.WMCP.Token)
	assert.Equal(t, "/bin/bash", cfg.Execution.Shell)
}

func TestValidateStructMethod(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid native mode",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AWS: AWSConfig{
					Region: "us-west-2",
				},
			},
			wantErr: false,
		},
		{
			name: "valid wmcp mode",
			cfg: &Config{
				Mode: "wmcp",
				WMCP: WMCPConfig{
					Endpoint: "https://example.com",
					Token:    "token",
				},
				AWS: AWSConfig{
					Region: "us-west-2",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			cfg: &Config{
				Mode: "unknown",
				AWS: AWSConfig{
					Region: "us-west-2",
				},
			},
			wantErr: true,
			errMsg:  "invalid mode",
		},
		{
			name: "native mode missing bot token",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "",
				},
				AWS: AWSConfig{
					Region: "us-west-2",
				},
			},
			wantErr: true,
			errMsg:  "webex.bot_token is required in native mode",
		},
		{
			name: "wmcp mode missing endpoint",
			cfg: &Config{
				Mode: "wmcp",
				WMCP: WMCPConfig{
					Endpoint: "",
					Token:    "token",
				},
				AWS: AWSConfig{
					Region: "us-west-2",
				},
			},
			wantErr: true,
			errMsg:  "wmcp.endpoint is required in wmcp mode",
		},
		{
			name: "wmcp mode missing token",
			cfg: &Config{
				Mode: "wmcp",
				WMCP: WMCPConfig{
					Endpoint: "https://example.com",
					Token:    "",
				},
				AWS: AWSConfig{
					Region: "us-west-2",
				},
			},
			wantErr: true,
			errMsg:  "wmcp.token is required in wmcp mode",
		},
		{
			name: "missing aws region with bedrock provider",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AI: AIConfig{
					Provider: "bedrock",
				},
				AWS: AWSConfig{
					Region: "",
				},
			},
			wantErr: true,
			errMsg:  "aws.region is required when using bedrock provider",
		},
		{
			name: "missing aws region with local provider is ok",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AI: AIConfig{
					Provider: "local",
				},
				AWS: AWSConfig{
					Region: "",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResolveAIProvider_ExplicitLocal(t *testing.T) {
	cfg := &Config{AI: AIConfig{Provider: "local"}}
	assert.Equal(t, "local", cfg.ResolveAIProvider())
}

func TestResolveAIProvider_ExplicitBedrock(t *testing.T) {
	cfg := &Config{AI: AIConfig{Provider: "bedrock"}}
	assert.Equal(t, "bedrock", cfg.ResolveAIProvider())
}

func TestResolveAIProvider_AutoWithAWSCreds(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secretkey")

	cfg := &Config{AI: AIConfig{Provider: "auto"}}
	assert.Equal(t, "bedrock", cfg.ResolveAIProvider())
}

func TestResolveAIProvider_AutoWithoutAWSCreds(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	cfg := &Config{AI: AIConfig{Provider: "auto"}}
	assert.Equal(t, "local", cfg.ResolveAIProvider())
}

func TestResolveAIProvider_AutoWithPartialAWSCreds(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	cfg := &Config{AI: AIConfig{Provider: "auto"}}
	assert.Equal(t, "local", cfg.ResolveAIProvider())
}

func TestResolveAIProvider_EmptyDefaultsToLocal(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	cfg := &Config{AI: AIConfig{Provider: ""}}
	assert.Equal(t, "local", cfg.ResolveAIProvider())
}

func TestValidateBedrockOverridesLocalModel(t *testing.T) {
	cfg := &Config{
		Mode: "native",
		Webex: WebexConfig{
			BotToken: "token",
		},
		AI: AIConfig{
			Provider: "bedrock",
			Model:    "phi4-mini",
		},
		AWS: AWSConfig{
			Region: "us-west-2",
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, "global.anthropic.claude-sonnet-4-6", cfg.AI.Model)
}

func TestValidateBedrockKeepsExplicitModel(t *testing.T) {
	cfg := &Config{
		Mode: "native",
		Webex: WebexConfig{
			BotToken: "token",
		},
		AI: AIConfig{
			Provider: "bedrock",
			Model:    "us.anthropic.claude-haiku-3-20240307-v1:0",
		},
		AWS: AWSConfig{
			Region: "us-west-2",
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
	assert.Equal(t, "us.anthropic.claude-haiku-3-20240307-v1:0", cfg.AI.Model)
}

func TestExpandEnvVarsOllamaHost(t *testing.T) {
	cfg := &Config{}
	cfg.AI.OllamaHost = "${OLLAMA_HOST_VAR}"

	t.Setenv("OLLAMA_HOST_VAR", "http://myhost:11434")
	cfg.expandEnvVars()

	assert.Equal(t, "http://myhost:11434", cfg.AI.OllamaHost)
}
