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

func TestAllowlist_Reload(t *testing.T) {
	allowlist := NewAllowlist([]string{"user@example.com"})
	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.False(t, allowlist.IsAllowed("admin@example.com"))

	// Reload with new list
	allowlist.Reload([]string{"admin@example.com", "superuser@example.com"})
	assert.False(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("admin@example.com"))
	assert.True(t, allowlist.IsAllowed("superuser@example.com"))
}

func TestAllowlist_Reload_EmptyList(t *testing.T) {
	allowlist := NewAllowlist([]string{"user@example.com"})
	assert.False(t, allowlist.IsAllowed("admin@example.com"))

	// Reload with empty list (allow all)
	allowlist.Reload([]string{})
	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("admin@example.com"))
	assert.True(t, allowlist.IsAllowed("anyone@example.com"))
}

func TestAllowlist_ConcurrentAccess(t *testing.T) {
	allowlist := NewAllowlist([]string{"user@example.com"})

	var wg sync.WaitGroup
	numGoroutines := 10

	// Half readers, half writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		if i%2 == 0 {
			// Reader goroutine
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = allowlist.IsAllowed("user@example.com")
				}
			}()
		} else {
			// Writer goroutine
			go func(index int) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					allowlist.Reload([]string{"user@example.com", "admin@example.com"})
				}
			}(i)
		}
	}

	wg.Wait()

	// After concurrent operations, check state is valid
	assert.True(t, allowlist.IsAllowed("user@example.com"))
	assert.True(t, allowlist.IsAllowed("admin@example.com"))
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

func TestAllowlist_ConcurrentReload(t *testing.T) {
	allowlist := NewAllowlist([]string{"user@example.com"})

	var wg sync.WaitGroup
	numGoroutines := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				//nolint:gosec // G115: index is bounded by numGoroutines (5)
				allowlist.Reload([]string{"user" + string(rune(index+48)) + "@example.com"})
			}
		}(i)
	}

	wg.Wait()

	// After concurrent reloads, the allowlist should be in a valid state
	assert.NotNil(t, allowlist.emails)
}
