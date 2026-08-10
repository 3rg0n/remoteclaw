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

// IsAllowedInRoom checks whether email may act in a space of the given type.
// Unlike IsAllowed, an empty allowlist does not mean "allow all" here: only an
// explicit 1:1 space is permissive.
//
// roomType must be "direct" for a 1:1 space; anything else — "group", or a value
// we could not determine — takes the strict path and requires an explicit
// allowlist entry. Matching on "direct" rather than excluding "group" is
// deliberate and load-bearing: the upstream handler infers the room type from
// Mercury activity tags and returns "" when those tags are absent or
// unrecognized, so an unknown type is reachable in practice. Treating unknown as
// direct would mean an untagged group room with an empty allowlist authorized
// every sender in it — the exact gap this choke point exists to close (ADR 0005).
// Fail closed: we deny what we cannot classify.
func (a *Allowlist) IsAllowedInRoom(email string, roomType string) bool {
	if roomType == "direct" {
		return a.IsAllowed(email)
	}

	// Group rooms and unclassifiable spaces require an explicit entry.
	if len(a.emails) == 0 {
		return false
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	return a.emails[normalizedEmail]
}
