package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/3rg0n/remoteclaw/internal/secrets"
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
ai:
  provider: "openai-compat"
  openai_base_url: "http://localhost:11434/v1"
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

	assert.Equal(t, "openai-compat", cfg.AI.Provider)
	assert.Equal(t, "http://localhost:11434/v1", cfg.AI.OpenAIBaseURL)
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
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Minimal config with required fields
	configContent := `
mode: native
webex:
  bot_token: "test-token"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Check defaults are applied
	assert.Equal(t, "", cfg.AI.Provider)
	assert.Equal(t, AIModeInterpret, cfg.AI.Mode)
	assert.Equal(t, "", cfg.AI.Model)
	assert.Equal(t, 4096, cfg.AI.MaxTokens)
	assert.Equal(t, 10, cfg.AI.MaxIterations)
	assert.InDelta(t, 0.2, cfg.AI.Temperature, 0.001)

	// Empty provider with no base URL resolves to inferd
	assert.Equal(t, ProviderInferd, cfg.ResolveAIProvider())
	assert.False(t, cfg.PassthroughMode())

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
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid mode")
}

func TestValidateOpenAICompatRequiresBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  provider: "openai-compat"
  openai_base_url: ""
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "ai.openai_base_url is required when using the openai-compat provider")
}

func TestValidateInferdProviderNeedsNoBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  provider: "inferd"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, ProviderInferd, cfg.ResolveAIProvider())
}

func TestValidateInvalidProvider(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  provider: "bedrock"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid ai.provider")
}

func TestValidateInvalidAIMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  mode: "sideways"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid ai.mode")
}

func TestValidatePassthroughSkipsProviderValidation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Passthrough with openai-compat but no base URL is still valid:
	// passthrough needs no inference backend.
	configContent := `
mode: native
webex:
  bot_token: "test-token"
ai:
  mode: "passthrough"
  provider: "openai-compat"
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.True(t, cfg.PassthroughMode())
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
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "prefix-expanded-token-suffix", cfg.Webex.BotToken)
}

func TestLoadConfigWMCPMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: wmcp
wmcp:
  endpoint: "https://wmcp.example.com"
  token: "wmcp-secret-token"
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
	assert.Equal(t, "warn", cfg.Logging.Level)

	// Verify other defaults still apply
	assert.Equal(t, AIModeInterpret, cfg.AI.Mode)
	assert.True(t, cfg.Health.Enabled)
}

func TestTimeoutParsing(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
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

func TestExpandEnvVarsAIFields(t *testing.T) {
	cfg := &Config{}
	cfg.AI.InferdSocket = "${INFERD_SOCK}"
	cfg.AI.OpenAIBaseURL = "${OPENAI_BASE}"
	cfg.AI.OpenAIAPIKey = "${OPENAI_KEY}"

	t.Setenv("INFERD_SOCK", "/tmp/inferd/inferd.sock")
	t.Setenv("OPENAI_BASE", "http://localhost:8080/openai/v1")
	t.Setenv("OPENAI_KEY", "sk-test")

	cfg.expandEnvVars()

	assert.Equal(t, "/tmp/inferd/inferd.sock", cfg.AI.InferdSocket)
	assert.Equal(t, "http://localhost:8080/openai/v1", cfg.AI.OpenAIBaseURL)
	assert.Equal(t, "sk-test", cfg.AI.OpenAIAPIKey)
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
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			cfg: &Config{
				Mode: "unknown",
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
			},
			wantErr: true,
			errMsg:  "wmcp.token is required in wmcp mode",
		},
		{
			name: "openai-compat missing base url",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AI: AIConfig{
					Provider: ProviderOpenAICompat,
				},
			},
			wantErr: true,
			errMsg:  "ai.openai_base_url is required",
		},
		{
			name: "inferd provider needs no base url",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AI: AIConfig{
					Provider: ProviderInferd,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid provider",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AI: AIConfig{
					Provider: "bedrock",
				},
			},
			wantErr: true,
			errMsg:  "invalid ai.provider",
		},
		{
			name: "passthrough skips provider validation",
			cfg: &Config{
				Mode: "native",
				Webex: WebexConfig{
					BotToken: "token",
				},
				AI: AIConfig{
					Mode:     AIModePassthrough,
					Provider: ProviderOpenAICompat,
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

func TestResolveAIProvider_ExplicitInferd(t *testing.T) {
	cfg := &Config{AI: AIConfig{Provider: ProviderInferd}}
	assert.Equal(t, ProviderInferd, cfg.ResolveAIProvider())
}

func TestResolveAIProvider_ExplicitOpenAICompat(t *testing.T) {
	cfg := &Config{AI: AIConfig{Provider: ProviderOpenAICompat, OpenAIBaseURL: "http://x/v1"}}
	assert.Equal(t, ProviderOpenAICompat, cfg.ResolveAIProvider())
}

func TestResolveAIProvider_EmptyWithBaseURL(t *testing.T) {
	cfg := &Config{AI: AIConfig{Provider: "", OpenAIBaseURL: "http://localhost:11434/v1"}}
	assert.Equal(t, ProviderOpenAICompat, cfg.ResolveAIProvider())
}

func TestResolveAIProvider_EmptyDefaultsToInferd(t *testing.T) {
	cfg := &Config{AI: AIConfig{Provider: ""}}
	assert.Equal(t, ProviderInferd, cfg.ResolveAIProvider())
}

func TestPassthroughMode(t *testing.T) {
	assert.True(t, (&Config{AI: AIConfig{Mode: AIModePassthrough}}).PassthroughMode())
	assert.False(t, (&Config{AI: AIConfig{Mode: AIModeInterpret}}).PassthroughMode())
	assert.False(t, (&Config{AI: AIConfig{Mode: ""}}).PassthroughMode())
}

func TestValidateHealthAddr(t *testing.T) {
	tests := []struct {
		name             string
		addr             string
		enabled          bool
		allowNonLoopback bool
		wantErr          string
	}{
		// Loopback — accepted.
		{name: "ipv4 loopback", addr: "127.0.0.1:9090", enabled: true},
		{name: "ipv4 loopback alt", addr: "127.0.0.53:9090", enabled: true},
		{name: "localhost", addr: "localhost:9090", enabled: true},
		{name: "localhost mixed case", addr: "LocalHost:9090", enabled: true},
		{name: "ipv6 loopback", addr: "[::1]:9090", enabled: true},

		// Non-loopback — rejected.
		{name: "all interfaces ipv4", addr: "0.0.0.0:9090", enabled: true,
			wantErr: "binds non-loopback address"},
		{name: "lan address", addr: "192.168.1.5:9090", enabled: true,
			wantErr: "binds non-loopback address"},
		{name: "empty host", addr: ":9090", enabled: true,
			wantErr: "binds all interfaces"},
		{name: "ipv6 unspecified", addr: "[::]:9090", enabled: true,
			wantErr: "binds non-loopback address"},

		// Hostnames are rejected: DNS resolution is not a durable guarantee.
		{name: "hostname", addr: "health.internal:9090", enabled: true,
			wantErr: "must be an IP literal"},

		// Malformed.
		{name: "no port", addr: "127.0.0.1", enabled: true, wantErr: "invalid health.addr"},
		{name: "empty", addr: "", enabled: true, wantErr: "invalid health.addr"},
		{name: "garbage", addr: "not::a::host", enabled: true, wantErr: "invalid health.addr"},

		// Opt-out paths.
		{name: "non-loopback allowed by opt-in", addr: "0.0.0.0:9090", enabled: true,
			allowNonLoopback: true},
		{name: "health disabled skips validation", addr: "0.0.0.0:9090", enabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Health: HealthConfig{
				Enabled:          tt.enabled,
				Addr:             tt.addr,
				AllowNonLoopback: tt.allowNonLoopback,
			}}
			err := cfg.validateHealthAddr()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestValidateRejectsNonLoopbackHealthAddr proves the check is reachable from
// Load(), not just callable in isolation.
func TestValidateRejectsNonLoopbackHealthAddr(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
mode: native
webex:
  bot_token: "test-token"
health:
  enabled: true
  addr: "0.0.0.0:9090"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0600))

	cfg, err := Load(configPath)
	assert.Nil(t, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binds non-loopback address")
}

// fakeGetter is a test double for secrets.Getter.
type fakeGetter struct {
	available bool
	values    map[secrets.Key]string
	errs      map[secrets.Key]error
}

func (f *fakeGetter) Available() bool { return f.available }
func (f *fakeGetter) Name() string    { return "fake" }
func (f *fakeGetter) Get(key secrets.Key) (string, bool, error) {
	if err, ok := f.errs[key]; ok {
		return "", false, err
	}
	if v, ok := f.values[key]; ok {
		return v, true, nil
	}
	return "", false, nil
}

func TestResolveSecrets_StoreOverridesEnv(t *testing.T) {
	cfg := &Config{}
	cfg.Webex.BotToken = "env-token"
	cfg.WMCP.Token = "env-wmcp"
	cfg.AI.OpenAIAPIKey = "env-key"

	getter := &fakeGetter{
		available: true,
		values: map[secrets.Key]string{
			secrets.KeyWebexBotToken: "store-token",
			secrets.KeyOpenAIAPIKey:  "store-key",
			// wmcp_token intentionally absent → keep env value
		},
	}
	cfg.resolveSecretsWith(getter)

	assert.Equal(t, "store-token", cfg.Webex.BotToken, "store value should override env")
	assert.Equal(t, "store-key", cfg.AI.OpenAIAPIKey)
	assert.Equal(t, "env-wmcp", cfg.WMCP.Token, "absent-in-store secret keeps env value")
}

func TestResolveSecrets_UnavailableStoreKeepsEnv(t *testing.T) {
	cfg := &Config{}
	cfg.Webex.BotToken = "env-token"

	cfg.resolveSecretsWith(&fakeGetter{available: false})
	assert.Equal(t, "env-token", cfg.Webex.BotToken)

	// nil getter is also safe (keeps env).
	cfg.resolveSecretsWith(nil)
	assert.Equal(t, "env-token", cfg.Webex.BotToken)
}

func TestResolveSecrets_LookupErrorKeepsEnv(t *testing.T) {
	cfg := &Config{}
	cfg.Webex.BotToken = "env-token"

	getter := &fakeGetter{
		available: true,
		errs:      map[secrets.Key]error{secrets.KeyWebexBotToken: assertErr("boom")},
	}
	cfg.resolveSecretsWith(getter)
	assert.Equal(t, "env-token", cfg.Webex.BotToken, "lookup error must not clobber the env value")
}

// assertErr is a tiny error helper for tests.
type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestLockdownDefaultsTrue(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
mode: native
webex:
  bot_token: "t"
ai:
  provider: "inferd"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0600))
	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.True(t, cfg.Security.Lockdown, "lockdown must default to true (secure by default)")

	// LockdownPaths includes the config file and its directory.
	paths := cfg.LockdownPaths()
	absConfig, _ := filepath.Abs(configPath)
	assert.Contains(t, paths, absConfig)
	assert.Contains(t, paths, filepath.Dir(absConfig))
}

func TestLockdownCanBeDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
mode: native
webex:
  bot_token: "t"
security:
  lockdown: false
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0600))
	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.False(t, cfg.Security.Lockdown, "operator opt-out to wide open must be honored")
}
