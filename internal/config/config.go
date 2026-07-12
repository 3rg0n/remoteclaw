package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Build info variables set via ldflags
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Config holds all RemoteClaw configuration settings
type Config struct {
	Mode      string          `mapstructure:"mode"`
	Webex     WebexConfig     `mapstructure:"webex"`
	WMCP      WMCPConfig      `mapstructure:"wmcp"`
	AI        AIConfig        `mapstructure:"ai"`
	Execution ExecutionConfig `mapstructure:"execution"`
	Security  SecurityConfig  `mapstructure:"security"`
	Logging   LoggingConfig   `mapstructure:"logging"`
	Health    HealthConfig    `mapstructure:"health"`
}

// WebexConfig holds Webex-specific settings
type WebexConfig struct {
	BotToken      string   `mapstructure:"bot_token"`
	AllowedEmails []string `mapstructure:"allowed_emails"`
}

// WMCPConfig holds Webex MCP backend settings
type WMCPConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	Token    string `mapstructure:"token"`
}

// AI provider and mode constants.
const (
	// ProviderInferd uses the local inferd inference daemon over a Unix
	// socket / named pipe. This is the default local provider.
	ProviderInferd = "inferd"
	// ProviderOpenAICompat uses any OpenAI-compatible HTTP endpoint
	// (Ollama's /v1, mantle/Bedrock-as-OpenAI, Anthropic's OpenAI API,
	// vLLM, LM Studio, LocalAI, or real OpenAI).
	ProviderOpenAICompat = "openai-compat"

	// AIModeInterpret runs the local agentic AI loop: the model interprets
	// the user's request and drives the tools. This is the default.
	AIModeInterpret = "interpret"
	// AIModePassthrough treats the chat like an SSH session: the inbound
	// message is executed directly as a command with no local inference.
	// All security guardrails still apply.
	AIModePassthrough = "passthrough"
)

// AIConfig holds AI model settings
type AIConfig struct {
	// Provider selects the inference backend: "" (default) or "inferd" for
	// the local daemon, "openai-compat" for a remote OpenAI-compatible API.
	// An empty value resolves to "openai-compat" when OpenAIBaseURL is set,
	// otherwise "inferd". Ignored when Mode is "passthrough".
	Provider string `mapstructure:"provider"`
	// Mode selects request handling: "interpret" (default, local AI loop)
	// or "passthrough" (execute the message directly, no inference).
	Mode          string  `mapstructure:"mode"`
	Model         string  `mapstructure:"model"` // openai-compat model name; ignored by inferd (daemon owns the model)
	MaxTokens     int     `mapstructure:"max_tokens"`
	MaxIterations int     `mapstructure:"max_iterations"`
	Temperature   float64 `mapstructure:"temperature"`     // 0.0-1.0
	InferdSocket  string  `mapstructure:"inferd_socket"`   // optional socket/pipe path override (empty = platform default)
	OpenAIBaseURL string  `mapstructure:"openai_base_url"` // e.g. "http://localhost:11434/v1"; setting this selects openai-compat
	OpenAIAPIKey  string  `mapstructure:"openai_api_key"`  // bearer token for openai-compat (may be empty for local endpoints)
}

// ExecutionConfig holds command execution settings
type ExecutionConfig struct {
	DefaultTimeout time.Duration `mapstructure:"default_timeout"`
	MaxTimeout     time.Duration `mapstructure:"max_timeout"`
	Shell          string        `mapstructure:"shell"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	File   string `mapstructure:"file"`
}

// SecurityConfig holds security hardening settings
type SecurityConfig struct {
	DangerousCommands bool   `mapstructure:"dangerous_commands"` // Enable dangerous command blocking
	AuditLog          string `mapstructure:"audit_log"`          // Path to audit log file (empty = disabled)
	RateLimitPerMin   int    `mapstructure:"rate_limit_per_min"` // Max requests per minute per space
	Challenge         string `mapstructure:"challenge"`          // Challenge string for destructive command confirmation (empty = disabled)
}

// HealthConfig holds health check settings
type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"`
}

// Load reads and parses a YAML config file, applies defaults, and validates the configuration.
// If a .env file exists in the current directory, it is loaded first (does not override system env vars).
func Load(path string) (*Config, error) {
	// Load .env file if present — Overload() means .env values take precedence
	// over system env vars. If .env does not exist, the error is ignored.
	_ = godotenv.Overload()

	cfg := &Config{}

	// Set up viper
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(path)

	// Read the config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Apply defaults before unmarshaling
	applyDefaults(v)

	// Unmarshal into Config struct
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand environment variables in string fields
	cfg.expandEnvVars()

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyDefaults sets default values in viper before unmarshaling
func applyDefaults(v *viper.Viper) {
	v.SetDefault("mode", "native")
	v.SetDefault("ai.provider", "")
	v.SetDefault("ai.mode", AIModeInterpret)
	v.SetDefault("ai.model", "")
	v.SetDefault("ai.max_tokens", 4096)
	v.SetDefault("ai.max_iterations", 10)
	v.SetDefault("ai.temperature", 0.2)
	v.SetDefault("execution.default_timeout", "30s")
	v.SetDefault("execution.max_timeout", "5m")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("security.dangerous_commands", true)
	v.SetDefault("security.audit_log", "")
	v.SetDefault("security.rate_limit_per_min", 10)
	v.SetDefault("security.challenge", "")
	v.SetDefault("health.enabled", true)
	v.SetDefault("health.addr", "127.0.0.1:9090")
}

// expandEnvVars expands environment variables in string fields using os.ExpandEnv
func (c *Config) expandEnvVars() {
	c.Webex.BotToken = os.ExpandEnv(c.Webex.BotToken)
	c.WMCP.Endpoint = os.ExpandEnv(c.WMCP.Endpoint)
	c.WMCP.Token = os.ExpandEnv(c.WMCP.Token)
	c.AI.InferdSocket = os.ExpandEnv(c.AI.InferdSocket)
	c.AI.OpenAIBaseURL = os.ExpandEnv(c.AI.OpenAIBaseURL)
	c.AI.OpenAIAPIKey = os.ExpandEnv(c.AI.OpenAIAPIKey)
	c.Execution.Shell = os.ExpandEnv(c.Execution.Shell)
	c.Security.AuditLog = os.ExpandEnv(c.Security.AuditLog)
	c.Security.Challenge = os.ExpandEnv(c.Security.Challenge)
	c.Logging.File = os.ExpandEnv(c.Logging.File)
}

// ResolveAIProvider determines the inference provider from config.
// Explicit values ("inferd", "openai-compat") are returned as-is. An empty
// provider resolves to "openai-compat" when an OpenAI base URL is set,
// otherwise to the local "inferd" daemon.
func (c *Config) ResolveAIProvider() string {
	switch c.AI.Provider {
	case ProviderInferd, ProviderOpenAICompat:
		return c.AI.Provider
	default: // empty
		if c.AI.OpenAIBaseURL != "" {
			return ProviderOpenAICompat
		}
		return ProviderInferd
	}
}

// PassthroughMode reports whether the agent should execute inbound messages
// directly as commands without local inference.
func (c *Config) PassthroughMode() bool {
	return c.AI.Mode == AIModePassthrough
}

// Validate checks that required fields are populated based on the configured mode
func (c *Config) Validate() error {
	// Validate mode
	if c.Mode != "native" && c.Mode != "wmcp" {
		return fmt.Errorf("invalid mode: %q (must be 'native' or 'wmcp')", c.Mode)
	}

	// Validate mode-specific requirements
	if c.Mode == "native" {
		if c.Webex.BotToken == "" {
			return fmt.Errorf("webex.bot_token is required in native mode")
		}
	}

	if c.Mode == "wmcp" {
		if c.WMCP.Endpoint == "" {
			return fmt.Errorf("wmcp.endpoint is required in wmcp mode")
		}
		if c.WMCP.Token == "" {
			return fmt.Errorf("wmcp.token is required in wmcp mode")
		}
	}

	// Validate AI mode
	if c.AI.Mode != "" && c.AI.Mode != AIModeInterpret && c.AI.Mode != AIModePassthrough {
		return fmt.Errorf("invalid ai.mode: %q (must be %q or %q)", c.AI.Mode, AIModeInterpret, AIModePassthrough)
	}

	// Passthrough mode needs no inference backend; skip provider validation.
	if !c.PassthroughMode() {
		// Validate explicit provider values
		if c.AI.Provider != "" && c.AI.Provider != ProviderInferd && c.AI.Provider != ProviderOpenAICompat {
			return fmt.Errorf("invalid ai.provider: %q (must be %q or %q)", c.AI.Provider, ProviderInferd, ProviderOpenAICompat)
		}
		// openai-compat requires a base URL
		if c.ResolveAIProvider() == ProviderOpenAICompat && c.AI.OpenAIBaseURL == "" {
			return fmt.Errorf("ai.openai_base_url is required when using the openai-compat provider")
		}
	}

	// Cap temperature to 0.3 for security consistency — high temperatures
	// increase non-determinism which can cause intermittent safety bypass.
	if c.AI.Temperature > 0.3 {
		c.AI.Temperature = 0.3
	}

	return nil
}
