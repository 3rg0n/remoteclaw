package connect

import (
	"strings"
)

// Allowlist manages email-based access control.
//
// Immutable after construction: emails is populated once in NewAllowlist and
// never written again, so it is safe for concurrent readers without locking.
// Config is read once at startup; there is no runtime reload path.
type Allowlist struct {
	emails map[string]bool
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
	// If allowlist is empty, allow all
	if len(a.emails) == 0 {
		return true
	}

	// Normalize to lowercase for case-insensitive comparison
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	return a.emails[normalizedEmail]
}

// IsAllowedInRoom checks if the email is allowed in a group room.
// Unlike IsAllowed, this enforces strict matching even when the allowlist is empty:
// in group rooms, an empty allowlist means no one is authorized.
// roomType should be "group" for rooms or "direct" for 1:1 spaces.
func (a *Allowlist) IsAllowedInRoom(email string, roomType string) bool {
	if roomType != "group" {
		return a.IsAllowed(email)
	}

	// In group rooms, require explicit allowlist entry
	if len(a.emails) == 0 {
		return false
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	return a.emails[normalizedEmail]
}

