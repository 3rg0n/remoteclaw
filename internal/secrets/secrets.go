package secrets

// Key identifies a secret RemoteClaw may retrieve. Only these are allowed.
type Key string

// These are secret-store lookup KEY NAMES (pass entry suffixes), not secret
// values. gosec G101 flags the words "token"/"key"; annotated as false positive.
const (
	KeyWebexBotToken Key = "webex_bot_token" //#nosec G101 -- key name (pass entry suffix), not a credential
	KeyWMCPToken     Key = "wmcp_token"      //#nosec G101 -- key name (pass entry suffix), not a credential
	KeyOpenAIAPIKey  Key = "openai_api_key"  //#nosec G101 -- key name (pass entry suffix), not a credential
)

// allowedKeys is the strict allowlist of retrievable secrets. Get on any
// other key returns an error — prevents arbitrary pass-store reads.
var allowedKeys = map[Key]struct{}{
	KeyWebexBotToken: {},
	KeyWMCPToken:     {},
	KeyOpenAIAPIKey:  {},
}

// Getter retrieves secrets from a backend (e.g., pass, Vault, 1Password).
type Getter interface {
	// Get returns (value, found, error).
	// found=false means the secret isn't in the store (not an error).
	// error is for real failures (store unreachable, exec error).
	Get(key Key) (string, bool, error)

	// Available reports whether this backend is usable right now.
	Available() bool

	// Name returns a short backend name for logging (e.g. "pass").
	Name() string
}

// IsAllowed checks whether a key is in the allowed list of retrievable secrets.
func IsAllowed(key Key) bool {
	_, ok := allowedKeys[key]
	return ok
}
