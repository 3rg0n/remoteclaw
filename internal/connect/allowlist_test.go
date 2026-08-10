package connect

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAllowlist_Empty(t *testing.T) {
	allowlist := NewAllowlist(nil)
	assert.NotNil(t, allowlist)
	assert.Equal(t, 0, len(allowlist.emails))
}

func TestNewAllowlist_WithEmails(t *testing.T) {
	emails := []string{"user@example.com", "admin@example.com"}
	allowlist := NewAllowlist(emails)
	assert.NotNil(t, allowlist)
	assert.Equal(t, 2, len(allowlist.emails))
}

func TestAllowlist_IsAllowed_EmptyAllowlist(t *testing.T) {
	allowlist := NewAllowlist(nil)
	// Empty allowlist should allow all
	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("anyone@anywhere.com"))
}

func TestAllowlist_IsAllowed_PopulatedAllowlist(t *testing.T) {
	emails := []string{"user@example.com", "admin@example.com"}
	allowlist := NewAllowlist(emails)

	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("admin@example.com"))
	assert.False(t, allowlist.IsAllowed("unauthorized@example.com"))
}

func TestAllowlist_IsAllowed_CaseInsensitive(t *testing.T) {
	emails := []string{"User@Example.com"}
	allowlist := NewAllowlist(emails)

	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("USER@EXAMPLE.COM"))
	assert.True(t, allowlist.IsAllowed("User@Example.com"))
}

func TestAllowlist_IsAllowed_WithWhitespace(t *testing.T) {
	emails := []string{"  user@example.com  ", "admin@example.com"}
	allowlist := NewAllowlist(emails)

	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("  user@example.com  "))
}

func TestAllowlist_IsAllowed_EmptyEmail(t *testing.T) {
	emails := []string{"", "user@example.com"}
	allowlist := NewAllowlist(emails)

	// Empty email in allowlist should be ignored
	assert.Equal(t, 1, len(allowlist.emails))
	assert.True(t, allowlist.IsAllowed("user@example.com"))
}

// TestAllowlist_ConcurrentReads verifies the lock-free read path is safe for
// concurrent use. Allowlist is immutable after construction, so readers need no
// synchronization — this is the guard on that invariant.
func TestAllowlist_ConcurrentReads(t *testing.T) {
	allowlist := NewAllowlist([]string{"user@example.com", "admin@example.com"})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				assert.True(t, allowlist.IsAllowed("user@example.com"))
				assert.False(t, allowlist.IsAllowed("nobody@example.com"))
				assert.True(t, allowlist.IsAllowedInRoom("admin@example.com", "group"))
			}
		}()
	}
	wg.Wait()
}

func TestAllowlist_IsAllowedInRoom_GroupEmpty(t *testing.T) {
	// Empty allowlist in group room should deny all
	allowlist := NewAllowlist(nil)
	assert.False(t, allowlist.IsAllowedInRoom("anyone@example.com", "group"))
}

func TestAllowlist_IsAllowedInRoom_GroupPopulated(t *testing.T) {
	allowlist := NewAllowlist([]string{"alice@example.com", "bob@example.com"})

	assert.True(t, allowlist.IsAllowedInRoom("alice@example.com", "group"))
	assert.True(t, allowlist.IsAllowedInRoom("bob@example.com", "group"))
	assert.False(t, allowlist.IsAllowedInRoom("charlie@example.com", "group"))
}

func TestAllowlist_IsAllowedInRoom_DirectEmpty(t *testing.T) {
	// Empty allowlist in direct space should allow all (same as IsAllowed)
	allowlist := NewAllowlist(nil)
	assert.True(t, allowlist.IsAllowedInRoom("anyone@example.com", "direct"))
}

func TestAllowlist_IsAllowedInRoom_DirectPopulated(t *testing.T) {
	allowlist := NewAllowlist([]string{"alice@example.com"})

	assert.True(t, allowlist.IsAllowedInRoom("alice@example.com", "direct"))
	assert.False(t, allowlist.IsAllowedInRoom("bob@example.com", "direct"))
}

func TestAllowlist_IsAllowedInRoom_EmptyRoomType(t *testing.T) {
	// Empty room type should behave like direct (legacy/1:1)
	allowlist := NewAllowlist(nil)
	assert.True(t, allowlist.IsAllowedInRoom("anyone@example.com", ""))
}
