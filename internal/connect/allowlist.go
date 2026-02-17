package connect

import (
	"strings"
	"sync"
)

// Allowlist manages email-based access control
type Allowlist struct {
	emails map[string]bool
	mu     sync.RWMutex
}

// NewAllowlist creates an allowlist from a slice of emails.
// If emails is empty or nil, all emails are allowed (allowlist is empty).
func NewAllowlist(emails []string) *Allowlist {
	a := &Allowlist{
		emails: make(map[string]bool),
	}

	for _, email := range emails {
		// Normalize to lowercase for case-insensitive comparison
		normalizedEmail := strings.ToLower(strings.TrimSpace(email))
		if normalizedEmail != "" {
			a.emails[normalizedEmail] = true
		}
	}

	return a
}

// IsAllowed checks if the email is allowed.
// Case-insensitive comparison.
// If allowlist is empty (nil or 0 entries), returns true (all allowed).
func (a *Allowlist) IsAllowed(email string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// If allowlist is empty, allow all
	if len(a.emails) == 0 {
		return true
	}

	// Normalize to lowercase for case-insensitive comparison
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	return a.emails[normalizedEmail]
}

// Reload updates the allowlist with a new set of emails.
// Thread-safe.
func (a *Allowlist) Reload(emails []string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Clear the existing allowlist
	a.emails = make(map[string]bool)

	// Add new emails
	for _, email := range emails {
		// Normalize to lowercase for case-insensitive comparison
		normalizedEmail := strings.ToLower(strings.TrimSpace(email))
		if normalizedEmail != "" {
			a.emails[normalizedEmail] = true
		}
	}
}
